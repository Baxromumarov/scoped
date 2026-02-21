package scoped

import (
	"context"
	"sync"
)

// Result holds the outcome of an asynchronous task that produces a typed
// value. Create one via [SpawnResult].
type Result[T any] struct {
	done chan struct{} // closed when result is ready
	once sync.Once
	val  T
	err  error
}

// ResultValue holds the value and error from a completed [Result] task.
type ResultValue[T any] struct {
	Val T
	Err error
}

// resolve stores the result and signals completion exactly once.
func (r *Result[T]) resolve(val T, err error) {
	r.once.Do(func() {
		r.val = val
		r.err = err
		close(r.done)
	})
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
	if sp == nil {
		panic("scoped: SpawnResult requires non-nil spawner")
	}
	if fn == nil {
		panic("scoped: SpawnResult requires non-nil fn")
	}
	r := &Result[T]{done: make(chan struct{})}

	sp.Spawn(
		name,
		func(ctx context.Context, _ Spawner) (err error) {
			var v T
			var zero T

			defer func() {
				if rec := recover(); rec != nil {
					r.resolve(zero, newPanicError(rec))
					panic(rec)
				}
				r.resolve(v, err)
			}()

			v, err = fn(ctx)
			return err
		},
	)

	return r
}

// Wait blocks until the task completes and returns its value and error.
func (r *Result[T]) Wait() (T, error) {
	<-r.done
	return r.val, r.err
}

// Done returns a channel that receives exactly one [ResultValue] and then closes.
func (r *Result[T]) Done() <-chan ResultValue[T] {
	ch := make(chan ResultValue[T], 1)
	go func() {
		<-r.done
		ch <- ResultValue[T]{Val: r.val, Err: r.err}
		close(ch)
	}()
	return ch
}
