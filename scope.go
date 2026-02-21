// Package scoped provides a mechanism for structured concurrency in Spawn, managing a group of goroutines
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

// runningTaskEntry is an internal entry for task-level tracking.
type runningTaskEntry struct {
	name    string
	started time.Time
}

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
	completed    atomic.Int64
	errored      atomic.Int64
	panicked     atomic.Int64
	cancelled    atomic.Int64

	// Metrics ticker cleanup.
	stopMetrics func()

	// Task-level tracking (populated when trackTasks is true).
	trackTasks   bool
	tasksMu      sync.Mutex
	runningTasks map[uint64]runningTaskEntry
	nextTaskID   uint64

	// Stall detector cleanup.
	stopStall func()
}

// registerTask records a task as running and returns its unique ID.
func (s *scope) registerTask(name string) uint64 {
	s.tasksMu.Lock()
	id := s.nextTaskID
	s.nextTaskID++
	s.runningTasks[id] = runningTaskEntry{name: name, started: time.Now()}
	s.tasksMu.Unlock()
	return id
}

// unregisterTask removes a task from the running set.
func (s *scope) unregisterTask(id uint64) {
	s.tasksMu.Lock()
	delete(s.runningTasks, id)
	s.tasksMu.Unlock()
}

// longestActive returns the duration of the longest-running active task.
// Caller must NOT hold tasksMu.
func (s *scope) longestActive() time.Duration {
	if !s.trackTasks {
		return 0
	}
	now := time.Now()
	var longest time.Duration
	s.tasksMu.Lock()
	for _, entry := range s.runningTasks {
		elapsed := now.Sub(entry.started)
		if elapsed > longest {
			longest = elapsed
		}
	}
	s.tasksMu.Unlock()
	return longest
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

		// Stop metrics ticker if running.
		if s.stopMetrics != nil {
			s.stopMetrics()
		}

		// Stop stall detector if running.
		if s.stopStall != nil {
			s.stopStall()
		}

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

// emitCompletionEvent determines the correct EventKind for a completed task,
// increments metrics counters, and emits the event via the onEvent hook.
func (s *scope) emitCompletionEvent(info TaskInfo, err error, d time.Duration) {
	var kind EventKind

	switch {
	case err == nil:
		kind = EventDone
		s.completed.Add(1)
	case errors.As(err, new(*PanicError)):
		kind = EventPanicked
		s.panicked.Add(1)
	case s.ctx.Err() != nil:
		kind = EventCancelled
		s.cancelled.Add(1)
	default:
		kind = EventErrored
		s.errored.Add(1)
	}

	if s.cfg.onEvent == nil {
		return
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
	finDone  chan struct{} // closed when finalization completes
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

	// Initialize task tracking if enabled.
	if cfg.trackTasks {
		s.trackTasks = true
		s.runningTasks = make(map[uint64]runningTaskEntry)
	}

	// Start metrics ticker if configured.
	if cfg.onMetrics != nil {
		stop := make(chan struct{})
		s.stopMetrics = func() { close(stop) }
		go func() {
			ticker := time.NewTicker(cfg.metricsInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					cfg.onMetrics(Metrics{
						TotalSpawned:  s.totalSpawned.Load(),
						ActiveTasks:   s.activeTasks.Load(),
						Completed:     s.completed.Load(),
						Errored:       s.errored.Load(),
						Panicked:      s.panicked.Load(),
						Cancelled:     s.cancelled.Load(),
						LongestActive: s.longestActive(),
					})
				case <-stop:
					return
				}
			}
		}()
	}

	// Start stall detector if configured.
	if cfg.onStall != nil {
		stop := make(chan struct{})
		s.stopStall = func() { close(stop) }

		checkInterval := max(cfg.stallThreshold/2, 10*time.Millisecond)

		go func() {
			ticker := time.NewTicker(checkInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					now := time.Now()
					s.tasksMu.Lock()
					var stalled []RunningTask
					for _, entry := range s.runningTasks {
						elapsed := now.Sub(entry.started)
						if elapsed >= cfg.stallThreshold {
							stalled = append(stalled, RunningTask{
								Name:    entry.name,
								Started: entry.started,
								Elapsed: elapsed,
							})
						}
					}
					s.tasksMu.Unlock()

					for _, rt := range stalled {
						cfg.onStall(rt)
					}
				case <-stop:
					return
				}
			}
		}()
	}

	root := &spawner{
		s:    s,
		open: true,
	}

	sc := &Scope{
		s:       s,
		root:    root,
		finDone: make(chan struct{}),
	}

	return sc, root
}

// doFinalize triggers scope finalization exactly once and signals finDone.
func (sc *Scope) doFinalize() {
	sc.once.Do(func() {
		sc.root.close()
		sc.result, sc.panicVal = sc.s.finalize()
		close(sc.finDone)
	})
}

// Wait closes the root [Spawner], waits for all spawned tasks to complete,
// and returns the aggregated error. If a task panicked and [WithPanicAsError]
// was not set, Wait re-panics with the captured [*PanicError].
//
// Wait is idempotent; subsequent calls return the same result.
func (sc *Scope) Wait() error {
	sc.doFinalize()

	if sc.panicVal != nil {
		panic(sc.panicVal)
	}
	return sc.result
}

// WaitTimeout is like [Wait] but returns [context.DeadlineExceeded] if the
// timeout expires before all tasks finish. The scope is NOT cancelled on
// timeout — call [Scope.Cancel] explicitly if needed.
//
// WaitTimeout is safe to call alongside [Wait]; finalization runs at most once.
func (sc *Scope) WaitTimeout(d time.Duration) error {
	// Trigger finalization in a goroutine so we can select on timeout.
	go sc.doFinalize()

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-sc.finDone:
		if sc.panicVal != nil {
			panic(sc.panicVal)
		}
		return sc.result
	case <-t.C:
		return context.DeadlineExceeded
	}
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

// Snapshot returns a point-in-time view of the scope's state.
// If task tracking is not enabled (via [WithStallDetector] or [WithTaskTracking]),
// RunningTasks will be nil and LongestActive will be zero.
func (sc *Scope) Snapshot() ScopeSnapshot {
	s := sc.s
	snap := ScopeSnapshot{
		Metrics: Metrics{
			TotalSpawned:  s.totalSpawned.Load(),
			ActiveTasks:   s.activeTasks.Load(),
			Completed:     s.completed.Load(),
			Errored:       s.errored.Load(),
			Panicked:      s.panicked.Load(),
			Cancelled:     s.cancelled.Load(),
			LongestActive: s.longestActive(),
		},
	}

	if s.trackTasks {
		now := time.Now()
		s.tasksMu.Lock()
		snap.RunningTasks = make([]RunningTask, 0, len(s.runningTasks))
		for _, entry := range s.runningTasks {
			elapsed := now.Sub(entry.started)
			if elapsed > snap.LongestActive {
				snap.LongestActive = elapsed
			}
			snap.RunningTasks = append(snap.RunningTasks, RunningTask{
				Name:    entry.name,
				Started: entry.started,
				Elapsed: elapsed,
			})
		}
		s.tasksMu.Unlock()
	}

	return snap
}
