package chanx

import "context"

// Map transforms values from in by applying fn and sends the results
// to the returned channel. The output channel is closed when in is
// closed or ctx is cancelled.
//
// If in is nil, returns a closed channel immediately.
func Map[T, U any](ctx context.Context, in <-chan T, fn func(T) U) <-chan U {
	out := make(chan U)

	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- fn(v):
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

// Filter passes values from in to the returned channel only if fn
// returns true. The output channel is closed when in is closed or
// ctx is cancelled.
//
// If in is nil, returns a closed channel immediately.
func Filter[T any](ctx context.Context, in <-chan T, fn func(T) bool) <-chan T {
	out := make(chan T)

	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				if !fn(v) {
					continue
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
