package chanx

import (
	"context"
	"reflect"
)

// First returns a channel that delivers the first value received from
// any of the input channels, then closes. If no channels are provided
// or all are nil, the returned channel is closed immediately. If ctx
// is cancelled before any value arrives, the returned channel is closed
// with no value.
//
// Note: First uses [reflect.Select] internally, which has higher
// overhead than a direct select statement. This is acceptable because
// First performs exactly one selection.
func First[T any](ctx context.Context, chs ...<-chan T) <-chan T {
	out := make(chan T, 1) // buffer 1 so the goroutine never blocks on send

	// Filter out nil channels.
	valid := make([]<-chan T, 0, len(chs))
	for _, ch := range chs {
		if ch != nil {
			valid = append(valid, ch)
		}
	}

	if len(valid) == 0 {
		close(out)
		return out
	}

	go func() {
		defer close(out)

		// Build select cases dynamically.
		cases := make([]reflect.SelectCase, len(valid)+1)
		for i, ch := range valid {
			cases[i] = reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(ch),
			}
		}
		// Last case: ctx.Done().
		cases[len(valid)] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ctx.Done()),
		}

		chosen, value, ok := reflect.Select(cases)
		if chosen == len(valid) {
			// Context cancelled.
			return
		}
		if ok {
			out <- value.Interface().(T)
		}
	}()
	return out
}
