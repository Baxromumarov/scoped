// Package chanx provides context-aware, goroutine-safe channel utilities.
//
// Go channels are powerful but have sharp edges: sends to closed channels
// panic, blocked sends leak goroutines, and combining channels with
// context cancellation requires careful select statements.
//
// chanx provides building blocks that handle these concerns:
//
// # Send and Receive
//
//   - [Send] and [Recv]: context-aware send and receive that unblock on
//     cancellation instead of leaking goroutines.
//   - [TrySend] and [TryRecv]: non-blocking send and receive.
//   - [SendTimeout] and [RecvTimeout]: send and receive with a deadline.
//   - [SendBatch] and [RecvBatch]: send or receive multiple values in one
//     call, stopping early on cancellation or channel close.
//
// # Fan-in, Fan-out, and Broadcasting
//
//   - [Merge]: fan-in that combines multiple channels into one.
//   - [FanOut]: distributes values from one channel across N workers.
//   - [Tee]: broadcasts every value to N unbuffered output channels.
//     All consumers must read concurrently or the sender blocks.
//   - [Broadcast]: like Tee but with buffered outputs, allowing slow
//     consumers to lag behind up to bufSize items.
//
// # Transformation and Filtering
//
//   - [Map]: transforms values through a function in a pipeline.
//   - [Filter]: passes only values matching a predicate.
//   - [Take]: forwards the first n items then closes.
//   - [Skip]: drops the first n items then forwards the rest.
//   - [Scan]: emits running accumulations of input values.
//   - [Partition]: splits a channel into two by predicate (match and rest).
//     Both outputs must be read concurrently to avoid deadlock.
//
// # Rate Limiting and Batching
//
//   - [Throttle]: rate-limits a channel using a token-bucket algorithm.
//   - [Buffer]: collects items into batches by size or timeout.
//   - [BufferWithReason]: like Buffer but each batch includes a
//     [FlushReason] indicating why it was flushed (size, timeout, or
//     channel close).
//
// # Timing
//
//   - [Debounce]: emits the most recent value after a quiet period,
//     suppressing bursts of rapid updates.
//   - [Window]: groups items into time-based windows. Supports [Tumbling]
//     (non-overlapping fixed intervals) and [Sliding] (timestamp-based
//     duration windows) modes.
//
// # Combining and Selection
//
//   - [Zip]: pairs values from two channels into [Pair] structs, stopping
//     when either channel closes.
//   - [First]: returns the first value from any of several channels.
//
// # Lifecycle
//
//   - [OrDone]: wraps a channel to respect context cancellation.
//   - [Drain]: discards remaining values to unblock producers.
//   - [Closable]: an idempotent-close channel wrapper that converts
//     send-on-closed panics to [ErrClosed] errors.
//
// All functions that spawn goroutines tie them to a [context.Context],
// ensuring they terminate when the context is cancelled.
package chanx
