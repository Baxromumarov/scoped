// Package scoped provides structured concurrency primitives for Go.
//
// Structured concurrency ensures that concurrent tasks have well-defined
// lifecycles: they are spawned and joined within a clear scope, preventing
// goroutine leaks, orphaned tasks, and unpredictable control flow.
//
// # Running Tasks
//
// The primary entry point is [Run], which creates a scope, executes a
// function that spawns tasks via [Spawner], and waits for all tasks to
// complete before returning:
//
//	err := scoped.Run(ctx, func(sp scoped.Spawner) {
//	    sp.Go("fetch", func(ctx context.Context) error {
//	        return fetch(ctx)
//	    })
//	    sp.Spawn("process", func(ctx context.Context, sub scoped.Spawner) error {
//	        sub.Go("step-1", step1)
//	        return nil
//	    })
//	})
//
// Use [Spawner.Go] for simple tasks and [Spawner.Spawn] when the task
// needs to spawn sub-tasks of its own.
//
// For manual lifecycle control, [New] returns a [Scope] and root [Spawner]
// separately. The caller must call [Scope.Wait] to finalize.
// [Scope.WaitTimeout] adds a deadline to finalization.
//
// # Error Policies
//
// Error policies control how the scope reacts to task failures:
//
//   - [FailFast] (default): the first error cancels all sibling tasks.
//     [Scope.Wait] returns that first error.
//   - [Collect]: all errors are collected without cancelling siblings.
//     [Scope.Wait] returns all errors joined via [errors.Join].
//     Use [WithMaxErrors] to cap stored errors in high-volume scenarios.
//
// All task errors are wrapped in [*TaskError] for attribution. Use
// [IsTaskError], [TaskOf], [CauseOf], and [AllTaskErrors] to inspect them.
//
// # Helpers
//
// Convenience functions for common patterns:
//
//   - [ForEachSlice]: apply a function to every item in a slice concurrently.
//   - [MapSlice]: transform every item concurrently, preserving order.
//   - [SpawnResult]: spawn a task that returns a typed value via [Result].
//   - [SpawnTimeout]: spawn a task with a per-task deadline.
//   - [SpawnRetry]: spawn a task with exponential-backoff retries.
//   - [SpawnScope]: spawn a sub-scope as a single task, allowing
//     hierarchical error handling with independent policies.
//   - [Race]: run multiple tasks concurrently and return the first
//     successful result, cancelling the rest.
//
// # Bounded Concurrency
//
// Use [WithLimit] to restrict the number of goroutines executing
// concurrently within a scope. Tasks beyond the limit wait for a slot,
// respecting context cancellation while waiting.
//
// For standalone use outside scopes, [Semaphore] provides a weighted
// semaphore with [Semaphore.Acquire], [Semaphore.TryAcquire], and
// [Semaphore.Release].
//
// # Worker Pool
//
// [Pool] provides a reusable fixed-size worker pool. Tasks are submitted
// via [Pool.Submit] (blocking) or [Pool.TrySubmit] (non-blocking) and
// processed by a fixed number of goroutines. Call [Pool.Close] to drain
// the queue and collect errors.
//
// # Panic Recovery
//
// By default, a panic in any task is captured with its full stack trace
// and re-raised in [Scope.Wait]. Use [WithPanicAsError] to convert panics
// to [*PanicError] values and return them as regular errors instead.
//
// # Panic Contract
//
// This package intentionally panics for programmer misuse (invalid options,
// nil required callbacks, invalid lifecycle calls such as spawning after
// shutdown, or concurrent [Stream.Next] calls). These panics enforce API
// invariants and are considered contract violations, not recoverable runtime
// data errors.
//
// # Observability
//
// Register hooks for task lifecycle events:
//
//   - [WithOnStart]: called when each task begins executing.
//   - [WithOnDone]: called when each task finishes, with error and duration.
//   - [WithOnEvent]: unified hook receiving [TaskEvent] for every state
//     change (started, done, errored, panicked, cancelled).
//   - [WithOnMetrics]: periodic [Metrics] snapshots with counters for
//     spawned, active, completed, errored, panicked, and cancelled tasks.
//   - [WithTaskTracking]: enables per-task tracking so [Scope.Snapshot]
//     includes [RunningTask] entries and [ScopeSnapshot.LongestActive].
//   - [WithStallDetector]: periodic check for tasks exceeding a duration
//     threshold, calling a callback for each stalled task. Purely
//     observational — does not cancel stalled tasks.
//
// [Pool] exposes [Pool.Stats] returning a [PoolStats] snapshot, and
// [WithPoolMetrics] for periodic pool metrics callbacks.
//
// [Stream] tracks items read, errors, and timing automatically.
// [Stream.Stats] returns a [StreamStats] snapshot including throughput.
// [Observe] wraps a stream with a per-item [StreamEvent] callback.
//
// # Streams
//
// [Stream] provides a pull-based, composable data pipeline. Create streams
// with [NewStream], [FromSlice], [FromSliceRef], [FromChan], [Empty],
// [Repeat], or [Generate]. Chains of [Stream.Filter], [Stream.Take],
// [Stream.Skip], [Stream.Peek], [Stream.TakeWhile], [Stream.DropWhile],
// [Map], [Batch], [FlatMap], [Distinct], [Scan], and [Zip] are evaluated
// lazily. [Reduce] folds a stream into a single value.
//
// [ParallelMap] processes items concurrently with optional ordering and
// backpressure via [StreamOptions.MaxPending].
//
// Terminal methods ([Stream.ToSlice], [Stream.ForEach], [Stream.Count])
// return partial results alongside any error, following [io.Reader]
// conventions. [Stream.ToChanScope] bridges a stream to a channel within
// a scope. Stream errors are aggregated via [errors.Join].
//
// Streams are single-consumer; concurrent [Stream.Next] calls panic.
//
// # Spawner Lifetime
//
// Each task function receives a child [Spawner] that is valid only for the
// duration of the task. Storing the child Spawner and calling Spawn on it
// after the task returns will panic. This is by design: structured
// concurrency requires that all child tasks are scoped to their parent.
//
// # Channel Utilities
//
// The [github.com/baxromumarov/scoped/chanx] subpackage provides
// context-aware channel operations (Send, Recv, TrySend, TryRecv,
// SendTimeout, RecvTimeout, SendBatch, RecvBatch), fan-in/fan-out
// patterns (Merge, Tee, FanOut, Broadcast), transformation pipelines
// (Map, Filter), rate limiting (Throttle), batching (Buffer,
// BufferWithReason), timing (Debounce, Window), combining (Zip, First),
// and an idempotent-close channel wrapper (Closable).
package scoped
