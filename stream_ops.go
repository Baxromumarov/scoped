package scoped

import "context"

// Pair holds two values paired from two streams.
// It is used by [Zip].
type Pair[A, B any] struct {
	First  A
	Second B
}

// Scan returns a stream that applies fn cumulatively to each item,
// emitting each intermediate accumulation. The first emitted value is
// fn(initial, firstItem).
//
// This is the streaming counterpart of [Reduce]: Reduce produces a
// single final value, while Scan produces a stream of running values.
//
// Panics if s is nil or fn is nil.
func Scan[T, R any](s *Stream[T], initial R, fn func(R, T) R) *Stream[R] {
	if s == nil {
		panic("scoped: Scan requires non-nil source stream")
	}
	if fn == nil {
		panic("scoped: Scan requires non-nil accumulator")
	}
	acc := initial
	return &Stream[R]{
		next: func(ctx context.Context) (R, error) {
			val, err := s.Next(ctx)
			if err != nil {
				var zero R
				return zero, err
			}
			acc = fn(acc, val)
			return acc, nil
		},
		stop: s.stopNow,
	}
}

// Zip pairs items from two streams element-by-element. The resulting
// stream emits [Pair] values and stops as soon as either input stream
// is exhausted (EOF). When one stream ends, the other is stopped
// immediately.
//
// Both streams are read sequentially (a first, then b) within each
// Next call — this is safe because streams are single-consumer.
//
// Panics if a or b is nil.
func Zip[A, B any](a *Stream[A], b *Stream[B]) *Stream[Pair[A, B]] {
	if a == nil {
		panic("scoped: Zip requires non-nil first stream")
	}
	if b == nil {
		panic("scoped: Zip requires non-nil second stream")
	}
	return &Stream[Pair[A, B]]{
		next: func(ctx context.Context) (Pair[A, B], error) {
			va, err := a.Next(ctx)
			if err != nil {
				b.stopNow()
				var zero Pair[A, B]
				return zero, err
			}
			vb, err := b.Next(ctx)
			if err != nil {
				a.stopNow()
				var zero Pair[A, B]
				return zero, err
			}
			return Pair[A, B]{First: va, Second: vb}, nil
		},
		stop: func() {
			a.stopNow()
			b.stopNow()
		},
	}
}
