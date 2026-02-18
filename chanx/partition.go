package chanx

import "context"

// Partition splits items from in into two channels based on fn.
// Items for which fn returns true go to the first (match) channel;
// items for which fn returns false go to the second (rest) channel.
// Both output channels are closed when in is closed or ctx is cancelled.
//
// IMPORTANT: Callers MUST consume both output channels concurrently
// (typically in separate goroutines). If only one channel is read, the
// single dispatcher goroutine will block on the unconsumed channel,
// causing a deadlock. This is the same constraint as [Tee].
//
// If in is nil, both output channels are closed immediately.
// Panics if fn is nil.
func Partition[T any](
	ctx context.Context,
	in <-chan T,
	fn func(T) bool,
) (match <-chan T, rest <-chan T) {
	if fn == nil {
		panic("chanx: Partition requires non-nil predicate")
	}
	matchCh := make(chan T)
	restCh := make(chan T)

	if in == nil {
		close(matchCh)
		close(restCh)
		return matchCh, restCh
	}

	go func() {
		defer close(matchCh)
		defer close(restCh)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				if fn(v) {
					select {
					case matchCh <- v:
					case <-ctx.Done():
						return
					}
				} else {
					select {
					case restCh <- v:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return matchCh, restCh
}
