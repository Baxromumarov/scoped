// Scope provides a mechanism for structured concurrency in Spawn, managing a group of goroutines
// with coordinated lifecycle and error handling. It allows spawning child tasks that share a
// common context and aggregate errors according to a configured policy (FailFast or Collect).
//
// A Scope must be created via New() and finalized by calling Wait(). The Spawner interface
// is used to spawn new tasks within the scope. All tasks receive a context that is cancelled
// when the scope ends, either due to completion of all tasks or an explicit cancellation.
//
// Error handling is configurable:
//   - FailFast: The scope stops on the first error and cancels remaining tasks.
//   - Collect: All errors are collected and joined together at the end.
//
// Panics in tasks are captured and can be converted to errors (panicAsErr option) or
// re-panicked after scope finalization.
//
// Example usage:
//
//	sc, spawner := New(context.Background())
//	spawner.Spawn("child", func(ctx context.Context, sp Spawner) error {
//	    *task implementation is here*
//	    return nil
//	})
//	err := sc.Wait()
package scoped

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// TaskFunc is the signature for a task function running within a scope.
// It receives a context (cancelled when the scope ends) and a Spawner
// to spawn sub-tasks.
type TaskFunc func(ctx context.Context, sp Spawner) error

// scope internal
// it maintains the state of a structured concurrency scope.
type scope struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	cfg    config

	wg sync.WaitGroup

	firstErr atomicError // for concurrent access in Spawn and Wait
	errOnce  sync.Once

	errMu         sync.Mutex
	errs          []*TaskError
	droppedErrors int // errors exceeding maxErrors cap

	panicMu sync.Mutex
	panics  []*PanicError

	sem chan struct{}

	finOnce  sync.Once
	finErr   error
	finPanic *PanicError

	// Observability counters.
	totalSpawned atomic.Int64
	activeTasks  atomic.Int64
}

// Run creates a [Scope], invokes fn with its root [Spawner], then waits for
// every spawned task to complete. It returns the aggregated error according to
// the configured [Policy] (default [FailFast]).
//
// Run is the primary entry point for structured concurrency. The scope is
// automatically finalized when fn returns, so no explicit cleanup is needed.
func Run(parent context.Context, fn func(sp Spawner), opts ...Option) (err error) {
	sc, sp := New(parent, opts...)

	defer func() {
		// Step 1: Capture any panic from fn before cleanup.
		runPanic := recover()

		// Step 2: Close the root spawner so no new tasks can be submitted.
		sc.root.close()

		// Step 3: Wait for all in-flight tasks and aggregate errors.
		waitErr, waitPanic := sc.s.finalize()

		// Step 4: Re-raise panics. User panics take priority over task panics.
		if runPanic != nil {
			panic(runPanic)
		}
		if waitPanic != nil {
			panic(waitPanic)
		}

		// Step 5: Surface the aggregated task error.
		err = waitErr
	}()

	fn(sp)
	return nil
}

// finalize waits for all tasks to complete and returns the aggregated error.
func (s *scope) finalize() (error, *PanicError) {
	s.finOnce.Do(func() {
		s.wg.Wait()

		// Check if context was cancelled externally (before cleanup).
		ctxWasCancelled := s.ctx.Err() != nil

		select {
		case <-s.ctx.Done():
		default:
			s.cancel(nil)
		}

		if !s.cfg.panicAsErr {
			s.panicMu.Lock()
			if len(s.panics) > 0 {
				s.finPanic = s.panics[0]
			}
			s.panicMu.Unlock()
		}

		switch s.cfg.policy {
		case FailFast:
			if v := s.firstErr.Load(); v != nil {
				s.finErr = v
			}
		case Collect:
			s.errMu.Lock()
			if len(s.errs) > 0 {
				errs := make([]error, 0, len(s.errs))
				for _, te := range s.errs {
					errs = append(errs, te)
				}
				s.finErr = errors.Join(errs...)
			}
			s.errMu.Unlock()
		}

		// If no task errors were recorded but the context was cancelled
		// externally (before scope cleanup), surface the context error.
		if s.finErr == nil && ctxWasCancelled {
			s.finErr = s.ctx.Err()
		}
	})

	return s.finErr, s.finPanic
}

