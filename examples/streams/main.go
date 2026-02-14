// Package main demonstrates the Stream API: constructors, transforms,
// terminal operations, batching, and parallel processing.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/baxromumarov/scoped"
)

func main() {
	basicStream()
	streamFromChannel()
	fluentChaining()
	batchProcessing()
	parallelMapOrdered()
	parallelMapUnordered()
	parallelMapWithTake()
	streamToChannel()
	streamErrorHandling()
	customStream()
}

// basicStream shows creating a stream from a slice and consuming it.
func basicStream() {
	fmt.Println("=== Basic Stream ===")

	s := scoped.FromSlice([]int{1, 2, 3, 4, 5})
	result, err := s.ToSlice(context.Background())
	fmt.Printf("  ToSlice: %v (err=%v)\n", result, err)

	// ForEach consumes the stream with a callback.
	var sum int
	err = scoped.FromSlice([]int{10, 20, 30}).ForEach(context.Background(), func(v int) error {
		sum += v
		return nil
	})
	fmt.Printf("  ForEach sum: %d (err=%v)\n", sum, err)

	// Count returns the number of items.
	count, err := scoped.FromSlice([]string{"a", "b", "c"}).Count(context.Background())
	fmt.Printf("  Count: %d (err=%v)\n\n", count, err)
}

// streamFromChannel shows creating a stream from a Go channel.
func streamFromChannel() {
	fmt.Println("=== Stream from Channel ===")

	ch := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		ch <- i * 10
	}
	close(ch)

	result, err := scoped.FromChan(ch).ToSlice(context.Background())
	fmt.Printf("  from chan: %v (err=%v)\n\n", result, err)
}

// fluentChaining demonstrates composing multiple transforms.
func fluentChaining() {
	fmt.Println("=== Fluent Chaining ===")

	// Filter even numbers, skip first 2, take next 3.
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	result, err := scoped.FromSlice(items).
		Filter(func(v int) bool { return v%2 == 0 }).
		Skip(2).
		Take(3).
		ToSlice(context.Background())

	fmt.Printf("  even, skip 2, take 3: %v (err=%v)\n", result, err)

	// Peek lets you observe items without modifying the stream.
	var peeked []int
	result, _ = scoped.FromSlice([]int{1, 2, 3}).
		Peek(func(v int) { peeked = append(peeked, v) }).
		ToSlice(context.Background())
	fmt.Printf("  peeked: %v, result: %v\n", peeked, result)

	// Map transforms types (function, not method, because Go generics).
	words := scoped.FromSlice([]string{"hello", "world"})
	upper := scoped.Map(words, func(ctx context.Context, s string) (string, error) {
		return strings.ToUpper(s), nil
	})
	res, _ := upper.ToSlice(context.Background())
	fmt.Printf("  Map uppercase: %v\n\n", res)
}

// batchProcessing groups items into fixed-size batches.
func batchProcessing() {
	fmt.Println("=== Batch Processing ===")

	items := []int{1, 2, 3, 4, 5, 6, 7}
	batches := scoped.Batch(scoped.FromSlice(items), 3)
	result, err := batches.ToSlice(context.Background())

	fmt.Printf("  batch(3) of %v:\n", items)
	for i, b := range result {
		fmt.Printf("    batch %d: %v\n", i, b)
	}
	fmt.Printf("  err=%v\n\n", err)
}

// parallelMapOrdered processes items concurrently while preserving input order.
func parallelMapOrdered() {
	fmt.Println("=== ParallelMap (Ordered) ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		src := scoped.FromSlice([]int{1, 2, 3, 4, 5, 6, 7, 8})

		// Simulate variable-latency work. Ordered mode ensures output
		// matches input order regardless of completion order.
		pm := scoped.ParallelMap(context.Background(), sp, src,
			scoped.StreamOptions{MaxWorkers: 4, Ordered: true},
			func(ctx context.Context, v int) (string, error) {
				time.Sleep(time.Duration(8-v) * time.Millisecond) // reverse delay
				return fmt.Sprintf("item-%d", v), nil
			},
		)

		result, err := pm.ToSlice(context.Background())
		fmt.Printf("  ordered result: %v (err=%v)\n", result, err)
	})

	if err != nil {
		fmt.Printf("  scope error: %v\n", err)
	}
	fmt.Println()
}

