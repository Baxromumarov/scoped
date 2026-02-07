package scoped

import "context"

// Result holds the outcome of an asynchronous task that produces a typed
// value. Create one via [GoResult].
type Result[T any] struct {
	val  T
	err  error
	done chan struct{}
	ctx  context.Context
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
	s *Scope,
	name string,
	fn func(ctx context.Context) (T, error),
) *Result[T] {
	r := &Result[T]{
		done: make(chan struct{}),
		ctx:  s.ctx,
	}
	s.Go(name, func(ctx context.Context) error {
		defer close(r.done)
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
// If the scope's context is canceled before the task completes, Wait
// returns the zero value and the context error.
func (r *Result[T]) Wait() (T, error) {
	select {
	case <-r.done:
		return r.val, r.err
	case <-r.ctx.Done():
		// Prioritize a completed result over context cancellation
		// in case both channels are ready simultaneously.
		select {
		case <-r.done:
			return r.val, r.err
		default:
			var zero T
			return zero, r.ctx.Err()
		}
	}
}

// Done returns a channel that is closed when the task completes.
func (r *Result[T]) Done() <-chan struct{} {
	return r.done
}