// exec runs a function with panic recovery.
func (s *scope) exec(fn func(ctx context.Context) error) (err error) {
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

// emitEvent calls the onEvent hook if registered.
func (s *scope) emitEvent(e TaskEvent) {
	if s.cfg.onEvent != nil {
		s.cfg.onEvent(e)
	}
}

// emitCompletionEvent determines the correct EventKind for a completed task
// and emits the event via the onEvent hook.
func (s *scope) emitCompletionEvent(info TaskInfo, err error, d time.Duration) {
	if s.cfg.onEvent == nil {
		return
	}

	var kind EventKind
	
	switch {
	case err == nil:
		kind = EventDone
	case errors.As(err, new(*PanicError)):
		kind = EventPanicked
	case s.ctx.Err() != nil:
		kind = EventCancelled
	default:
		kind = EventErrored
	}

	s.cfg.onEvent(TaskEvent{
		Kind:     kind,
		Task:     info,
		Err:      err,
		Duration: d,
	})
}

// recordError records an error according to the configured policy.
func (s *scope) recordError(taskInfo TaskInfo, err error) {
	te := &TaskError{
		Task: taskInfo,
		Err:  err,
	}
	
	switch s.cfg.policy {
	case FailFast:
		s.errOnce.Do(
			func() {
				s.firstErr.Store(te)
				s.cancel(err)
			},
		)
	case Collect:
		s.errMu.Lock()
		if s.cfg.maxErrors > 0 && len(s.errs) >= s.cfg.maxErrors {
			s.droppedErrors++
		} else {
			s.errs = append(s.errs, te)
		}
		s.errMu.Unlock()
	}
}

// Scope wraps the internal scope state and exposes lifecycle and
// observability methods. Create one via [New]; finalize with [Scope.Wait].
type Scope struct {
	s        *scope
	root     *spawner
	once     sync.Once
	result   error
	panicVal *PanicError
}

// New creates a [Scope] and root [Spawner] for manual lifecycle control.
// The caller must call [Scope.Wait] to finalize the scope and collect errors.
//
// Prefer [Run] for most use cases; use New when you need to pass the
// [Spawner] across function boundaries or integrate with existing lifecycle
// management.
func New(parent context.Context, opts ...Option) (*Scope, Spawner) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, cancel := context.WithCancelCause(parent)
	s := &scope{
		ctx:    ctx,
		cancel: cancel,
		cfg:    cfg,
	}

	if cfg.limit > 0 {
		s.sem = make(chan struct{}, cfg.limit)
	}

	root := &spawner{
		s:    s,
		open: true,
	}

	sc := &Scope{
		s:    s,
		root: root,
	}

	return sc, root
}

// Wait closes the root [Spawner], waits for all spawned tasks to complete,
// and returns the aggregated error. If a task panicked and [WithPanicAsError]
// was not set, Wait re-panics with the captured [*PanicError].
//
// Wait is idempotent; subsequent calls return the same result.
func (sc *Scope) Wait() error {
	sc.once.Do(func() {
		sc.root.close()
		sc.result, sc.panicVal = sc.s.finalize()
	})

	if sc.panicVal != nil {
		panic(sc.panicVal)
	}
	return sc.result
}

// Cancel cancels the scope's context with the given cause, signaling all
// tasks to stop. Subsequent calls have no additional effect on the context.
func (sc *Scope) Cancel(err error) {
	sc.s.cancel(err)
}

// Context returns the scope's context, which is cancelled when the scope
// finalizes or is explicitly cancelled via [Scope.Cancel].
func (sc *Scope) Context() context.Context {
	return sc.s.ctx
}

// ActiveTasks returns the number of tasks currently executing within the scope.
func (sc *Scope) ActiveTasks() int64 {
	return sc.s.activeTasks.Load()
}

// TotalSpawned returns the total number of tasks that have been spawned
// within the scope, including those that have already completed.
func (sc *Scope) TotalSpawned() int64 {
	return sc.s.totalSpawned.Load()
}

// DroppedErrors returns the number of errors that were not stored because
// the [WithMaxErrors] limit was reached. This is only meaningful in
// [Collect] mode.
func (sc *Scope) DroppedErrors() int {
	sc.s.errMu.Lock()
	defer sc.s.errMu.Unlock()

	return sc.s.droppedErrors
}
