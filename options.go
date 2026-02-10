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

type config struct {
	policy     Policy
	limit      int
	panicAsErr bool
	onStart    func(TaskInfo)
	onDone     func(TaskInfo, error, time.Duration)
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
