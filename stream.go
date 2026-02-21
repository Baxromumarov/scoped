package scoped

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrStreamGap is returned when an ordered stream terminates with missing items.
	ErrStreamGap = fmt.Errorf("stream terminated with missing results (gap)")
)

// Stream represents a structured, pull-based data stream.
//
// Streams are single-consumer. Next() and other terminal methods
// must not be called concurrently. Concurrent calls to Next() will panic.
type Stream[T any] struct {
	next     func(ctx context.Context) (T, error)
	err      error
	stop     func()
	stopOnce sync.Once
	mu       sync.Mutex
	active   atomic.Int32 // concurrent Next() detector

	// Observability counters (always-on, ~1ns overhead per Next call).
	itemsRead atomic.Int64
	errCount  atomic.Int64
	startNano atomic.Int64 // unix nano, set on first Next() call
	lastRead  atomic.Int64 // unix nano of last successful read
}

// Next returns the next item in the stream.
// Returns io.EOF when the stream is exhausted.
//
// Next panics if called concurrently from multiple goroutines.
func (s *Stream[T]) Next(ctx context.Context) (T, error) {
	if s.active.Add(1) > 1 {
		s.active.Add(-1)
		panic("scoped: concurrent Stream.Next() calls detected; streams are single-consumer")
	}
	defer s.active.Add(-1)

	// Record start time on first call.
	s.startNano.CompareAndSwap(0, time.Now().UnixNano())

	val, err := s.next(ctx)
	if err != nil && err != io.EOF {
		s.errCount.Add(1)
		s.setError(err)
	}
	if err == nil {
		s.itemsRead.Add(1)
		s.lastRead.Store(time.Now().UnixNano())
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
	s.err = errors.Join(s.err, err)
	s.mu.Unlock()
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

// StreamStats provides observability counters for a stream.
type StreamStats struct {
	ItemsRead  int64         // items successfully read
	Errors     int64         // non-EOF errors encountered
	StartTime  time.Time     // zero if Next() was never called
	LastReadAt time.Time     // zero if no items have been read
	Throughput float64       // items per second since StartTime (0 if no items)
}

// Stats returns the current observability counters for the stream.
// Safe to call concurrently with Next() from a monitoring goroutine.
func (s *Stream[T]) Stats() StreamStats {
	read := s.itemsRead.Load()
	stats := StreamStats{
		ItemsRead: read,
		Errors:    s.errCount.Load(),
	}
	if ns := s.startNano.Load(); ns > 0 {
		stats.StartTime = time.Unix(0, ns)
	}
	if ns := s.lastRead.Load(); ns > 0 {
		stats.LastReadAt = time.Unix(0, ns)
	}
	if read > 0 && !stats.StartTime.IsZero() {
		elapsed := time.Since(stats.StartTime).Seconds()
		if elapsed > 0 {
			stats.Throughput = float64(read) / elapsed
		}
	}
	return stats
}

// StreamEvent describes an event within a stream's lifecycle.
// It is passed to the callback registered via [Observe].
type StreamEvent[T any] struct {
	Item     T
	Err      error         // non-nil for errors (not EOF)
	Duration time.Duration // time spent in the underlying Next call
	EOF      bool          // true when stream is exhausted
	Seq      int64         // 0-based sequence number
}

// Observe wraps a stream with an event callback that fires after every
// Next() call. The callback runs synchronously in the consumer's goroutine.
//
// Panics if s or fn is nil.
func Observe[T any](s *Stream[T], fn func(StreamEvent[T])) *Stream[T] {
	if s == nil {
		panic("scoped: Observe requires non-nil stream")
	}
	if fn == nil {
		panic("scoped: Observe requires non-nil callback")
	}
	var seq int64
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			start := time.Now()
			val, err := s.Next(ctx)
			elapsed := time.Since(start)

			evt := StreamEvent[T]{
				Item:     val,
				Duration: elapsed,
				Seq:      seq,
			}
			seq++

			if err == io.EOF {
				evt.EOF = true
			} else if err != nil {
				evt.Err = err
			}

			fn(evt)
			return val, err
		},
		stop: s.stopNow,
	}
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

	idx := 0

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
	// MaxPending limits the number of out-of-order results buffered in ordered mode.
	// When reached, the dispatcher blocks until the consumer catches up.
	// 0 means default (MaxWorkers * 2). Ignored when Ordered is false.
	MaxPending int
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
		var zero T
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}
		if idx >= len(cpy) {
			return zero, io.EOF
		}

		val := cpy[idx]
		idx++
		return val, nil
	})
}

