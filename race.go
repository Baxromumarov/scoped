package scoped

import (
	"context"
	"fmt"
	"sync"
)

// Race runs all tasks concurrently and returns the result of the first
// task to succeed (return nil error). The contexts of remaining tasks
// are cancelled immediately upon the first success.
//
// If all tasks fail, Race returns the zero value and the last error
// observed. If ctx is cancelled before any task succeeds, Race returns
// ctx.Err().
//
// If tasks is empty, Race returns (zero, nil).
//
// Race panics if any element of tasks is nil.
func Race[T any](
	ctx context.Context,
	tasks ...func(context.Context) (T, error),
) (T, error) {
	var zero T
	if len(tasks) == 0 {
		return zero, nil
	}
	for i, fn := range tasks {
		if fn == nil {
			panic(fmt.Sprintf("scoped: Race task[%d] must not be nil", i))
		}
	}

	raceCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	type result struct {
		val T
		err error
	}

	// Buffered so all goroutines can send without blocking after the
	// first success is picked up.
	ch := make(chan result, len(tasks))

	var wg sync.WaitGroup
	wg.Add(len(tasks))

	for _, fn := range tasks {
		go func() {
			defer wg.Done()
			val, err := fn(raceCtx)
			ch <- result{val: val, err: err}
		}()
	}

	// Close ch once all goroutines are done so the drain loop terminates.
	go func() {
		wg.Wait()
		close(ch)
	}()

	var lastErr error
	for res := range ch {
		if res.err == nil {
			cancel(nil)
			return res.val, nil
		}
		lastErr = res.err
	}

	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	return zero, lastErr
}
