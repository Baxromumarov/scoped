package scoped

import "time"

// Policy determines how a [Scope] handles errors from child tasks.
type Policy int

const (
	// FailFast cancels all sibling tasks when the first error occurs.
	// [Scope.Wait] returns the first error encountered.
	FailFast Policy = iota

	// Collect gathers all errors without cancelling siblings.
	// [Scope.Wait] returns all errors joined via [errors.Join].
	Collect
)

// TaskInfo provides metadata about a running task.
// It is passed to observability hooks registered via [WithOnStart] and [WithOnDone].
type TaskInfo struct {
	Name string
}

// EventKind identifies the lifecycle stage of a [TaskEvent].
type EventKind int

const (
	// EventStarted is emitted when a task begins executing.
	EventStarted EventKind = iota
	// EventDone is emitted when a task completes successfully (Err is nil).
	EventDone
	// EventErrored is emitted when a task returns a non-nil error.
	EventErrored
	// EventPanicked is emitted when a task panics.
	EventPanicked
	// EventCancelled is emitted when a task's context was cancelled
	// before or during execution.
	EventCancelled
)

func (k EventKind) String() string {
	switch k {
	case EventStarted:
		return "started"
	case EventDone:
		return "done"
	case EventErrored:
		return "errored"
	case EventPanicked:
		return "panicked"
	case EventCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// TaskEvent describes a lifecycle event for a task. It is passed to the
// callback registered via [WithOnEvent].
type TaskEvent struct {
	Kind     EventKind
	Task     TaskInfo
	Err      error         // non-nil for EventErrored and EventPanicked
	Duration time.Duration // wall-clock time; zero for EventStarted
}

// Metrics provides aggregated counters for a scope's lifecycle.
type Metrics struct {
	TotalSpawned  int64
	ActiveTasks   int64
	Completed     int64
	Errored       int64
	Panicked      int64
	Cancelled     int64
	LongestActive time.Duration // zero if task tracking is not enabled
}

// RunningTask describes a task currently executing within a scope.
type RunningTask struct {
	Name    string
	Started time.Time
	Elapsed time.Duration // populated at snapshot time
}

// ScopeSnapshot is a point-in-time view of a scope's state.
type ScopeSnapshot struct {
	Metrics       Metrics
	RunningTasks  []RunningTask
	LongestActive time.Duration
}

type config struct {
	policy          Policy
	limit           int
	maxErrors       int // 0 = unlimited (default)
	panicAsErr      bool
	onStart         func(TaskInfo)
	onDone          func(TaskInfo, error, time.Duration)
	onEvent         func(TaskEvent)
	onMetrics       func(Metrics)
	metricsInterval time.Duration
	trackTasks      bool
	stallThreshold  time.Duration
	onStall         func(RunningTask)
}

// Option configures a [Scope].
type Option func(*config)

func defaultConfig() config {
	return config{
		policy: FailFast,
	}
}

// WithPolicy sets the error handling policy for the scope.
// It panics if p is not a known Policy value.
func WithPolicy(p Policy) Option {
	return func(c *config) {
		switch p {
		case FailFast, Collect:
			c.policy = p
		default:
			panic("scoped: invalid policy")
		}
	}
}

// WithLimit sets the maximum number of goroutines that can execute
// concurrently within the scope. Tasks beyond the limit block until
// a slot becomes available or the context is canceled.
//
// A limit of zero (the default) means unlimited concurrency.
// WithLimit panics if n is negative.
func WithLimit(n int) Option {
	return func(c *config) {
		if n < 0 {
			panic("scoped: limit must be non-negative")
		}
		c.limit = n
	}
}

// WithMaxErrors sets the maximum number of errors stored in [Collect] mode.
// When the limit is reached, subsequent errors are still counted but not stored,
// preventing unbounded memory growth in high-volume scenarios.
//
// A value of zero (the default) means unlimited error collection.
// This option has no effect in [FailFast] mode.
// WithMaxErrors panics if n is negative.
func WithMaxErrors(n int) Option {
	return func(c *config) {
		if n < 0 {
			panic("scoped: maxErrors must be non-negative")
		}
		c.maxErrors = n
	}
}

// WithPanicAsError converts panics in child tasks to [*PanicError]
// values returned as regular errors, instead of re-raising them
// in [Scope.Wait].
func WithPanicAsError() Option {
	return func(c *config) {
		c.panicAsErr = true
	}
}

// WithOnStart registers a hook invoked when each task begins executing.
// The hook runs inside the task's goroutine before the task function.
func WithOnStart(fn func(TaskInfo)) Option {
	return func(c *config) {
		c.onStart = fn
	}
}

// WithOnDone registers a hook invoked when each task finishes.
// The hook receives the task's error (nil on success) and wall-clock duration.
// The hook runs inside the task's goroutine after the task function returns.
func WithOnDone(fn func(TaskInfo, error, time.Duration)) Option {
	return func(c *config) {
		c.onDone = fn
	}
}

// WithOnEvent registers a unified lifecycle hook that receives a [TaskEvent]
// for every task state change: started, done, errored, panicked, and cancelled.
// The hook runs inside the task's goroutine.
//
// WithOnEvent can be used alongside [WithOnStart] and [WithOnDone]; all
// registered hooks will fire.
func WithOnEvent(fn func(TaskEvent)) Option {
	return func(c *config) {
		c.onEvent = fn
	}
}

// WithOnMetrics registers a periodic metrics callback that fires every interval.
// The callback receives a snapshot of current scope counters.
//
// Panics if interval <= 0 or fn is nil.
func WithOnMetrics(interval time.Duration, fn func(Metrics)) Option {
	if interval <= 0 {
		panic("scoped: WithOnMetrics requires interval > 0")
	}
	if fn == nil {
		panic("scoped: WithOnMetrics requires non-nil callback")
	}
	return func(c *config) {
		c.onMetrics = fn
		c.metricsInterval = interval
	}
}

// WithTaskTracking enables per-task tracking so that [Scope.Snapshot]
// includes [RunningTask] entries and [ScopeSnapshot.LongestActive] duration.
// This has a small overhead (mutex acquisition per task start/end).
func WithTaskTracking() Option {
	return func(c *config) {
		c.trackTasks = true
	}
}

// WithStallDetector registers a periodic check for tasks running longer than
// threshold. The callback receives each stalled task with its elapsed duration.
// The check runs every threshold/2 interval (minimum 10ms).
//
// This is purely observational — unlike [SpawnTimeout], it does not cancel
// stalled tasks. Implicitly enables task tracking (see [WithTaskTracking]).
//
// Panics if threshold <= 0 or fn is nil.
func WithStallDetector(threshold time.Duration, fn func(RunningTask)) Option {
	if threshold <= 0 {
		panic("scoped: WithStallDetector requires threshold > 0")
	}
	if fn == nil {
		panic("scoped: WithStallDetector requires non-nil callback")
	}
	return func(c *config) {
		c.stallThreshold = threshold
		c.onStall = fn
		c.trackTasks = true
	}
}
