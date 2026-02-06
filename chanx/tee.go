package chanx

import "context"

// Tee broadcasts every value from in to n independent output channels.
// All outputs receive every value. The output channels are closed when
// in is closed or the context is cancelled.
//
// Warning: if any consumer is slow, it blocks the broadcast to all others.
// Use buffered consumers or [OrDone] to mitigate this.
// Tee panics if n is not positive.
func Tee[T any](ctx context.Context, in <-chan T, n int) []<-chan T {
	if n <= 0 {
		panic("chanx: Tee requires n > 0")
	}

	outs := make([]chan T, n)
	for i := range outs {
		outs[i] = make(chan T)
	}

	go func() {
		defer func() {
			for _, ch := range outs {
				close(ch)
			}
		}()
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				for _, ch := range outs {
					select {
					case ch <- v:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	result := make([]<-chan T, n)
	for i, ch := range outs {
		result[i] = ch
	}
	return result
}
