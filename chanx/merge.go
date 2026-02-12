package chanx

import (
	"context"
	"sync"
)

// Merge combines multiple input channels into a single output channel
// (fan-in). The output channel is closed when all inputs are closed or
// the context is canceled. The order of values is non-deterministic.
//
// Nil channels are skipped (they would block forever).
// Every internal goroutine is tied to ctx and will exit promptly on
// cancellation.
func Merge[T any](ctx context.Context, chs ...<-chan T) <-chan T {
	out := make(chan T)

	var wg sync.WaitGroup
	for _, ch := range chs {
		if ch == nil {
			continue // Skip nil channels
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case v, ok := <-ch:
					if !ok {
						return
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
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// FanOut distributes values from in across n output channels in
// round-robin order. Each output channel is closed when in is closed
// or the context is canceled.
//
// If in is nil, all output channels are closed immediately.
// This is useful for distributing work to a fixed set of workers.
//
// Note: Output channels have a buffer of 1. If any consumer is slow,
// it blocks the entire round-robin distribution (head-of-line blocking).
// Consider draining outputs concurrently to avoid stalls.
//
// FanOut panics if n is not positive.
func FanOut[T any](ctx context.Context, in <-chan T, n int) []<-chan T {
	if n <= 0 {
		panic("chanx: FanOut requires n > 0")
	}

	outs := make([]chan T, n)
	for i := range outs {
		outs[i] = make(chan T, 1) // Buffer of 1 allows non-blocking sends during round-robin
	}

	// Handle nil input channel - close all outputs immediately
	if in == nil {
		for _, ch := range outs {
			close(ch)
		}
		result := make([]<-chan T, n)
		for i, ch := range outs {
			result[i] = ch
		}
		return result
	}

	go func() {
		defer func() {
			for _, ch := range outs {
				close(ch)
			}
		}()
		idx := 0
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case outs[idx%n] <- v:
					idx++
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	result := make([]<-chan T, n)
	for i, ch := range outs {
		result[i] = ch
	}
	return result
}