// FromSliceRef creates a stream that reads directly from the provided slice
// without copying. The caller must not modify the slice after this call.
//
// Use this instead of [FromSlice] in performance-critical paths where the
// allocation cost of copying matters and ownership can be guaranteed.
func FromSliceRef[T any](items []T) *Stream[T] {
	var idx int
	return NewStream(func(ctx context.Context) (T, error) {
		var zero T
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		if idx >= len(items) {
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
		var zero T

		if ch == nil {
			return zero, io.EOF
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case v, ok := <-ch:
			if !ok {
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

// Empty returns a stream that immediately signals [io.EOF].
// It never yields any items.
func Empty[T any]() *Stream[T] {
	return NewStream(func(_ context.Context) (T, error) {
		var zero T
		return zero, io.EOF
	})
}

// Repeat returns a stream that emits val exactly n times.
// If n is negative, the stream repeats indefinitely until the context
// is cancelled or the consumer stops reading.
func Repeat[T any](val T, n int) *Stream[T] {
	count := 0
	return NewStream(func(ctx context.Context) (T, error) {
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}
		if n >= 0 && count >= n {
			var zero T
			return zero, io.EOF
		}
		count++
		return val, nil
	})
}

// Generate returns an infinite stream starting from seed, applying fn to
// produce each subsequent value: seed, fn(seed), fn(fn(seed)), ...
//
// The stream is infinite; use [Stream.Take], [Stream.TakeWhile], or
// context cancellation to bound it.
//
// Panics if fn is nil.
func Generate[T any](seed T, fn func(T) T) *Stream[T] {
	if fn == nil {
		panic("scoped: Generate requires non-nil fn")
	}
	cur := seed
	first := true
	return NewStream(func(ctx context.Context) (T, error) {
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}
		if first {
			first = false
			return cur, nil
		}
		cur = fn(cur)
		return cur, nil
	})
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
		opts.MaxWorkers = runtime.NumCPU() // default to number of logical CPUs if not set
	}
	if opts.MaxPending < 0 {
		panic("scoped: ParallelMap requires non-negative MaxPending")
	}

	// Default MaxPending for ordered mode.
	maxPending := opts.MaxPending
	if opts.Ordered && maxPending == 0 {
		maxPending = opts.MaxWorkers * 2
	}

	// Backpressure semaphore: limits out-of-order buffering in ordered mode.
	var pendingSem chan struct{}
	if opts.Ordered && maxPending > 0 {
		pendingSem = make(chan struct{}, maxPending)
	}

	mapCtx, mapCancel := context.WithCancelCause(ctx)
	
	resChanSize := opts.BufferSize + opts.MaxWorkers 
	resCh := make(chan indexedResult[B], resChanSize)
	out := &Stream[B]{
		stop: func() {
			mapCancel(context.Canceled)
		},
	}
	out.next = makeParallelNext(out, opts, resCh, pendingSem)

	sp.Spawn(
		"parallel-map-dispatcher",
		func(taskCtx context.Context, _ Spawner) error {
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

			// NOTE: defers run LIFO, so register <-done BEFORE runCancel
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

				// Backpressure: in ordered mode, limit pending out-of-order items.
				if pendingSem != nil {
					select {
					case pendingSem <- struct{}{}:
					case <-runCtx.Done():
						wg.Wait()
						if ctx.Err() != nil {
							return runCtx.Err()
						}
						return nil
					}
				}

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
								fnErr = newPanicError(r)
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
		},
	)

	return out
}

func makeParallelNext[B any](
	out *Stream[B],
	opts StreamOptions,
	resCh chan indexedResult[B],
	pendingSem chan struct{}, // may be nil when unordered or unbounded
) func(context.Context) (B, error) {
	var nextIdx int64
	// For ordered mode, buffer out-of-order results in a map keyed by index.
	// When pendingSem is non-nil, the pending map is bounded by its capacity.
	pending := make(map[int64]indexedResult[B])

	// releasePending releases a slot on the backpressure semaphore,
	// allowing the dispatcher to send more work.
	releasePending := func() {
		if pendingSem != nil {
			<-pendingSem
		}
	}

	return func(ctx context.Context) (B, error) {
		for {
			// Ordered mode: emit the next expected index if buffered.
			if opts.Ordered {
				if res, ok := pending[nextIdx]; ok {
					delete(pending, nextIdx)
					nextIdx++
					releasePending()
					if res.err != nil && res.err != io.EOF {
						out.setError(res.err)
					}
					return res.val, res.err
				}
			}

			var zero B
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case res, ok := <-resCh:
				if !ok {
					if !opts.Ordered {
						return zero, io.EOF
					}
					// Channel closed — drain any remaining buffered results.
					if res, ok := pending[nextIdx]; ok {
						delete(pending, nextIdx)
						nextIdx++
						releasePending()
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

						return zero, ErrStreamGap
					}

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
func (s *Stream[T]) ToSlice(ctx context.Context) (items []T, err error) {
	defer s.stopNow()
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

	sp.Spawn(
		"stream-to-chan",
		func(ctx context.Context, _ Spawner) error {
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
		},
	)
	return ch, errCh
}

// FromSliceUnsafe is a deprecated alias for [FromSliceRef].
//
// Deprecated: Use [FromSliceRef] instead.
func FromSliceUnsafe[T any](items []T) *Stream[T] {
	return FromSliceRef(items)
}

// Skip skips the first n items in the stream.
func (s *Stream[T]) Skip(n int) *Stream[T] {
	if n < 0 {
		panic("scoped: Skip requires n >= 0")
	}

	var skipped int
	var zero T

	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			for skipped < n {
				_, err := s.Next(ctx)
				if err != nil {
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
	count := 0

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

// TakeWhile returns a stream that emits items as long as fn returns true.
// Once fn returns false, the upstream is stopped and the stream signals EOF.
//
// Panics if fn is nil.
func (s *Stream[T]) TakeWhile(fn func(T) bool) *Stream[T] {
	if fn == nil {
		panic("scoped: TakeWhile requires non-nil predicate")
	}
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			val, err := s.Next(ctx)
			if err != nil {
				return val, err
			}
			if fn(val) {
				return val, nil
			}
			s.stopNow()
			var zero T
			return zero, io.EOF
		},
		stop: s.stopNow,
	}
}

// DropWhile returns a stream that skips items while fn returns true, then
// emits all remaining items unconditionally.
//
// Panics if fn is nil.
func (s *Stream[T]) DropWhile(fn func(T) bool) *Stream[T] {
	if fn == nil {
		panic("scoped: DropWhile requires non-nil predicate")
	}
	dropping := true
	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			for {
				val, err := s.Next(ctx)
				if err != nil {
					return val, err
				}
				if dropping && fn(val) {
					continue
				}
				dropping = false
				return val, nil
			}
		},
		stop: s.stopNow,
	}
}

// Any returns true if any item in the stream satisfies fn.
// It stops reading as soon as a match is found.
// Returns (false, nil) for an empty stream.
//
// Panics if fn is nil.
func (s *Stream[T]) Any(ctx context.Context, fn func(T) bool) (bool, error) {
	if fn == nil {
		panic("scoped: Any requires non-nil predicate")
	}
	defer s.stopNow()
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			return false, s.Err()
		}
		if err != nil {
			return false, err
		}
		if fn(val) {
			return true, nil
		}
	}
}

// All returns true if every item in the stream satisfies fn.
// It stops reading as soon as a non-matching item is found.
// Returns (true, nil) for an empty stream (vacuous truth).
//
// Panics if fn is nil.
func (s *Stream[T]) All(ctx context.Context, fn func(T) bool) (bool, error) {
	if fn == nil {
		panic("scoped: All requires non-nil predicate")
	}
	defer s.stopNow()
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			return true, s.Err()
		}
		if err != nil {
			return false, err
		}
		if !fn(val) {
			return false, nil
		}
	}
}

// First returns the first item in the stream and stops the stream.
// If the stream is empty, it returns the zero value and nil error.
func (s *Stream[T]) First(ctx context.Context) (T, error) {
	defer s.stopNow()
	val, err := s.Next(ctx)
	if err == io.EOF {
		var zero T
		return zero, s.Err()
	}
	return val, err
}

// Last consumes the entire stream and returns the last item.
// If the stream is empty, it returns the zero value and nil error.
func (s *Stream[T]) Last(ctx context.Context) (T, error) {
	defer s.stopNow()
	var last T
	found := false
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			if !found {
				return last, s.Err()
			}
			return last, s.Err()
		}
		if err != nil {
			if found {
				return last, err
			}
			return last, err
		}
		last = val
		found = true
	}
}

