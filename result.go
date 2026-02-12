package scoped

import "context"

// Result holds the outcome of an asynchronous task that produces a typed
// value. Create one via [SpawnResult].
type Result[T any] struct {
	ch chan result[T]
}

type result[T any] struct {
	val T
	err error
}

// SpawnResult spawns a named task that returns a typed value and wraps the
// outcome in a [Result]. The task runs within the given [Scope], inheriting
// its lifecycle and error policy.
/* Example:
	r := scoped.SpawnResult(s, "compute", func(ctx context.Context) (int, error) {
    	return expensiveCalc(ctx)
	})
	val, err := r.Wait()
*/
func SpawnResult[T any](
	sp Spawner,
	name string,
	fn func(ctx context.Context) (T, error),
) *Result[T] {
	r := &Result[T]{ch: make(chan result[T], 1)}

	sp.Spawn(name, func(ctx context.Context, _ Spawner) (err error) {
		var v T

		defer func() {
			if rec := recover(); rec != nil {
				var zero T
				r.ch <- result[T]{val: zero, err: newPanicError(rec)}
				close(r.ch)
				panic(rec)
			}

			r.ch <- result[T]{val: v, err: err}
			close(r.ch)
		}()

		v, err = fn(ctx)
		return err
	})

	return r
}

// Wait blocks until the task completes.
// It does not return early on scope cancellation.
// It returns the task's value and error.
//
// Note: Since Spawner does not expose the scope's context, this Wait
// only waits for the task to complete.

func (r *Result[T]) Wait() (T, error) {
	res := <-r.ch
	return res.val, res.err
}

// Done returns a channel that receives exactly one task result and then closes.
func (r *Result[T]) Done() <-chan result[T] {
	return r.ch
}
