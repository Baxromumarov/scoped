package chanx

import "context"

// Pair holds two values zipped together from two channels.
type Pair[A, B any] struct {
	First  A
	Second B
}

// Zip combines values from two channels pairwise. The output channel
// emits one Pair for each value received from both chA and chB.
// The output is closed when either input is closed or ctx is cancelled.
//
// If either input is nil, the output is closed immediately.
func Zip[A, B any](
	ctx context.Context,
	chA <-chan A,
	chB <-chan B,
) <-chan Pair[A, B] {
	out := make(chan Pair[A, B])

	if chA == nil || chB == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for {
			var a A
			var b B

			select {
			case val, ok := <-chA:
				if !ok {
					return
				}
				a = val
			case <-ctx.Done():
				return
			}

			select {
			case val, ok := <-chB:
				if !ok {
					return
				}
				b = val
			case <-ctx.Done():
				return
			}

			select {
			case out <- Pair[A, B]{
				First:  a,
				Second: b,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}
