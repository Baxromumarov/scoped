package scoped_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/baxromumarov/scoped"
	"github.com/sourcegraph/conc"
	concpool "github.com/sourcegraph/conc/pool"
	conciter "github.com/sourcegraph/conc/iter"
	"golang.org/x/sync/errgroup"
)

// ─────────────────────────────────────────────────────────────────────────────
// 1. Fan-out: spawn N no-op goroutines and wait
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkFanOut_Native(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				for range n {
					wg.Add(1)
					go func() { wg.Done() }()
				}
				wg.Wait()
			}
		})
	}
}

func BenchmarkFanOut_Errgroup(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				g, _ := errgroup.WithContext(context.Background())
				for range n {
					g.Go(func() error { return nil })
				}
				_ = g.Wait()
			}
		})
	}
}

func BenchmarkFanOut_Conc(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				wg := conc.NewWaitGroup()
				for range n {
					wg.Go(func() {})
				}
				wg.Wait()
			}
		})
	}
}

func BenchmarkFanOut_Scoped(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
					for range n {
						sp.Go("", func(ctx context.Context) error { return nil })
					}
				})
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Limited concurrency: spawn N tasks with max 10 concurrent
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkLimited_Native(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				sem := make(chan struct{}, 10)
				for range n {
					wg.Add(1)
					sem <- struct{}{}
					go func() {
						defer func() { <-sem; wg.Done() }()
					}()
				}
				wg.Wait()
			}
		})
	}
}

func BenchmarkLimited_Errgroup(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				g, _ := errgroup.WithContext(context.Background())
				g.SetLimit(10)
				for range n {
					g.Go(func() error { return nil })
				}
				_ = g.Wait()
			}
		})
	}
}

func BenchmarkLimited_ConcPool(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				p := concpool.New().WithMaxGoroutines(10)
				for range n {
					p.Go(func() {})
				}
				p.Wait()
			}
		})
	}
}

func BenchmarkLimited_Scoped(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
					for range n {
						sp.Go("", func(ctx context.Context) error { return nil })
					}
				}, scoped.WithLimit(10))
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Error propagation: first error cancels remaining work (fail-fast)
// ─────────────────────────────────────────────────────────────────────────────

var errSentinel = errors.New("boom")

func BenchmarkFailFast_Errgroup(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		g, ctx := errgroup.WithContext(context.Background())
		for j := range 50 {
			g.Go(func() error {
				if j == 25 {
					return errSentinel
				}
				<-ctx.Done()
				return nil
			})
		}
		_ = g.Wait()
	}
}

// Note: conc does not natively support fail-fast (context cancellation on
// first error). Its error pool collects all errors. We benchmark the closest
// equivalent: a context pool where tasks check ctx themselves.
func BenchmarkFailFast_ConcPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		p := concpool.New().WithErrors().WithContext(ctx)
		for j := range 50 {
			p.Go(func(ctx context.Context) error {
				if j == 25 {
					cancel()
					return errSentinel
				}
				<-ctx.Done()
				return nil
			})
		}
		_ = p.Wait()
		cancel() // ensure cleanup
	}
}

func BenchmarkFailFast_Scoped(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
			for j := range 50 {
				sp.Go("", func(ctx context.Context) error {
					if j == 25 {
						return errSentinel
					}
					<-ctx.Done()
					return nil
				})
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. ForEach over a slice (parallel iteration)
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkForEach_Native(b *testing.B) {
	items := makeItems(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)
		for idx := range items {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				items[idx] *= 2
				items[idx] /= 2
			}()
		}
		wg.Wait()
	}
}

func BenchmarkForEach_Errgroup(b *testing.B) {
	items := makeItems(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g, _ := errgroup.WithContext(context.Background())
		g.SetLimit(10)
		for idx := range items {
			g.Go(func() error {
				items[idx] *= 2
				items[idx] /= 2
				return nil
			})
		}
		_ = g.Wait()
	}
}

func BenchmarkForEach_ConcIter(b *testing.B) {
	items := makeItems(1000)
	iter := conciter.Iterator[int]{MaxGoroutines: 10}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter.ForEach(items, func(v *int) {
			*v *= 2
			*v /= 2
		})
	}
}

func BenchmarkForEach_Scoped(b *testing.B) {
	items := makeItems(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scoped.ForEachSlice(context.Background(), items, func(ctx context.Context, v int) error {
			_ = v * 2
			return nil
		}, scoped.WithLimit(10))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Map (collect results from parallel work)
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkMapSlice_Native(b *testing.B) {
	items := makeItems(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := make([]int, len(items))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)
		for idx, v := range items {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				results[idx] = v * 2
			}()
		}
		wg.Wait()
		_ = results
	}
}

func BenchmarkMapSlice_ConcResult(b *testing.B) {
	items := makeItems(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mapper := conciter.Mapper[int, int]{MaxGoroutines: 10}
		results := mapper.Map(items, func(v *int) int {
			return *v * 2
		})
		_ = results
	}
}

func BenchmarkMapSlice_Scoped(b *testing.B) {
	items := makeItems(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, _ := scoped.MapSlice(context.Background(), items, func(ctx context.Context, v int) (int, error) {
			return v * 2, nil
		}, scoped.WithLimit(10))
		_ = results
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Result collection: spawn tasks that produce typed results
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkResult_Native(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		results := make([]int, 50)
		var wg sync.WaitGroup
		for j := range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[j] = j * 2
			}()
		}
		wg.Wait()
		_ = results
	}
}

func BenchmarkResult_ConcResultPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := concpool.NewWithResults[int]().WithMaxGoroutines(10)
		for j := range 50 {
			p.Go(func() int { return j * 2 })
		}
		_ = p.Wait()
	}
}

func BenchmarkResult_Scoped(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var results [50]*scoped.Result[int]
		_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
			for j := range 50 {
				results[j] = scoped.SpawnResult(sp, "", func(ctx context.Context) (int, error) {
					return j * 2, nil
				})
			}
		})
		for _, r := range results {
			_, _ = r.Wait()
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Lightweight work: measure overhead per task (single goroutine at a time)
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkOverheadPerTask_Native(b *testing.B) {
	b.ReportAllocs()
	var counter atomic.Int64
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			counter.Add(1)
			wg.Done()
		}()
		wg.Wait()
	}
}

func BenchmarkOverheadPerTask_Errgroup(b *testing.B) {
	b.ReportAllocs()
	var counter atomic.Int64
	for i := 0; i < b.N; i++ {
		g, _ := errgroup.WithContext(context.Background())
		g.Go(func() error {
			counter.Add(1)
			return nil
		})
		_ = g.Wait()
	}
}

func BenchmarkOverheadPerTask_Conc(b *testing.B) {
	b.ReportAllocs()
	var counter atomic.Int64
	for i := 0; i < b.N; i++ {
		wg := conc.NewWaitGroup()
		wg.Go(func() {
			counter.Add(1)
		})
		wg.Wait()
	}
}

func BenchmarkOverheadPerTask_Scoped(b *testing.B) {
	b.ReportAllocs()
	var counter atomic.Int64
	for i := 0; i < b.N; i++ {
		_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
			sp.Go("", func(ctx context.Context) error {
				counter.Add(1)
				return nil
			})
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func makeItems(n int) []int {
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}
	return items
}