// Reduce folds the stream into a single value using the given accumulator function.
// On error, the partial accumulation so far is returned alongside the error.
func Reduce[T, R any](ctx context.Context, s *Stream[T], initial R, fn func(R, T) R) (R, error) {
	if s == nil {
		panic("scoped: Reduce requires non-nil source stream")
	}
	if fn == nil {
		panic("scoped: Reduce requires non-nil accumulator")
	}
	defer s.stopNow()

	acc := initial
	for {
		val, err := s.Next(ctx)
		if err == io.EOF {
			return acc, s.Err()
		}
		if err != nil {
			return acc, err
		}
		acc = fn(acc, val)
	}
}

// FlatMap transforms each item in the source stream into a sub-stream and
// concatenates all sub-streams sequentially. Each sub-stream is fully consumed
// before moving to the next source item.
//
// Nil sub-streams returned by fn are skipped.
func FlatMap[A, B any](s *Stream[A], fn func(context.Context, A) *Stream[B]) *Stream[B] {
	if s == nil {
		panic("scoped: FlatMap requires non-nil source stream")
	}
	if fn == nil {
		panic("scoped: FlatMap requires non-nil mapper")
	}

	var current *Stream[B]

	return &Stream[B]{
		next: func(ctx context.Context) (B, error) {
			for {
				// Try reading from current sub-stream.
				if current != nil {
					val, err := current.Next(ctx)
					if err == io.EOF {
						current.stopNow()
						current = nil
						continue
					}
					return val, err
				}

				// Get next source item.
				srcVal, err := s.Next(ctx)
				if err != nil {
					var zero B
					return zero, err
				}

				// Create sub-stream for this source item.
				current = fn(ctx, srcVal)
				// nil sub-streams are skipped.
			}
		},
		stop: func() {
			if current != nil {
				current.stopNow()
			}
			s.stopNow()
		},
	}
}

// Distinct returns a stream that suppresses duplicate items.
// Items are compared by value equality. T must be comparable.
func Distinct[T comparable](s *Stream[T]) *Stream[T] {
	if s == nil {
		panic("scoped: Distinct requires non-nil source stream")
	}

	seen := make(map[T]struct{})

	return &Stream[T]{
		next: func(ctx context.Context) (T, error) {
			for {
				val, err := s.Next(ctx)
				if err != nil {
					return val, err
				}
				if _, exists := seen[val]; !exists {
					seen[val] = struct{}{}
					return val, nil
				}
			}
		},
		stop: s.stopNow,
	}
}
