package scoped

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkStream(b *testing.B) {
	sizes := []int{100, 10000, 100000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Simple/Size=%d", size), func(b *testing.B) {
			items := make([]int, size)
			for i := 0; i < size; i++ {
				items[i] = i
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s := FromSlice(items)
				ms := Map(
					s,
					func(ctx context.Context, v int) (int, error) {
						return v * 2, nil
					},
				)
				_, _ = ms.ToSlice(context.Background())
			}
		})

		b.Run(fmt.Sprintf("Fluent/Size=%d", size), func(b *testing.B) {
			items := make([]int, size)
			for i := 0; i < size; i++ {
				items[i] = i
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s := FromSlice(items).
					Filter(func(v int) bool { return v%2 == 0 }).
					Take(size / 2)
				_, _ = s.ToSlice(context.Background())
			}
		})

		b.Run(fmt.Sprintf("ParallelUnordered/Size=%d", size), func(b *testing.B) {
			items := make([]int, size)
			for i := 0; i < size; i++ {
				items[i] = i
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Run(context.Background(), func(s Spawner) {
					src := FromSlice(items)
					pm := ParallelMap(
						context.Background(),
						s,
						src,
						StreamOptions{MaxWorkers: 10},
						func(ctx context.Context, v int) (int, error) {
							return v * 2, nil
						},
					)
					_, _ = pm.ToSlice(context.Background())
				})
			}
		})

		b.Run(fmt.Sprintf("ParallelOrdered/Size=%d", size), func(b *testing.B) {
			items := make([]int, size)
			for i := 0; i < size; i++ {
				items[i] = i
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Run(context.Background(), func(s Spawner) {
					src := FromSlice(items)
					pm := ParallelMap(
						context.Background(),
						s,
						src,
						StreamOptions{
							MaxWorkers: 10,
							Ordered:    true,
						},
						func(ctx context.Context, v int) (int, error) {
							return v * 2, nil
						},
					)
					_, _ = pm.ToSlice(context.Background())
				})
			}
		})
	}
}

func BenchmarkHeavyParallel(b *testing.B) {
	size := 10000
	items := make([]int, size)
	for i := 0; i < size; i++ {
		items[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Run(context.Background(), func(s Spawner) {
			src := FromSlice(items)
			pm := ParallelMap(
				context.Background(),
				s,
				src,
				StreamOptions{
					MaxWorkers: 100,
					Ordered:    true,
				},
				func(ctx context.Context, v int) (int, error) {
					// Heavyish computation
					acc := 0
					for j := 0; j < 1000; j++ {
						acc += j
					}
					return v + acc, nil
				},
			)
			_, _ = pm.ToSlice(context.Background())
		})
	}
}
