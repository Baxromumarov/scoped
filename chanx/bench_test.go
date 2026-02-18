package chanx

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkMerge(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				chs := make([]<-chan int, 4)
				for i := range chs {
					ch := make(chan int, n/4)
					for j := range n / 4 {
						ch <- j
					}
					close(ch)
					chs[i] = ch
				}
				out := Merge(ctx, chs...)
				for range out {
				}
			}
		})
	}
}

func BenchmarkTee(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				outs := Tee(ctx, in, 3)
				done := make(chan struct{})
				for _, out := range outs {
					go func() {
						for range out {
						}
						done <- struct{}{}
					}()
				}
				for range outs {
					<-done
				}
			}
		})
	}
}

func BenchmarkFanOut(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				outs := FanOut(ctx, in, 4)
				done := make(chan struct{})
				for _, out := range outs {
					go func() {
						for range out {
						}
						done <- struct{}{}
					}()
				}
				for range outs {
					<-done
				}
			}
		})
	}
}

func BenchmarkBuffer(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Buffer(ctx, in, 10, time.Second)
				for range out {
				}
			}
		})
	}
}

func BenchmarkThrottle(b *testing.B) {
	b.Run("items=100", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			ctx := context.Background()
			in := make(chan int, 100)
			for i := range 100 {
				in <- i
			}
			close(in)
			// High rate so benchmark doesn't wait
			out := Throttle(ctx, in, 100000, time.Second)
			for range out {
			}
		}
	})
}

func BenchmarkOrDone(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := OrDone(ctx, in)
				for range out {
				}
			}
		})
	}
}

func BenchmarkFilter(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Filter(ctx, in, func(v int) bool { return v%2 == 0 })
				for range out {
				}
			}
		})
	}
}

func BenchmarkMapChanx(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Map(ctx, in, func(v int) int { return v * 2 })
				for range out {
				}
			}
		})
	}
}

func BenchmarkZip(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				chA := make(chan int, n)
				chB := make(chan int, n)
				for i := range n {
					chA <- i
					chB <- i * 10
				}
				close(chA)
				close(chB)
				out := Zip(ctx, chA, chB)
				for range out {
				}
			}
		})
	}
}

func BenchmarkBroadcast(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				outs := Broadcast(ctx, in, 3, n)
				done := make(chan struct{})
				for _, out := range outs {
					go func() {
						for range out {
						}
						done <- struct{}{}
					}()
				}
				for range 3 {
					<-done
				}
			}
		})
	}
}

func BenchmarkDebounce(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Debounce(ctx, in, time.Microsecond)
				for range out {
				}
			}
		})
	}
}

func BenchmarkWindow(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("Tumbling/items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Window(ctx, in, time.Microsecond, Tumbling)
				for range out {
				}
			}
		})
	}
}

func BenchmarkTake(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Take(ctx, in, n/2)
				for range out {
				}
			}
		})
	}
}

func BenchmarkSkip(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Skip(ctx, in, n/2)
				for range out {
				}
			}
		})
	}
}

func BenchmarkScanChanx(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ctx := context.Background()
				in := make(chan int, n)
				for i := range n {
					in <- i
				}
				close(in)
				out := Scan(ctx, in, 0, func(acc, v int) int { return acc + v })
				for range out {
				}
			}
		})
	}
}
