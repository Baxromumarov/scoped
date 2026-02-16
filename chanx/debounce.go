package chanx

import (
	"context"
	"time"
)

// Debounce emits the last value received from in after a quiet period
// of duration d. Each new value resets the timer. The output channel
// is closed when in is closed or ctx is cancelled.
//
// Debounce panics if d <= 0.
// If in is nil, returns a closed channel immediately.
func Debounce[T any](
	ctx context.Context,
	in <-chan T,
	d time.Duration,
) <-chan T {
	if d <= 0 {
		panic("chanx: Debounce requires d > 0")
	}
	out := make(chan T)
	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		var timer *time.Timer
		var timerC <-chan time.Time
		var latest T
		var hasValue bool

		for {
			select {
			case v, ok := <-in:
				if !ok {
					// Input closed. Emit last value if pending.
					if hasValue {
						select {
						case out <- latest:
						case <-ctx.Done():
						}
					}
					return
				}
				latest = v
				hasValue = true
				if timer == nil {
					timer = time.NewTimer(d)
					timerC = timer.C
				} else {
					if !timer.Stop() {
						select {
						case <-timerC:
						default:
						}
					}
					timer.Reset(d)
				}
			case <-timerC:
				if hasValue {
					select {
					case out <- latest:
					case <-ctx.Done():
						return
					}
					hasValue = false
					timerC = nil
					timer = nil
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
