// Package scoped provides structured concurrency primitives for Go.
//
// Structured concurrency ensures that concurrent tasks have well-defined
// lifecycles: they are spawned and joined within a clear scope, preventing
// goroutine leaks, orphaned tasks, and unpredictable control flow.
//
// The primary entry point is [Run], which creates a [Scope], executes a
// function that spawns tasks via [Scope.Go], and waits for all tasks to
// complete before returning:
//
//	err := scoped.Run(ctx, func(s *scoped.Scope) {
//	    s.Go("fetch", func(ctx context.Context) error {
//	        return fetch(ctx)
//	    })
//	    s.Go("process", func(ctx context.Context) error {
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
// context-aware channel operations (Send, Recv), fan-in/fan-out patterns
// (Merge, Tee, FanOut), and an idempotent-close channel wrapper (Closable).
package scoped
