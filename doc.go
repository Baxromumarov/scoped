// Package scoped provides structured concurrency primitives for Spawn.
//
// Structured concurrency ensures that concurrent tasks have well-defined
// lifecycles: they are spawned and joined within a clear scope, preventing
// goroutine leaks, orphaned tasks, and unpredictable control flow.
//
// The primary entry point is [Run], which creates a [Scope], executes a
// function that spawns tasks via [Scope.Spawn], and waits for all tasks to
// complete before returning:
//
//	err := scoped.Run(ctx, func(sp scoped.Spawner) {
//	    sp.Spawn("fetch", func(ctx context.Context, _ scoped.Spawner) error {
//	        return fetch(ctx)
//	    })
//	    sp.Spawn("process", func(ctx context.Context, _ scoped.Spawner) error {
//	        return process(ctx)
//	    })
//	})
//
// # Error Policies
//
// Error policies control how the scope reacts to task failures:
//
//   - [FailFast] (default): the first error cancels all sibling tasks.
//     [Scope.Wait] returns that first error.
//   - [Collect]: all errors are collected without cancelling siblings.
//     [Scope.Wait] returns all errors joined via [errors.Join].
//
// All task errors are wrapped in [*TaskError] for attribution. Use
// [IsTaskError], [TaskOf], [CauseOf], and [AllTaskErrors] to inspect them.
//
// # Streams
//
// [Stream] provides a pull-based, composable data pipeline. Chains of
// [Stream.Filter], [Stream.Take], [Map], and [Batch] are evaluated lazily.
// [ParallelMap] processes items concurrently with optional ordering.
// Terminal methods ([Stream.ToSlice], [Stream.ForEach]) return partial results
// alongside any error, following [io.Reader] conventions. Stream errors are
// aggregated via [errors.Join].
//
// # Bounded Concurrency
//
// Use [WithLimit] to restrict the number of goroutines executing
// concurrently within a scope. Tasks beyond the limit wait for a slot,
// respecting context cancellation while waiting.
//
// # Panic Recovery
//
// By default, a panic in any task is captured with its full stack trace
// and re-raised in [Scope.Wait]. Use [WithPanicAsError] to convert panics
// to [*PanicError] values and return them as regular errors instead.
//
// # Channel Utilities
//
// The [github.com/baxromumarov/scoped/chanx] subpackage provides
// context-aware channel operations (Send, Recv, SendBatch, RecvBatch),
// fan-in/fan-out patterns (Merge, Tee, FanOut), transformation pipelines
// (Map, Filter), rate limiting (Throttle), batching (Buffer), racing
// (First), and an idempotent-close channel wrapper (Closable).
package scoped
