package scoped_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/baxromumarov/scoped"
)

// BenchmarkRunNoWork measures the overhead of spawning N tasks
// that do nothing, compared to raw goroutines + WaitGroup.
func BenchmarkRunNoWork(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(taskCountName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = scoped.Run(context.Background(), func(s scoped.Spawner) {
					for j := 0; j < n; j++ {
						s.Go("", func(ctx context.Context, _ scoped.Spawner) error {
							return nil
						})
					}
				})
			}
		})
	}
}

// BenchmarkRunWithLimit measures bounded concurrency overhead.
func BenchmarkRunWithLimit(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(taskCountName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = scoped.Run(context.Background(), func(s scoped.Spawner) {
					for j := 0; j < n; j++ {
						s.Go("", func(ctx context.Context, _ scoped.Spawner) error {
							return nil
						})
					}
				}, scoped.WithLimit(10))
			}
		})
	}
}

// BenchmarkRawGoroutineWaitGroup is the baseline: raw go + sync.WaitGroup.
func BenchmarkRawGoroutineWaitGroup(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(taskCountName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				for j := 0; j < n; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
					}()
				}
				wg.Wait()
			}
		})
	}
}

// BenchmarkGoResult measures the overhead of typed result collection.
func BenchmarkGoResult(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var results [10]*scoped.Result[int]
		_ = scoped.Run(context.Background(), func(s scoped.Spawner) {
			for j := 0; j < 10; j++ {
				j := j
				results[j] = scoped.GoResult(s, "", func(ctx context.Context) (int, error) {
					return j * 2, nil
				})
			}
		})
		for _, r := range results {
			_, _ = r.Wait()
		}
	}
}

// BenchmarkForEach measures ForEachSlice helper overhead.
func BenchmarkForEach(b *testing.B) {
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scoped.ForEachSlice(context.Background(), items, func(ctx context.Context, item int) error {
			return nil
		}, scoped.WithLimit(10))
	}
}

// BenchmarkMap measures MapSlice helper overhead.
func BenchmarkMap(b *testing.B) {
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scoped.MapSlice(context.Background(), items, func(ctx context.Context, item int) (int, error) {
			return item * 2, nil
		}, scoped.WithLimit(10))
	}
}

func taskCountName(n int) string {
	return fmt.Sprintf("%d", n)
}
