package scoped

import "context"

// Result holds the outcome of an asynchronous task that produces a typed
// value. Create one via [GoResult].
type Result[T any] struct {
	val  T
	err  error
	done chan struct{}
}

// GoResult spawns a named task that returns a typed value and wraps the
// outcome in a [Result]. The task runs within the given [Scope], inheriting
// its lifecycle and error policy.
//
//	r := scoped.GoResult(s, "compute", func(ctx context.Context) (int, error) {
//	    return expensiveCalc(ctx)
//	})
//	val, err := r.Wait()
func GoResult[T any](
	sp Spawner,
	name string,
	fn func(ctx context.Context) (T, error),
) *Result[T] {
	r := &Result[T]{
		done: make(chan struct{}),
	}
	sp.Go(name, func(ctx context.Context, _ Spawner) error {
		defer close(r.done)
		defer func() {
			if rec := recover(); rec != nil {
				r.err = newPanicError(rec)
				panic(rec) // re-panic for Scope to handle
			}
		}()
		v, err := fn(ctx)
		r.val = v
		r.err = err
		return err
	})
	return r
}

// Wait blocks until the task completes or the scope's context is canceled.
// It returns the task's value and error.
//
// Note: Since Spawner does not expose the scope's context, this Wait
// only waits for the task to complete.
func (r *Result[T]) Wait() (T, error) {
	<-r.done
	return r.val, r.err
}

// Done returns a channel that is closed when the task completes.
func (r *Result[T]) Done() <-chan struct{} {
	return r.done
}
