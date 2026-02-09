//! Stream functionality isn't ready for production use.
package scoped

import (
	"container/heap"
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
)

var (
	// ErrStreamGap is returned when an ordered stream terminates with missing items.
	ErrStreamGap = fmt.Errorf("stream terminated with missing results (gap)")
)

// Stream represents a structured, pull-based data stream.
//
// Note: Streams are single-consumer. Next() and other terminal methods
// must not be called concurrently.
type Stream[T any] struct {
	next func(ctx context.Context) (T, error)
	err  error
	mu   sync.Mutex
}

// Next returns the next item in the stream.
// Returns io.EOF when the stream is exhausted.
func (s *Stream[T]) Next(ctx context.Context) (T, error) {
	val, err := s.next(ctx)
	if err != nil && err != io.EOF {
		s.setError(err)
	}
	return val, err
}

// Err returns the final aggregated error after completion.
func (s *Stream[T]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Stream[T]) setError(err error) {
	if err == nil || err == io.EOF {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *Stream[T]) Filter(fn func(T) bool) *Stream[T] {
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			for {
				val, err := s.Next(ctx)
				if err != nil {
					return val, err
				}
				if fn(val) {
					return val, nil
				}
			}
		},
	}
}

// Take limits the stream to n items.
func (s *Stream[T]) Take(n int) *Stream[T] {
	var idx int
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			if idx >= n {
				var zero T
				return zero, io.EOF
			}
			val, err := s.Next(ctx)
			if err != nil {
				return val, err
			}
			idx++
			return val, nil
		},
	}
}

// StreamOptions configures parallel stream processing.
type StreamOptions struct {
	MaxWorkers int
	BufferSize int
	Ordered    bool
}

// NewStream creates a new stream from an iterator function.
func NewStream[T any](next func(context.Context) (T, error)) *Stream[T] {
	return &Stream[T]{
		next: next,
	}
}

// FromSlice creates a stream from a slice.
func FromSlice[T any](items []T) *Stream[T] {
	var idx int
	return NewStream(func(ctx context.Context) (T, error) {
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}
		if idx >= len(items) {
			var zero T
			return zero, io.EOF
		}
		val := items[idx]
		idx++
		return val, nil
	})
}

// FromChan creates a stream from a channel.
func FromChan[T any](ch <-chan T) *Stream[T] {
	return NewStream(func(ctx context.Context) (T, error) {
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case v, ok := <-ch:
			if !ok {
				var zero T
				return zero, io.EOF
			}
			return v, nil
		}
	})
}

// FromFunc creates a stream from a function.
func FromFunc[T any](fn func(context.Context) (T, error)) *Stream[T] {
	return NewStream(fn)
}

// Map transforms a stream using a function.
// Note: This is a function and not a method because Go does not support
// generic methods on generic types.
func Map[A, B any](s *Stream[A], fn func(context.Context, A) (B, error)) *Stream[B] {
	return &Stream[B]{
		next: func(ctx context.Context) (B, error) {
			val, err := s.Next(ctx)
			if err != nil {
				var zero B
				return zero, err
			}
			return fn(ctx, val)
		},
	}
}

// Batch groups items into slices of size n.
func Batch[T any](s *Stream[T], n int) *Stream[[]T] {
	return &Stream[[]T]{
		next: func(ctx context.Context) ([]T, error) {
			var batch []T
			for i := 0; i < n; i++ {
				val, err := s.Next(ctx)
				if err != nil {
					if err == io.EOF {
						if len(batch) > 0 {
							return batch, nil
						}
						return nil, io.EOF
					}
					return nil, err
				}
				batch = append(batch, val)
			}
			return batch, nil
		},
	}
}

// ParallelMap transforms a stream concurrently.
func ParallelMap[A, B any](
	ctx context.Context,
	s *Scope,
	src *Stream[A],
	opts StreamOptions,
	fn func(context.Context, A) (B, error),
) *Stream[B] {
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = runtime.NumCPU()
	}

	resCh := make(chan indexedResult[B], opts.BufferSize+opts.MaxWorkers)
	out := &Stream[B]{}
	out.next = makeParallelNext(out, opts, resCh)

	s.Go("parallel-map-dispatcher", func(ctx context.Context) error {
		defer close(resCh)
		var wg sync.WaitGroup
		sem := make(chan struct{}, opts.MaxWorkers)
		var inputIdx atomic.Int64

		for {
			val, err := src.Next(ctx)
			if err != nil {
				if err != io.EOF {
					out.setError(err)
				}
				break
			}

			idx := inputIdx.Add(1) - 1
			wg.Add(1)

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			s.Go("parallel-map-worker", func(ctx context.Context) error {
				defer func() { <-sem; wg.Done() }()
				res, err := fn(ctx, val)
				select {
				case resCh <- indexedResult[B]{idx: idx, val: res, err: err}:
				case <-ctx.Done():
				}
				return nil
			})
		}
		wg.Wait()
		return nil
	})

	return out
}

