package chanx

import "context"

// Broadcast is a buffered variant of [Tee] that reduces slow-consumer
// blocking. Each output channel has an independent buffer of bufSize.
//
// If bufSize <= 0 or n <= 0, Broadcast panics.
// If in is nil, all outputs are closed immediately.
func Broadcast[T any](
	ctx context.Context,
	in <-chan T,
	n int,
	bufSize int,
) []<-chan T {
	if n <= 0 {
		panic("chanx: Broadcast requires n > 0")
	}
	if bufSize <= 0 {
		panic("chanx: Broadcast requires bufSize > 0")
	}

	outs := make([]chan T, n)
	for i := range outs {
		outs[i] = make(chan T, bufSize)
	}

	if in == nil {
		for _, ch := range outs {
			close(ch)
		}
		result := make([]<-chan T, n)
		for i, ch := range outs {
			result[i] = ch
		}
		return result
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
