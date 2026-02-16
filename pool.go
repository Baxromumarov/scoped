package scoped

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrPoolClosed is returned by [Pool.Submit] when the pool has been closed.
var ErrPoolClosed = errors.New("scoped: pool is closed")

// Pool is a reusable worker pool. Tasks are submitted via Submit and
// processed by a fixed number of worker goroutines.
type Pool struct {
	tasks  chan func() error
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool

	errMu sync.Mutex
	errs  []error
}

// PoolOption configures a [Pool].
type PoolOption func(*poolConfig)

type poolConfig struct {
	queueSize int
}

// WithQueueSize sets the task queue buffer size. Default is n * 2.
func WithQueueSize(size int) PoolOption {
	return func(c *poolConfig) {
		if size < 0 {
			panic("scoped: WithQueueSize requires non-negative size")
		}
		c.queueSize = size
	}
}

// NewPool creates a pool with n worker goroutines.
// Workers start immediately and process tasks until [Pool.Close] is called.
// Panics if n <= 0.
func NewPool(
	ctx context.Context,
	n int,
	opts ...PoolOption,
) *Pool {
	if n <= 0 {
		panic("scoped: NewPool requires n > 0")
	}

	cfg := poolConfig{queueSize: n * 2}
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, cancel := context.WithCancel(ctx)
	p := &Pool{
		tasks:  make(chan func() error, cfg.queueSize),
		ctx:    ctx,
		cancel: cancel,
	}

	p.wg.Add(n)
	for range n {
		go p.worker()
	}

	return p
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for fn := range p.tasks {
		p.runTask(fn)
	}
}

func (p *Pool) runTask(fn func() error) {
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = newPanicError(r)
			}
		}()
		err = fn()
	}()
	if err != nil {
		p.errMu.Lock()
		p.errs = append(p.errs, err)
		p.errMu.Unlock()
	}
}

// Submit submits a task to the pool. It blocks if the queue is full.
// Returns [ErrPoolClosed] if the pool has been closed.
// Returns ctx.Err() if the pool's context is cancelled.
func (p *Pool) Submit(fn func() error) (err error) {
	if p.closed.Load() {
		return ErrPoolClosed
	}

	// Guard against the race between the closed check above and
	// Close() closing the tasks channel. If Close fires between the
	// check and the send, the send panics; we recover and return
	// ErrPoolClosed.
	defer func() {
		if r := recover(); r != nil {
			err = ErrPoolClosed
		}
	}()

	select {
	case p.tasks <- fn:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// TrySubmit attempts to submit without blocking.
// Returns false if the queue is full or the pool is closed.
func (p *Pool) TrySubmit(fn func() error) (submitted bool) {
	if p.closed.Load() {
		return false
	}

	// Same TOCTOU guard as Submit.
	defer func() {
		if r := recover(); r != nil {
			submitted = false
		}
	}()

	select {
	case p.tasks <- fn:
		return true
	default:
		return false
	}
}

// Close stops accepting new tasks and waits for in-flight tasks to finish.
// Returns the joined errors from all failed tasks.
// Safe to call multiple times; subsequent calls return the same result.
func (p *Pool) Close() error {
	if p.closed.CompareAndSwap(false, true) {
		close(p.tasks)
	}
	p.wg.Wait()
	p.cancel()

	p.errMu.Lock()
	defer p.errMu.Unlock()
	return errors.Join(p.errs...)
}