func makeParallelNext[B any](
	out *Stream[B],
	opts StreamOptions,
	resCh chan indexedResult[B],
) func(context.Context) (B, error) {
	var nextIdx int64
	var h indexedResultHeap[B]

	return func(ctx context.Context) (B, error) {
		for {
			// If ordered, check heap first
			if opts.Ordered && len(h) > 0 && h[0].idx == nextIdx {
				res := heap.Pop(&h).(indexedResult[B])
				nextIdx++
				if res.err != nil && res.err != io.EOF {
					out.setError(res.err)
				}
				return res.val, res.err
			}

			select {
			case <-ctx.Done():
				var zero B
				return zero, ctx.Err()
			case res, ok := <-resCh:
				if !ok {
					// resCh is closed. Drain the heap if ordered.
					if opts.Ordered && len(h) > 0 {
						// First check if the next expected index is available
						if h[0].idx == nextIdx {
							res := heap.Pop(&h).(indexedResult[B])
							nextIdx++
							if res.err != nil && res.err != io.EOF {
								out.setError(res.err)
							}
							return res.val, res.err
						}
						// If not at nextIdx but we have items, check if any have errors.
						for len(h) > 0 {
							res := heap.Pop(&h).(indexedResult[B])
							if res.err != nil {
								out.setError(res.err)
								return res.val, res.err
							}
						}
						// If we reach here, we have a gap and no errors in heap.
						out.setError(ErrStreamGap)
						var zero B
						return zero, ErrStreamGap
					}
					var zero B
					return zero, io.EOF
				}

				if !opts.Ordered {
					if res.err != nil && res.err != io.EOF {
						out.setError(res.err)
					}
					return res.val, res.err
				}

				// Ordered: push to heap and retry
				heap.Push(&h, res)
			}
		}
	}
}

type indexedResult[T any] struct {
	idx int64
	val T
	err error
}

type indexedResultHeap[T any] []indexedResult[T]

func (h indexedResultHeap[T]) Len() int           { return len(h) }
func (h indexedResultHeap[T]) Less(i, j int) bool { return h[i].idx < h[j].idx }
func (h indexedResultHeap[T]) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *indexedResultHeap[T]) Push(x any)        { *h = append(*h, x.(indexedResult[T])) }
func (h *indexedResultHeap[T]) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// ToSlice collects all items in the stream into a slice.
func (s *Stream[T]) ToSlice(ctx context.Context) ([]T, error) {
	var items []T
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			return items, s.Err()
		}
		if err != nil {
			return nil, err
		}
		items = append(items, val)
	}
}

// ForEach applies a function to each item in the stream.
func (s *Stream[T]) ForEach(ctx context.Context, fn func(T) error) error {
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			return s.Err()
		}
		if err != nil {
			return err
		}
		if err := fn(val); err != nil {
			return err
		}
	}
}

// ToChan sends all items in the stream to a channel.
// Note: This spawns an unscoped goroutine. It is the caller's responsibility
// to ensure the context is canceled or the channel is drained.
// Use ToChanScope for structured concurrency.
func (s *Stream[T]) ToChan(ctx context.Context) (<-chan T, <-chan error) {
	ch := make(chan T)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		for {
			val, err := s.Next(ctx)
			if err == io.EOF {
				errCh <- s.Err()
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			select {
			case ch <- val:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()
	return ch, errCh
}

// ToChanScope sends all items in the stream to a channel within a Scope.
// The goroutine is managed by the scope and will stop when the stream is exhausted
// or the scope is canceled.
func (s *Stream[T]) ToChanScope(scope *Scope) (<-chan T, <-chan error) {
	ch := make(chan T)
	errCh := make(chan error, 1)
	scope.Go("stream-to-chan", func(ctx context.Context) error {
		defer close(ch)
		defer close(errCh)
		for {
			val, err := s.Next(ctx)
			if err == io.EOF {
				errCh <- s.Err()
				return nil
			}
			if err != nil {
				errCh <- err
				return err
			}
			select {
			case ch <- val:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return ctx.Err()
			}
		}
	})
	return ch, errCh
}

// Collect is an alias for ToSlice.
func (s *Stream[T]) Collect(ctx context.Context) ([]T, error) {
	return s.ToSlice(ctx)
}

// Skip skips the first n items in the stream.
func (s *Stream[T]) Skip(n int) *Stream[T] {
	var skipped int
	return NewStream(func(ctx context.Context) (T, error) {
		for skipped < n {
			_, err := s.Next(ctx)
			if err != nil {
				var zero T
				return zero, err
			}
			skipped++
		}
		return s.Next(ctx)
	})
}

// Peek allows inspecting items as they pass through the stream.
func (s *Stream[T]) Peek(fn func(T)) *Stream[T] {
	return NewStream(func(ctx context.Context) (T, error) {
		val, err := s.Next(ctx)
		if err == nil {
			fn(val)
		}
		return val, err
	})
}

// Count counts the number of items in the stream.
func (s *Stream[T]) Count(ctx context.Context) (int, error) {
	var count int
	for {
		_, err := s.Next(ctx)
		if err == io.EOF {
			return count, s.Err()
		}
		if err != nil {
			return 0, err
		}
		count++
	}
}
