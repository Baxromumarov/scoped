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

// Spawner allows spawning concurrent tasks into a scope.
type Spawner interface {
	// Go starts a new concurrent task with the given name.
	// The task function receives a child Spawner allowing it to create sub-tasks.
	Go(name string, fn TaskFunc)
}

// ======================
// Scope internals
// ======================

// scope maintains the state of a structured concurrency scope.
type scope struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	cfg    config

	wg sync.WaitGroup

	// error handling
	firstErr atomic.Value // for concurrent access in Go and Wait
	errOnce  sync.Once

	errMu sync.Mutex
	errs  []*TaskError

	panicMu sync.Mutex
	panics  []*PanicError

	sem chan struct{}
}

// ======================
// Public entry point
// ======================

func Run(parent context.Context, fn func(sp Spawner), opts ...Option) (err error) {
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
		open: atomic.Bool{},
	}
	root.open.Store(true)

	defer func() {
		root.close()

		runPanic := recover()

		var (
			waitErr   error
			waitPanic any
		)

		func() {
			defer func() {
				waitPanic = recover()
			}()
			waitErr = s.wait()
		}()

		if runPanic != nil {
			panic(runPanic)
		}
		if waitPanic != nil {
			panic(waitPanic)
		}

		err = waitErr
	}()

	fn(root)
	return nil
}

// ======================
// Spawner implementation
// ======================

// spawner implements the Spawner interface and manages the lifecycle of tasks.
type spawner struct {
	s      *scope
	open   atomic.Bool
	closed atomic.Bool
}

// Go implements Spawner.Go.
func (sp *spawner) Go(name string, fn TaskFunc) {
	if !sp.open.Load() {
		panic("scoped: spawning after spawner closed")
	}

	sp.s.wg.Add(1)
	info := TaskInfo{Name: name}

	go func() {
		defer sp.s.wg.Done()

		// semaphore
		if sp.s.sem != nil {
			select {
			case sp.s.sem <- struct{}{}:
				defer func() { <-sp.s.sem }()
			case <-sp.s.ctx.Done():
				sp.s.recordError(info, sp.s.ctx.Err())
				return
			}
		}

		if sp.s.cfg.onStart != nil {
			sp.s.cfg.onStart(info)
		}

		// child spawner
		child := &spawner{
			s:    sp.s,
			open: atomic.Bool{},
		}
		child.open.Store(true)

		start := time.Now()
		err := sp.s.exec(func(ctx context.Context) error {
			return fn(ctx, child)
		})
		elapsed := time.Since(start)

		child.close()

		if sp.s.cfg.onDone != nil {
			sp.s.cfg.onDone(info, err, elapsed)
		}

		if err != nil {
			sp.s.recordError(info, err)
		}
	}()
}

// close marks the spawner as closed, preventing further Go calls.
func (sp *spawner) close() {
	if sp.closed.Swap(true) {
		return
	}
	sp.open.Store(false)
}

// ======================
// Scope helpers
// ======================

// wait waits for all tasks to complete and returns the aggregated error.
func (s *scope) wait() error {
	s.wg.Wait()
	s.cancel(nil)

	if !s.cfg.panicAsErr {
		s.panicMu.Lock()
		defer s.panicMu.Unlock()
		if len(s.panics) > 0 {
			panic(s.panics[0])
		}
	}

	switch s.cfg.policy {
	case FailFast:
		if v := s.firstErr.Load(); v != nil {
			return v.(error)
		}
	case Collect:
		s.errMu.Lock()
		defer s.errMu.Unlock()
		if len(s.errs) > 0 {
			errs := make([]error, 0, len(s.errs))
			for _, te := range s.errs {
				errs = append(errs, te)
			}
			return errors.Join(errs...)
		}
	}

	return nil
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

// recordError records an error according to the configured policy.
func (s *scope) recordError(taskInfo TaskInfo, err error) {
	switch s.cfg.policy {
	case FailFast:
		s.errOnce.Do(func() {
			s.firstErr.Store(err)
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
