package chanx

import "context"

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
