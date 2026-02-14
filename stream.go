package scoped

import (
	"context"
	"errors"
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
	next     func(ctx context.Context) (T, error)
	err      error
	stop     func()
	stopOnce sync.Once
	mu       sync.Mutex
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
	s.err = errors.Join(s.err, err)
}

func (s *Stream[T]) stopNow() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.stop != nil {
			s.stop()
		}
	})
}

// Stop terminates the stream and releases associated resources.
// Safe to call multiple times and concurrently.
func (s *Stream[T]) Stop() {
	s.stopNow()
}

// Filter returns a stream that only emits items for which fn returns true.
func (s *Stream[T]) Filter(fn func(T) bool) *Stream[T] {
	if fn == nil {
		panic("scoped: Filter requires non-nil predicate")
	}
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
		stop: s.stopNow,
	}
}

// Take limits the stream to n items.
func (s *Stream[T]) Take(n int) *Stream[T] {
	if n < 0 {
		panic("scoped: Take requires n >= 0")
	}
	var idx int
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			if idx >= n {
				s.stopNow()
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
		stop: s.stopNow,
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
	if next == nil {
		panic("scoped: NewStream requires non-nil next function")
	}
	return &Stream[T]{
		next: next,
	}
}

// FromSlice creates a stream from a slice.
// The slice is copied internally; the caller may safely modify the original after this call.
func FromSlice[T any](items []T) *Stream[T] {
	cpy := make([]T, len(items))
	copy(cpy, items)
	var idx int
	return NewStream(func(ctx context.Context) (T, error) {
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}
		if idx >= len(cpy) {
			var zero T
			return zero, io.EOF
		}
		val := cpy[idx]
		idx++
		return val, nil
	})
}

