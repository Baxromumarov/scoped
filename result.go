package scoped

import "context"

// Result holds the outcome of an asynchronous task that produces a typed
// value. Create one via [SpawnResult].
type Result[T any] struct {
	ch chan ResultValue[T]
}

// ResultValue holds the value and error from a completed [Result] task.
type ResultValue[T any] struct {
	Val T
	Err error
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
	r := &Result[T]{ch: make(chan ResultValue[T], 1)}

	sp.Spawn(
		name,
		func(ctx context.Context, _ Spawner) (err error) {
			var v T
			var zero T
			
			defer func() {
				if rec := recover(); rec != nil {

					r.ch <- ResultValue[T]{
						Val: zero,
						Err: newPanicError(rec),
					}
					close(r.ch)
					panic(rec)
				}

				r.ch <- ResultValue[T]{
					Val: v,
					Err: err,
				}
				close(r.ch)
			}()

			v, err = fn(ctx)
			return err
		},
	)

	return r
}

// Wait blocks until the task completes and returns its value and error.
func (r *Result[T]) Wait() (T, error) {
	res := <-r.ch
	return res.Val, res.Err
}

// Done returns a channel that receives exactly one [ResultValue] and then closes.
func (r *Result[T]) Done() <-chan ResultValue[T] {
	return r.ch
}
