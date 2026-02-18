package chanx

import (
	"context"
)

// Map transforms values from in by applying fn and sends the results
// to the returned channel. The output channel is closed when in is
// closed or ctx is cancelled.
//
// If in is nil, returns a closed channel immediately.
func Map[T, U any](ctx context.Context, in <-chan T, fn func(T) U) <-chan U {
	out := make(chan U)

	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- fn(v):
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Filter passes values from in to the returned channel only if fn
// returns true. The output channel is closed when in is closed or
// ctx is cancelled.
//
// If in is nil, returns a closed channel immediately.
func Filter[T any](ctx context.Context, in <-chan T, fn func(T) bool) <-chan T {
	out := make(chan T)

	if in == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				if !fn(v) {
					continue
				}
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Take forwards the first n items from in, then closes the output channel.
// If in is closed before n items are received, the output closes early.
//
// If in is nil, returns a closed channel immediately.
// Panics if n < 0.
func Take[T any](ctx context.Context, in <-chan T, n int) <-chan T {
	if n < 0 {
		panic("chanx: Take requires n >= 0")
	}
	out := make(chan T)
	if in == nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		count := 0
		for count < n {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- v:
					count++
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Skip drops the first n items from in, then forwards the rest.
// If in is closed before n items are skipped, the output closes immediately.
//
// If in is nil, returns a closed channel immediately.
// Panics if n < 0.
func Skip[T any](ctx context.Context, in <-chan T, n int) <-chan T {
	if n < 0 {
		panic("chanx: Skip requires n >= 0")
	}
	out := make(chan T)
	if in == nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		skipped := 0
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				if skipped < n {
					skipped++
					continue
				}
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Scan applies fn cumulatively to items from in, emitting each
// intermediate accumulation to the output channel. The initial value
// is used as the starting accumulator; it is not emitted.
//
// If in is nil, returns a closed channel immediately.
// Panics if fn is nil.
func Scan[T, R any](ctx context.Context, in <-chan T, initial R, fn func(R, T) R) <-chan R {
	if fn == nil {
		panic("chanx: Scan requires non-nil accumulator")
	}
	out := make(chan R)
	if in == nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		acc := initial
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				acc = fn(acc, v)
				select {
				case out <- acc:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
