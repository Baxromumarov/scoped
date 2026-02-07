package chanx

import (
	"context"
	"time"
)

// Buffer collects values from in into slices of up to size elements.
// A batch is emitted when it reaches size elements or when timeout
// elapses since the first item in the current batch, whichever comes
// first. The output channel is closed when in is closed or ctx is
// cancelled. Any partial batch is flushed on channel close.
//
// Buffer panics if size is not positive or timeout is not positive.
// If in is nil, returns a closed channel immediately.
func Buffer[T any](ctx context.Context, in <-chan T, size int, timeout time.Duration) <-chan []T {
	if size <= 0 {
		panic("chanx: Buffer requires size > 0")
	}
	if timeout <= 0 {
		panic("chanx: Buffer requires timeout > 0")
	}

	out := make(chan []T)

	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)

		batch := make([]T, 0, size)
		var timer *time.Timer
		var timerC <-chan time.Time // nil until first item in batch

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			select {
			case out <- batch:
			case <-ctx.Done():
				return false
			}
			batch = make([]T, 0, size)
			if timer != nil {
				timer.Stop()
				timerC = nil
			}
			return true
		}

		defer func() {
			// Flush any partial batch on exit.
			// The reader is still draining out (close(out) runs after this defer),
			// so this send will succeed unless context was cancelled.
			if len(batch) > 0 {
				select {
				case out <- batch:
				case <-ctx.Done():
				}
			}
		}()

		for {
			select {
			case v, ok := <-in:
				if !ok {
					return // defer flushes partial batch
				}
				batch = append(batch, v)
				if len(batch) == 1 {
					// Start timer on first item in batch.
					timer = time.NewTimer(timeout)
					timerC = timer.C
				}
				if len(batch) >= size {
					if !flush() {
						return
					}
				}
			case <-timerC:
				if !flush() {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
