package chanx

import (
	"context"
	"time"
)

// Send sends v to ch, unblocking early if ctx is canceled.
// It returns nil on successful send, or the context error if canceled.
func Send[T any](ctx context.Context, ch chan<- T, v T) error {
	select {
	case ch <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TrySend attempts a non-blocking send of v to ch.
// Returns true if the send succeeded, false if the channel was full or had no receiver.
func TrySend[T any](ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	default:
		return false
	}
}

// SendTimeout sends v to ch with a timeout.
// Returns nil on success, context.DeadlineExceeded on timeout.
func SendTimeout[T any](ch chan<- T, v T, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Send(ctx, ch, v)
}

// Recv receives a value from ch, unblocking early if ctx is canceled.
// It returns the value, a boolean indicating whether the channel is still
// open (false means ch was closed), and any context error.
func Recv[T any](ctx context.Context, ch <-chan T) (T, bool, error) {
	select {
	case v, ok := <-ch:
		return v, ok, nil
	case <-ctx.Done():
		var zero T
		return zero, false, ctx.Err()
	}
}

// TryRecv attempts a non-blocking receive from ch.
// Returns the value, whether the channel is open, and whether a value was received.
// If no value was available, returns (zero, true, false).
// If channel was closed, returns (zero, false, false).
func TryRecv[T any](ch <-chan T) (v T, open bool, received bool) {
	select {
	case v, ok := <-ch:
		return v, ok, ok
	default:
		var zero T
		return zero, true, false
	}
}

// RecvTimeout receives from ch with a timeout.
// Returns the value, whether the channel is open, and any timeout error.
func RecvTimeout[T any](ch <-chan T, timeout time.Duration) (T, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Recv(ctx, ch)
}
