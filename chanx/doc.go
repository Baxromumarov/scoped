// Package chanx provides context-aware, goroutine-safe channel utilities.
//
// Go channels are powerful but have sharp edges: sends to closed channels
// panic, blocked sends leak goroutines, and combining channels with
// context cancellation requires careful select statements.
//
// chanx provides building blocks that handle these concerns:
//
//   - [Send] and [Recv]: context-aware send and receive that unblock on
//     cancellation instead of leaking goroutines.
//   - [SendBatch] and [RecvBatch]: send or receive multiple values in one
//     call, stopping early on cancellation or channel close.
//   - [Merge]: fan-in that combines multiple channels into one.
//   - [FanOut]: distributes values from one channel across N workers.
//   - [Tee]: broadcasts every value to N output channels.
//   - [Map]: transforms values through a function in a pipeline.
//   - [Filter]: passes only values matching a predicate.
//   - [Throttle]: rate-limits a channel to N items per duration.
//   - [Buffer]: collects items into batches by size or timeout.
//   - [First]: returns the first value from any of several channels.
//   - [OrDone]: wraps a channel to respect context cancellation.
//   - [Drain]: discards remaining values to unblock producers.
//   - [Closable]: an idempotent-close channel wrapper that converts
//     send-on-closed panics to errors.
//
// All functions that spawn goroutines tie them to a [context.Context],
// ensuring they terminate when the context is canceled.
package chanx
