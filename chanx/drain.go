package chanx

import "context"

// OrDone wraps a receive channel so that it respects context cancellation.
// The returned channel yields values from in until in is closed or ctx
// is cancelled, whichever comes first.
//
// The internal goroutine exits promptly on cancellation, preventing leaks.
func OrDone[T any](ctx context.Context, in <-chan T) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Drain reads and discards all values from ch until it is closed.
// Use this to unblock a producer that is sending to a channel during
// shutdown.
func Drain[T any](ch <-chan T) {
	for range ch {
	}
}
