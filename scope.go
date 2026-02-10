package scoped

import (
	"context"
	"errors"
	"sync"
)

// TaskFunc is the signature for a task function running within a scope.
// It receives a context (cancelled when the scope ends) and a Spawner
// to spawn sub-tasks.
type TaskFunc func(ctx context.Context, sp Spawner) error

// scope internal
// scope maintains the state of a structured concurrency scope.
type scope struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	cfg    config

	wg sync.WaitGroup

	firstErr atomicError // for concurrent access in Go and Wait
	errOnce  sync.Once

	errMu sync.Mutex
	errs  []*TaskError

	panicMu sync.Mutex
	panics  []*PanicError

	sem chan struct{}

	finOnce  sync.Once
	finErr   error
	finPanic *PanicError
}

func Run(parent context.Context, fn func(sp Spawner), opts ...Option) (err error) {
	sc, sp := New(parent, opts...)

	defer func() {
		runPanic := recover()
		waitErr, waitPanic := sc.s.finalize() // or sc.Wait(), but that panics on panicErr

		// close root spawner regardless
		sc.root.close()

		if runPanic != nil {
			panic(runPanic)
		}
		if waitPanic != nil {
			panic(waitPanic)
		}
		err = waitErr
	}()

	fn(sp)
	return nil
}

// finalize waits for all tasks to complete and returns the aggregated error.
func (s *scope) finalize() (error, *PanicError) {
	s.finOnce.Do(func() {
		s.wg.Wait()

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

// Public Sope, it wraps internal scope and provides the Spawner interface to users.
type Scope struct {
	s    *scope
	root *spawner
	once sync.Once
}

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

	root := &spawner{s: s}
	root.open.Store(true)

	sc := &Scope{
		s:    s,
		root: root,
	}

	return sc, root
}

func (sc *Scope) Wait() error {
	var (
		result   error
		panicErr *PanicError
	)

	sc.once.Do(func() {
		sc.root.close()
		result, panicErr = sc.s.finalize()
	})

	if panicErr != nil {
		panic(panicErr)
	}
	return result
}

func (sc *Scope) Cancel(err error) {
	sc.s.cancel(err)
}

func (sc *Scope) Context() context.Context {
	return sc.s.ctx
}
