// Package main demonstrates the monitoring and observability features of scoped:
// scope metrics, stall detection, task snapshots, pool stats, and stream monitoring.
package main

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/baxromumarov/scoped"
)

func main() {
	scopeMetricsExample()
	stallDetectorExample()
	taskSnapshotExample()
	poolStatsExample()
	poolMetricsExample()
	streamStatsExample()
	streamObserveExample()
}

// scopeMetricsExample shows periodic scope metrics via WithOnMetrics.
func scopeMetricsExample() {
	fmt.Println("=== Scope Periodic Metrics ===")

	var lastMetrics atomic.Value

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := range 5 {
			name := fmt.Sprintf("task-%d", i)
			sp.Go(name, func(ctx context.Context) error {
				time.Sleep(50 * time.Millisecond)
				return nil
			})
		}
	},
		scoped.WithOnMetrics(25*time.Millisecond, func(m scoped.Metrics) {
			lastMetrics.Store(m)
			fmt.Printf("  [metrics tick] active=%d completed=%d total=%d\n",
				m.ActiveTasks, m.Completed, m.TotalSpawned)
		}),
	)

	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}

	if v := lastMetrics.Load(); v != nil {
		m := v.(scoped.Metrics)
		fmt.Printf("  final: total=%d completed=%d errored=%d\n",
			m.TotalSpawned, m.Completed, m.Errored)
	}
	fmt.Println()
}

// stallDetectorExample shows how to detect tasks that run longer than expected.
func stallDetectorExample() {
	fmt.Println("=== Stall Detector ===")

	var stalledCount atomic.Int32

	sc, sp := scoped.New(context.Background(),
		// Fire callback for any task running longer than 100ms.
		scoped.WithStallDetector(100*time.Millisecond, func(rt scoped.RunningTask) {
			stalledCount.Add(1)
			fmt.Printf("  [stall] %q has been running for %v\n", rt.Name, rt.Elapsed.Round(time.Millisecond))
		}),
	)

	// This task finishes quickly — no stall alert.
	sp.Go("fast-task", func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// This task takes longer than the threshold — stall callback fires.
	sp.Go("slow-task", func(ctx context.Context) error {
		time.Sleep(300 * time.Millisecond)
		return nil
	})

	err := sc.Wait()
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}

	fmt.Printf("  stall callbacks fired: %d\n\n", stalledCount.Load())
}

// taskSnapshotExample shows how to take a point-in-time snapshot of running tasks.
func taskSnapshotExample() {
	fmt.Println("=== Task Snapshot ===")

	blocker := make(chan struct{})

	sc, sp := scoped.New(context.Background(), scoped.WithTaskTracking())

	sp.Go("download-file", func(ctx context.Context) error {
		<-blocker
		return nil
	})
	sp.Go("process-data", func(ctx context.Context) error {
		<-blocker
		return nil
	})

	// Give goroutines time to start and register.
	time.Sleep(30 * time.Millisecond)

	snap := sc.Snapshot()
	fmt.Printf("  active tasks: %d\n", snap.Metrics.ActiveTasks)
	fmt.Printf("  longest active: %v\n", snap.LongestActive.Round(time.Millisecond))
	for _, rt := range snap.RunningTasks {
		fmt.Printf("  - %q running for %v\n", rt.Name, rt.Elapsed.Round(time.Millisecond))
	}

	close(blocker)
	_ = sc.Wait()

	// After completion, snapshot shows zero running tasks.
	snap = sc.Snapshot()
	fmt.Printf("  after wait: running=%d completed=%d\n\n",
		len(snap.RunningTasks), snap.Metrics.Completed)
}

// poolStatsExample shows how to inspect pool counters on demand.
func poolStatsExample() {
	fmt.Println("=== Pool Stats ===")

	ctx := context.Background()
	pool := scoped.NewPool(ctx, 3)

	// Submit several tasks.
	for i := range 10 {
		i := i
		_ = pool.Submit(func() error {
			time.Sleep(20 * time.Millisecond)
			if i == 7 {
				return fmt.Errorf("task %d failed", i)
			}
			return nil
		})
	}

	// Check stats while tasks are running.
	stats := pool.Stats()
	fmt.Printf("  mid-run: submitted=%d in-flight=%d workers=%d queue=%d\n",
		stats.Submitted, stats.InFlight, stats.Workers, stats.QueueDepth)

	err := pool.Close()
	if err != nil {
		fmt.Printf("  pool errors: %v\n", err)
	}

	stats = pool.Stats()
	fmt.Printf("  final: submitted=%d completed=%d errored=%d\n\n",
		stats.Submitted, stats.Completed, stats.Errored)
}

// poolMetricsExample shows periodic pool metrics via WithPoolMetrics.
func poolMetricsExample() {
	fmt.Println("=== Pool Periodic Metrics ===")

	ctx := context.Background()
	pool := scoped.NewPool(ctx, 2,
		scoped.WithPoolMetrics(50*time.Millisecond, func(ps scoped.PoolStats) {
			fmt.Printf("  [pool tick] in-flight=%d completed=%d queue=%d\n",
				ps.InFlight, ps.Completed, ps.QueueDepth)
		}),
	)

	for range 8 {
		_ = pool.Submit(func() error {
			time.Sleep(60 * time.Millisecond)
			return nil
		})
	}

	err := pool.Close()
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}

	stats := pool.Stats()
	fmt.Printf("  final: submitted=%d completed=%d\n\n", stats.Submitted, stats.Completed)
}

// streamStatsExample shows how to check stream throughput and read counts.
func streamStatsExample() {
	fmt.Println("=== Stream Stats ===")

	// Simulate a data source with a small delay per item.
	callCount := 0
	s := scoped.NewStream(func(ctx context.Context) (int, error) {
		callCount++
		if callCount > 100 {
			return 0, io.EOF
		}
		time.Sleep(time.Millisecond)
		return callCount, nil
	})

	ctx := context.Background()
	items, err := s.ToSlice(ctx)
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}

	stats := s.Stats()
	fmt.Printf("  items collected: %d\n", len(items))
	fmt.Printf("  items read: %d\n", stats.ItemsRead)
	fmt.Printf("  errors: %d\n", stats.Errors)
	fmt.Printf("  throughput: %.1f items/sec\n", stats.Throughput)
	fmt.Printf("  duration: %v\n\n", stats.LastReadAt.Sub(stats.StartTime).Round(time.Millisecond))
}

// streamObserveExample shows per-item event hooks via Observe.
func streamObserveExample() {
	fmt.Println("=== Stream Observe ===")

	src := scoped.FromSlice([]string{"alpha", "bravo", "charlie", "delta"})

	var totalDuration time.Duration
	observed := scoped.Observe(src, func(e scoped.StreamEvent[string]) {
		if e.EOF {
			fmt.Printf("  [observe] stream ended after %d items\n", e.Seq)
			return
		}
		if e.Err != nil {
			fmt.Printf("  [observe] error at seq %d: %v\n", e.Seq, e.Err)
			return
		}
		totalDuration += e.Duration
		fmt.Printf("  [observe] seq=%d item=%q took=%v\n", e.Seq, e.Item, e.Duration)
	})

	items, err := observed.ToSlice(context.Background())
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}
	fmt.Printf("  collected %d items, total observe duration: %v\n\n", len(items), totalDuration)
}