// FromChan creates a stream from a channel.
func FromChan[T any](ch <-chan T) *Stream[T] {
	return NewStream(func(ctx context.Context) (T, error) {
		if ch == nil {
			var zero T
			return zero, io.EOF
		}
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
//
// Deprecated: Use [NewStream] instead, which is identical.
func FromFunc[T any](fn func(context.Context) (T, error)) *Stream[T] {
	return NewStream(fn)
}

// Map transforms a stream using a function.
// Note: This is a function and not a method because Go does not support
// generic methods on generic types.
func Map[A, B any](s *Stream[A], fn func(context.Context, A) (B, error)) *Stream[B] {
	if s == nil {
		panic("scoped: Map requires non-nil source stream")
	}
	if fn == nil {
		panic("scoped: Map requires non-nil mapper")
	}
	return &Stream[B]{
		next: func(ctx context.Context) (B, error) {
			val, err := s.Next(ctx)
			if err != nil {
				var zero B
				return zero, err
			}
			return fn(ctx, val)
		},
		stop: s.stopNow,
	}
}

// Batch groups items into slices of size n.
func Batch[T any](s *Stream[T], n int) *Stream[[]T] {
	if s == nil {
		panic("scoped: Batch requires non-nil source stream")
	}
	if n <= 0 {
		panic("scoped: Batch requires n > 0")
	}
	return &Stream[[]T]{
		next: func(ctx context.Context) ([]T, error) {
			batch := make([]T, 0, n)
			for range n {
				val, err := s.Next(ctx)
				if err != nil {
					if err == io.EOF {
						if len(batch) > 0 {
							return batch, nil
						}
						return nil, io.EOF
					}
					if len(batch) > 0 {
						return batch, err
					}
					return nil, err
				}
				batch = append(batch, val)
			}
			return batch, nil
		},
		stop: s.stopNow,
	}
}

// ParallelMap transforms a stream concurrently.
//
// Workers are managed internally and do NOT use sp.Spawn, so the results
// channel can be consumed inside the same [Run] callback without deadlock.
// The dispatcher goroutine IS spawned via sp.Spawn so it respects the
// scope lifecycle.
func ParallelMap[A, B any](
	ctx context.Context,
	sp Spawner,
	src *Stream[A],
	opts StreamOptions,
	fn func(context.Context, A) (B, error),
) *Stream[B] {
	if sp == nil {
		panic("scoped: ParallelMap requires non-nil spawner")
	}
	if src == nil {
		panic("scoped: ParallelMap requires non-nil source stream")
	}
	if fn == nil {
		panic("scoped: ParallelMap requires non-nil mapper")
	}
	if opts.BufferSize < 0 {
		panic("scoped: ParallelMap requires non-negative buffer size")
	}
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = runtime.NumCPU()
	}

	mapCtx, mapCancel := context.WithCancelCause(ctx)
	resCh := make(chan indexedResult[B], opts.BufferSize+opts.MaxWorkers)
	out := &Stream[B]{
		stop: func() {
			mapCancel(context.Canceled)
		},
	}
	out.next = makeParallelNext(out, opts, resCh)

	sp.Spawn("parallel-map-dispatcher", func(taskCtx context.Context, _ Spawner) error {
		defer close(resCh)

		runCtx, runCancel := context.WithCancelCause(taskCtx)

		// Bridge: if the consumer calls stopNow() (which cancels mapCtx),
		// propagate that into runCtx so workers and src.Next stop.
		done := make(chan struct{})
		go func() {
			defer close(done)
			select {
			case <-mapCtx.Done():
				runCancel(context.Cause(mapCtx))
			case <-runCtx.Done():
			}
		}()

		// IMPORTANT: defers run LIFO, so register <-done BEFORE runCancel
		// so that runCancel fires first, allowing the done goroutine to exit.
		defer func() { <-done }()
		defer runCancel(nil)

		var wg sync.WaitGroup
		sem := make(chan struct{}, opts.MaxWorkers)
		var inputIdx atomic.Int64

		for {
			val, err := src.Next(runCtx)
			if err != nil {
				if err != io.EOF {
					// Suppress only context.Canceled errors caused by
					// consumer-driven cancellation (stopNow/Take).
					// All other source errors are always recorded.
					consumerStopped := mapCtx.Err() != nil && ctx.Err() == nil
					if !(consumerStopped && errors.Is(err, context.Canceled)) {
						out.setError(err)
					}
				}
				break
			}

			idx := inputIdx.Add(1) - 1
			wg.Add(1)

			select {
			case sem <- struct{}{}:
			case <-runCtx.Done():
				wg.Done()
				wg.Wait()
				// Consumer-driven cancellation (stopNow/Take) is not an error,
				// but external context cancellation should propagate.
				if ctx.Err() != nil {
					return runCtx.Err()
				}
				return nil
			}

			// Workers use raw goroutines — NOT sp.Spawn — to avoid
			// deadlocking when the consumer reads resCh inside Run.
			go func() {
				defer func() { <-sem; wg.Done() }()
				var res B
				var fnErr error
				func() {
					defer func() {
						if r := recover(); r != nil {
							fnErr = fmt.Errorf("scoped: ParallelMap worker panicked: %v", r)
						}
					}()
					res, fnErr = fn(runCtx, val)
				}()
				select {
				case resCh <- indexedResult[B]{
					idx: idx,
					val: res,
					err: fnErr,
				}:
				case <-runCtx.Done():
				}
			}()
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
	// For ordered mode, buffer out-of-order results in a map keyed by index.
	// This avoids heap allocations (heap.Push/Pop box into any).
	//
	// NOTE: pending can grow up to the total stream size if an early item
	// is slow while later items complete quickly. The practical bound is
	// MaxWorkers + BufferSize for most workloads.
	pending := make(map[int64]indexedResult[B])

	return func(ctx context.Context) (B, error) {
		for {
			// Ordered mode: emit the next expected index if buffered.
			if opts.Ordered {
				if res, ok := pending[nextIdx]; ok {
					delete(pending, nextIdx)
					nextIdx++
					if res.err != nil && res.err != io.EOF {
						out.setError(res.err)
					}
					return res.val, res.err
				}
			}

			select {
			case <-ctx.Done():
				var zero B
				return zero, ctx.Err()
			case res, ok := <-resCh:
				if !ok {
					if !opts.Ordered {
						var zero B
						return zero, io.EOF
					}
					// Channel closed — drain any remaining buffered results.
					if res, ok := pending[nextIdx]; ok {
						delete(pending, nextIdx)
						nextIdx++
						if res.err != nil && res.err != io.EOF {
							out.setError(res.err)
						}
						return res.val, res.err
					}
					if len(pending) > 0 {
						// Out-of-order items remain but not nextIdx — gap.
						// Return the first embedded error if any, else ErrStreamGap.
						for _, res := range pending {
							if res.err != nil {
								out.setError(res.err)
								return res.val, res.err
							}
						}
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

				// Ordered: buffer and loop to check if nextIdx is ready.
				pending[res.idx] = res
			}
		}
	}
}

type indexedResult[T any] struct {
	idx int64
	val T
	err error
}

// ToSlice collects all items in the stream into a slice.
// On error, any items collected before the error are returned alongside it,
// following the io.Reader convention.
func (s *Stream[T]) ToSlice(ctx context.Context) ([]T, error) {
	defer s.stopNow()
	var items []T
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			return items, s.Err()
		}
		if err != nil {
			return items, err
		}
		items = append(items, val)
	}
}

// ForEach applies a function to each item in the stream.
func (s *Stream[T]) ForEach(ctx context.Context, fn func(T) error) error {
	if fn == nil {
		panic("scoped: ForEach requires non-nil callback")
	}
	defer s.stopNow()
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

// ToChanScope sends all items in the stream to a channel within a Scope.
// The goroutine is managed by the scope and will stop when the stream is exhausted
// or the scope is canceled.
func (s *Stream[T]) ToChanScope(sp Spawner) (<-chan T, <-chan error) {
	if sp == nil {
		panic("scoped: ToChanScope requires non-nil spawner")
	}
	ch := make(chan T)
	errCh := make(chan error, 1)
	sp.Spawn("stream-to-chan", func(ctx context.Context, _ Spawner) error {
		defer s.stopNow()
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
//
// Deprecated: Use [Stream.ToSlice] instead.
func (s *Stream[T]) Collect(ctx context.Context) ([]T, error) {
	return s.ToSlice(ctx)
}

// Skip skips the first n items in the stream.
func (s *Stream[T]) Skip(n int) *Stream[T] {
	if n < 0 {
		panic("scoped: Skip requires n >= 0")
	}
	var skipped int
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			for skipped < n {
				_, err := s.Next(ctx)
				if err != nil {
					var zero T
					return zero, err
				}
				skipped++
			}
			return s.Next(ctx)
		},
		stop: s.stopNow,
	}
}

// Peek allows inspecting items as they pass through the stream.
func (s *Stream[T]) Peek(fn func(T)) *Stream[T] {
	if fn == nil {
		panic("scoped: Peek requires non-nil callback")
	}
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			val, err := s.Next(ctx)
			if err == nil {
				fn(val)
			}
			return val, err
		},
		stop: s.stopNow,
	}
}

// Count counts the number of items in the stream.
func (s *Stream[T]) Count(ctx context.Context) (int, error) {
	defer s.stopNow()
	var count int
	for {
		_, err := s.Next(ctx)
		if err == io.EOF {
			return count, s.Err()
		}
		if err != nil {
			return count, err
		}
		count++
	}
}
