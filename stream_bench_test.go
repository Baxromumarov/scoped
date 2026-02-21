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
			for i := range size {
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
			for i := range size {
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
			for i := range size {
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
			for i := range size {
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

func BenchmarkFromSlice(b *testing.B) {
	for _, size := range []int{100, 10000, 100000} {
		items := make([]int, size)
		for i := range items {
			items[i] = i
		}
		b.Run(fmt.Sprintf("Safe/Size=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s := FromSlice(items)
				_, _ = s.ToSlice(context.Background())
			}
		})
		b.Run(fmt.Sprintf("Unsafe/Size=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s := FromSliceRef(items)
				_, _ = s.ToSlice(context.Background())
			}
		})
	}
}

func BenchmarkStreamBatch(b *testing.B) {
	for _, size := range []int{100, 10000} {
		items := make([]int, size)
		for i := range items {
			items[i] = i
		}
		for _, batchSize := range []int{10, 100} {
			b.Run(fmt.Sprintf("Size=%d/Batch=%d", size, batchSize), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					s := Batch(FromSliceRef(items), batchSize)
					_, _ = s.ToSlice(context.Background())
				}
			})
		}
	}
}

func BenchmarkStreamDistinct(b *testing.B) {
	for _, card := range []int{10, 1000} {
		size := 10000
		items := make([]int, size)
		for i := range items {
			items[i] = i % card
		}
		b.Run(fmt.Sprintf("Cardinality=%d", card), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s := Distinct(FromSliceRef(items))
				_, _ = s.ToSlice(context.Background())
			}
		})
	}
}

func BenchmarkStreamReduce(b *testing.B) {
	for _, size := range []int{100, 10000, 100000} {
		items := make([]int, size)
		for i := range items {
			items[i] = i
		}
		b.Run(fmt.Sprintf("Size=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			ctx := context.Background()
			for i := 0; i < b.N; i++ {
				_, _ = Reduce(ctx, FromSliceRef(items), 0, func(acc, v int) int { return acc + v })
			}
		})
	}
}

func BenchmarkStreamFlatMap(b *testing.B) {
	sub := []int{1, 2, 3, 4, 5}
	for _, size := range []int{100, 1000} {
		items := make([]int, size)
		for i := range items {
			items[i] = i
		}
		b.Run(fmt.Sprintf("Size=%d/SubSize=5", size), func(b *testing.B) {
			b.ReportAllocs()
			ctx := context.Background()
			for i := 0; i < b.N; i++ {
				fm := FlatMap(FromSliceRef(items), func(_ context.Context, _ int) *Stream[int] {
					return FromSliceRef(sub)
				})
				_, _ = fm.ToSlice(ctx)
			}
		})
	}
}

func BenchmarkStreamScan(b *testing.B) {
	for _, size := range []int{100, 10000} {
		items := make([]int, size)
		for i := range items {
			items[i] = i
		}
		b.Run(fmt.Sprintf("Size=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			ctx := context.Background()
			for i := 0; i < b.N; i++ {
				s := Scan(FromSliceRef(items), 0, func(acc, v int) int { return acc + v })
				_, _ = s.ToSlice(ctx)
			}
		})
	}
}

func BenchmarkHeavyParallel(b *testing.B) {
	size := 10000
	items := make([]int, size)
	for i := range size {
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
					for j := range 1000 {
						acc += j
					}
					return v + acc, nil
				},
			)
			_, _ = pm.ToSlice(context.Background())
		})
	}
}
