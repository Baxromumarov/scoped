package scoped

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Scope lifecycle states.
const (
	stateOpen    int32 = iota // Go() allowed
	stateClosing              // Wait() called, draining tasks
	stateClosed               // all tasks finished
)

// Scope manages a set of concurrent tasks with structured lifecycle
// guarantees. Every goroutine spawned via [Scope.Go] is owned by the
// Scope and must complete before [Scope.Wait] returns.
//
// Use [Run] for automatic lifecycle management (preferred), or
// [New] + [Scope.Wait] for advanced use cases.
type Scope struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	cfg    config

	state atomic.Int32
	wg    sync.WaitGroup

	// FailFast: stores the first error.
	firstErr error
	errOnce  sync.Once

	// Collect: accumulates all errors.
	errs  []*TaskError
	errMu sync.Mutex

	// Panics captured from child tasks (when panicAsErr is false).
	panics  []*PanicError
	panicMu sync.Mutex

	// Semaphore for bounded concurrency.
	sem chan struct{}

	// Wait result caching for idempotent Wait.
	waitOnce sync.Once
	waitErr  error
}

// TaskError represents an error from a specific task, including its name and the error itself.
// This struct is used to provide more context about which task failed when collecting errors in the Collect policy.

// New creates a new Scope with the given parent context and options.
// The returned context is a child of parent and is canceled when the
// scope finishes or is explicitly canceled.
//
// The caller MUST call [Scope.Wait] when done spawning tasks.
// Prefer [Run] for automatic lifecycle management.
func New(parent context.Context, opts ...Option) (*Scope, context.Context) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, cancel := context.WithCancelCause(parent)

	s := &Scope{
		ctx:    ctx,
		cancel: cancel,
		cfg:    cfg,
	}

	if cfg.limit > 0 {
		s.sem = make(chan struct{}, cfg.limit)
	}

	return s, ctx
}

// Run creates a Scope, executes fn (which should spawn tasks via
// [Scope.Go]), and waits for all spawned tasks to complete.
//
// This is the preferred entry point for structured concurrency.
// It guarantees that no goroutine outlives the returned error.
// If fn panics, Run still waits for spawned tasks, then re-panics.
//
//	err := scoped.Run(ctx, func(s *scoped.Scope) {
//	    s.Go("task-a", taskA)
//	    s.Go("task-b", taskB)
//	})
//
// If policy isn't given it is FailFast by default
func Run(parent context.Context, fn func(s *Scope), opts ...Option) (err error) {
	s, _ := New(parent, opts...)

	defer func() {
		runPanic := recover()

		var (
			waitErr   error
			waitPanic any
		)

		func() {
			defer func() {
				waitPanic = recover()
			}()
			waitErr = s.Wait()
		}()

		// If fn panicked, preserve that panic after cleanup.
		if runPanic != nil {
			panic(runPanic)
		}
		// Otherwise propagate Wait panic semantics.
		if waitPanic != nil {
			panic(waitPanic)
		}

		err = waitErr

	}()

	fn(s)
	return nil
}

// Go spawns a named task in the scope. The task function receives the
// scope's context, which is canceled when the scope shuts down.
//
// Tasks may spawn additional sibling tasks by calling Go on the same scope.
//
// Go panics if called after [Scope.Wait] has returned (state is closed).
func (s *Scope) Go(name string, fn func(ctx context.Context) error) {
	if s.state.Load() >= stateClosed {
		panic("scoped: Go called on a closed scope")
	}

	s.wg.Add(1)
	info := TaskInfo{Name: name}

	go func() {
		defer s.wg.Done()

		// Acquire semaphore slot if bounded.
		if s.sem != nil {
			select {
			case s.sem <- struct{}{}:
				defer func() { <-s.sem }()
			case <-s.ctx.Done():
				// Context canceled while waiting for a slot;
				// report the context error so the task isn't silently dropped.
				if s.cfg.onDone != nil {
					s.cfg.onDone(info, s.ctx.Err(), 0)
				}
				s.recordError(info, s.ctx.Err())
				return
			}
		}

		if s.cfg.onStart != nil {
			s.cfg.onStart(info)
		}

		start := time.Now()
		err := s.exec(fn)
		elapsed := time.Since(start)

		if s.cfg.onDone != nil {
			s.cfg.onDone(info, err, elapsed)
		}

		if err != nil {
			s.recordError(info, err)
		}
	}()
}

// Wait waits for all tasks to complete and returns the resulting error.
//
// Behavior depends on the error policy:
//   - [FailFast]: returns the first error (or nil).
//   - [Collect]: returns all errors joined via [errors.Join] (or nil).
//
// If any task panicked and [WithPanicAsError] was not set, Wait re-panics
// with the first [*PanicError]. The caller can recover it and inspect
// [PanicError.Value] and [PanicError.Stack].
//
// Wait is safe to call multiple times; subsequent calls return the
// cached result. If panics need re-raising, they are raised on every call.
// When error happens first error will be returned
func (s *Scope) Wait() error {
	s.waitOnce.Do(func() {
		s.state.Store(stateClosing)
		s.wg.Wait()
		s.state.Store(stateClosed)
		s.cancel(nil) // release context resourcess

		switch s.cfg.policy {
		case FailFast:
			s.waitErr = s.firstErr
		case Collect:
			s.errMu.Lock()
			if len(s.errs) > 0 {
				for _, taskErr := range s.errs {
					s.waitErr = errors.Join(s.waitErr, taskErr)
				}
			}
			s.errMu.Unlock()
		}
	})

	// Re-panic on every Wait call so callers always see the panic.
	if !s.cfg.panicAsErr {
		s.panicMu.Lock()
		panics := s.panics
		s.panicMu.Unlock()
		if len(panics) > 0 {
			panic(panics[0])
		}
	}

	return s.waitErr
}

// Context returns the scope's context. It is canceled when the scope
// finishes or when [Scope.Cancel] is called.
func (s *Scope) Context() context.Context {
	return s.ctx
}

// Cancel cancels the scope with the given cause. All child tasks will
// observe the cancellation via the context passed to their function.
func (s *Scope) Cancel(cause error) {
	s.cancel(cause)
}

// exec runs fn with panic recovery. If fn panics, the panic is either
// converted to a *PanicError (if panicAsErr is set) or stored for
// re-raising in Wait.
func (s *Scope) exec(fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			pe := newPanicError(r)
			if s.cfg.panicAsErr {
				err = pe
			} else {
				s.panicMu.Lock()
				s.panics = append(s.panics, pe)
				s.panicMu.Unlock()
				s.cancel(pe)
			}
		}
	}()
	return fn(s.ctx)
}

// recordError stores an error according to the configured policy.
func (s *Scope) recordError(taskInfo TaskInfo, err error) {
	switch s.cfg.policy {
	case FailFast:
		s.errOnce.Do(func() {
			s.firstErr = err
			s.cancel(err)
		})
	case Collect:
		s.errMu.Lock()
		s.errs = append(s.errs, &TaskError{
			Task: taskInfo,
			Err:  err,
		})
		s.errMu.Unlock()
	}
}
