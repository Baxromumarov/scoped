package scoped

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
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

	// Observability counters.
	submitted atomic.Int64
	completed atomic.Int64
	errored   atomic.Int64
	inFlight  atomic.Int64
	workers   int
}

// PoolStats provides a point-in-time snapshot of pool activity.
type PoolStats struct {
	Submitted  int64 // total tasks submitted
	Completed  int64 // tasks finished (success + error)
	Errored    int64 // tasks that returned non-nil error
	InFlight   int64 // tasks currently executing
	QueueDepth int   // tasks waiting in the queue
	Workers    int   // worker count (fixed at creation)
}

// PoolOption configures a [Pool].
type PoolOption func(*poolConfig)

type poolConfig struct {
	queueSize       int
	onMetrics       func(PoolStats)
	metricsInterval time.Duration
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

// WithPoolMetrics registers a periodic pool metrics callback that fires
// every interval. The callback receives a snapshot of current pool counters.
//
// Panics if interval <= 0 or fn is nil.
func WithPoolMetrics(interval time.Duration, fn func(PoolStats)) PoolOption {
	if interval <= 0 {
		panic("scoped: WithPoolMetrics requires interval > 0")
	}
	if fn == nil {
		panic("scoped: WithPoolMetrics requires non-nil callback")
	}
	return func(c *poolConfig) {
		c.onMetrics = fn
		c.metricsInterval = interval
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
		tasks:   make(chan func() error, cfg.queueSize),
		ctx:     ctx,
		cancel:  cancel,
		workers: n,
	}

	p.wg.Add(n)
	for range n {
		go p.worker()
	}

	// Start metrics ticker if configured.
	if cfg.onMetrics != nil {
		go func() {
			ticker := time.NewTicker(cfg.metricsInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if p.closed.Load() {
						return
					}
					cfg.onMetrics(p.Stats())
				case <-ctx.Done():
					return
				}
			}
		}()
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
	p.inFlight.Add(1)
	defer func() {
		p.inFlight.Add(-1)
		p.completed.Add(1)
	}()

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
		p.errored.Add(1)
		p.errMu.Lock()
		p.errs = append(p.errs, err)
		p.errMu.Unlock()
	}
}

// Stats returns a point-in-time snapshot of pool activity.
// Safe to call concurrently.
func (p *Pool) Stats() PoolStats {
	return PoolStats{
		Submitted:  p.submitted.Load(),
		Completed:  p.completed.Load(),
		Errored:    p.errored.Load(),
		InFlight:   p.inFlight.Load(),
		QueueDepth: len(p.tasks),
		Workers:    p.workers,
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
		p.submitted.Add(1)
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
		p.submitted.Add(1)
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
