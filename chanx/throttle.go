package chanx

import (
	"context"
	"time"
)

// Throttle rate-limits values from in to at most n items per duration.
// It uses a token-bucket approach: n tokens are available initially,
// and one token is replenished every per/n interval. The output channel
// is closed when in is closed or ctx is cancelled.
//
// Throttle panics if n is not positive or per is not positive.
// If in is nil, returns a closed channel immediately.
func Throttle[T any](ctx context.Context, in <-chan T, n int, per time.Duration) <-chan T {
	if n <= 0 {
		panic("chanx: Throttle requires n > 0")
	}
	if per <= 0 {
		panic("chanx: Throttle requires per > 0")
	}

	out := make(chan T)

	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)

		interval := per / time.Duration(n)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		tokens := n // start with full bucket for initial burst
		for {
			if tokens == 0 {
				// Wait for a token refill.
				select {
				case <-ticker.C:
					tokens++
				case <-ctx.Done():
					return
				}
				continue
			}

			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				tokens--
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			case <-ticker.C:
				if tokens < n {
					tokens++
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
