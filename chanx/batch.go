package chanx

import "context"

// SendBatch sends each value in values to ch, stopping on the first
// context cancellation. It returns nil if all values were sent, or the
// context error if cancelled mid-stream.
//
// SendBatch is a convenience wrapper around [Send].
func SendBatch[T any](ctx context.Context, ch chan<- T, values []T) error {
	for _, v := range values {
		if err := Send(ctx, ch, v); err != nil {
			return err
		}
	}
	return nil
}

// RecvBatch receives up to n values from ch. It returns the collected
// values and nil on success, or the collected values so far and the
// context error if ctx is cancelled. If ch is closed before n values
// are received, it returns the values received so far with a nil error.
//
// RecvBatch panics if n is not positive.
func RecvBatch[T any](ctx context.Context, ch <-chan T, n int) ([]T, error) {
	if n <= 0 {
		panic("chanx: RecvBatch requires n > 0")
	}
	result := make([]T, 0, n)
	for i := 0; i < n; i++ {
		v, ok, err := Recv(ctx, ch)
		if err != nil {
			return result, err
		}
		if !ok {
			return result, nil // channel closed
		}
		result = append(result, v)
	}
	return result, nil
}
