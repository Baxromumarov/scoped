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

	sp.Spawn(name, func(ctx context.Context, _ Spawner) error {
		var zero T

		// exec already converts panic → error when WithPanicAsError is set
		err := sp.(*spawner).s.exec(func(ctx context.Context) error {
			v, err := fn(ctx)
			r.ch <- result[T]{v, err}
			return err
		})

		// If panic occurred, exec returned a PanicError,
		// but the result was never published
		if err != nil {
			select {
			case r.ch <- result[T]{zero, err}:
			default:
			}
		}

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

// Done returns a channel that is closed when the task completes.
func (r *Result[T]) Done() <-chan result[T] {
	return r.ch
}