// parallelMapUnordered processes items concurrently, emitting results
// as soon as they complete (fastest throughput).
func parallelMapUnordered() {
	fmt.Println("=== ParallelMap (Unordered) ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		src := scoped.FromSlice([]int{1, 2, 3, 4, 5})

		pm := scoped.ParallelMap(context.Background(), sp, src,
			scoped.StreamOptions{MaxWorkers: 5},
			func(ctx context.Context, v int) (int, error) {
				return v * v, nil
			},
		)

		result, err := pm.ToSlice(context.Background())
		fmt.Printf("  unordered squares: %v (err=%v)\n", result, err)
		fmt.Println("  (order may vary between runs)")
	})

	if err != nil {
		fmt.Printf("  scope error: %v\n", err)
	}
	fmt.Println()
}

// parallelMapWithTake demonstrates early termination: Take(n) stops
// the parallel pipeline after receiving n results.
func parallelMapWithTake() {
	fmt.Println("=== ParallelMap + Take ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// Large source, but we only need the first 3 results.
		items := make([]int, 1000)
		for i := range items {
			items[i] = i
		}

		pm := scoped.ParallelMap(context.Background(), sp, scoped.FromSlice(items),
			scoped.StreamOptions{MaxWorkers: 10, Ordered: true},
			func(ctx context.Context, v int) (int, error) {
				return v * 2, nil
			},
		)

		result, err := pm.Take(3).ToSlice(context.Background())
		fmt.Printf("  first 3 of 1000: %v (err=%v)\n", result, err)
	})

	if err != nil {
		fmt.Printf("  scope error: %v\n", err)
	}
	fmt.Println()
}

// streamToChannel converts a stream into a channel within a scope.
func streamToChannel() {
	fmt.Println("=== Stream to Channel (ToChanScope) ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		s := scoped.FromSlice([]int{10, 20, 30})
		ch, errCh := s.ToChanScope(sp)

		// Consume the channel like any Go channel.
		var items []int
		for v := range ch {
			items = append(items, v)
		}

		fmt.Printf("  received: %v\n", items)
		if chErr := <-errCh; chErr != nil {
			fmt.Printf("  channel error: %v\n", chErr)
		}
	})

	if err != nil {
		fmt.Printf("  scope error: %v\n", err)
	}
	fmt.Println()
}

// streamErrorHandling shows how errors propagate through stream pipelines.
func streamErrorHandling() {
	fmt.Println("=== Stream Error Handling ===")

	// ToSlice returns partial results alongside the error.
	i := 0
	s := scoped.NewStream(func(ctx context.Context) (int, error) {
		i++
		if i > 3 {
			return 0, fmt.Errorf("source exhausted at %d", i)
		}
		return i * 10, nil
	})

	result, err := s.ToSlice(context.Background())
	fmt.Printf("  partial results: %v\n", result)
	fmt.Printf("  error: %v\n", err)

	// Batch also returns partial batches on error.
	j := 0
	bs := scoped.Batch(scoped.NewStream(func(ctx context.Context) (int, error) {
		j++
		if j > 5 {
			return 0, fmt.Errorf("done")
		}
		return j, nil
	}), 3)

	batches, _ := bs.ToSlice(context.Background())
	fmt.Printf("  batches with partial: %v\n", batches)

	// Stream.Stop() for explicit cleanup.
	s2 := scoped.FromSlice([]int{1, 2, 3, 4, 5})
	v, _ := s2.Next(context.Background())
	fmt.Printf("  got first item: %d, stopping stream\n", v)
	s2.Stop()
	fmt.Println()
}

// customStream shows creating a stream from a custom iterator.
func customStream() {
	fmt.Println("=== Custom Stream (Fibonacci) ===")

	a, b := 0, 1
	fib := scoped.NewStream(func(ctx context.Context) (int, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		val := a
		a, b = b, a+b
		return val, nil
	})

	// Take the first 10 Fibonacci numbers.
	result, err := fib.Take(10).ToSlice(context.Background())
	fmt.Printf("  first 10 fib: %v (err=%v)\n\n", result, err)
}
