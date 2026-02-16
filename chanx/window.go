package chanx

import (
	"context"
	"time"
)

// WindowMode specifies whether the window is tumbling or sliding.
type WindowMode int

const (
	// Tumbling windows are non-overlapping: each item belongs to exactly one window.
	Tumbling WindowMode = iota
	// Sliding windows overlap: each emitted batch contains all items from the last duration.
	Sliding
)

// Window collects items from in into time-based windows.
// In Tumbling mode, items are collected for duration then emitted as a batch.
// In Sliding mode, each emitted batch contains all items received within the
// last duration; a new batch is emitted at each tick.
//
// Window panics if duration <= 0.
// If in is nil, returns a closed channel immediately.
func Window[T any](
	ctx context.Context,
	in <-chan T,
	duration time.Duration,
	mode WindowMode,
) <-chan []T {
	if duration <= 0 {
		panic("chanx: Window requires duration > 0")
	}

	out := make(chan []T)

	if in == nil {
		close(out)
		return out
	}

	switch mode {
	case Tumbling:
		go windowTumbling(ctx, in, out, duration)
	case Sliding:
		go windowSliding(ctx, in, out, duration)
	default:
		panic("chanx: unknown WindowMode")
	}

	return out
}

func windowTumbling[T any](
	ctx context.Context,
	in <-chan T,
	out chan<- []T,
	duration time.Duration,
) {
	defer close(out)
	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	var batch []T
	for {
		select {
		case v, ok := <-in:
			if !ok {
				if len(batch) > 0 {
					select {
					case out <- batch:
					case <-ctx.Done():
					}
				}
				return
			}
			batch = append(batch, v)
		case <-ticker.C:
			if len(batch) > 0 {
				select {
				case out <- batch:
				case <-ctx.Done():
					return
				}
				batch = nil
			}
		case <-ctx.Done():
			return
		}
	}
}

type timestamped[T any] struct {
	val  T
	when time.Time
}

func windowSliding[T any](
	ctx context.Context,
	in <-chan T,
	out chan<- []T,
	duration time.Duration,
) {
	defer close(out)
	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	var items []timestamped[T]
	for {
		select {
		case v, ok := <-in:
			if !ok {
				// Emit remaining items within the window.
				if len(items) > 0 {
					cutoff := time.Now().Add(-duration)
					var batch []T
					for _, item := range items {
						if !item.when.Before(cutoff) {
							batch = append(batch, item.val)
						}
					}
					if len(batch) > 0 {
						select {
						case out <- batch:
						case <-ctx.Done():
						}
					}
				}
				return
			}
			items = append(items, timestamped[T]{
				val:  v,
				when: time.Now(),
			})
		case <-ticker.C:
			cutoff := time.Now().Add(-duration)
			// Remove expired items.
			start := 0
			for start < len(items) && items[start].when.Before(cutoff) {
				start++
			}
			items = items[start:]

			if len(items) > 0 {
				batch := make([]T, len(items))
				for i, item := range items {
					batch[i] = item.val
				}
				select {
				case out <- batch:
				case <-ctx.Done():
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}
