package scoped

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStallDetector_BasicDetection(t *testing.T) {
	var stalled atomic.Int32
	var stalledName atomic.Value

	sc, sp := New(context.Background(),
		WithStallDetector(50*time.Millisecond, func(rt RunningTask) {
			stalled.Add(1)
			stalledName.Store(rt.Name)
		}),
	)

	sp.Go("slow-task", func(ctx context.Context) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})

	err := sc.Wait()
	require.NoError(t, err)

	assert.True(t, stalled.Load() >= 1, "stall callback should have fired at least once")
	assert.Equal(t, "slow-task", stalledName.Load().(string))
}

func TestStallDetector_NoStalledTasks(t *testing.T) {
	var stalled atomic.Int32

	err := Run(context.Background(), func(sp Spawner) {
		for range 5 {
			sp.Go("fast", func(ctx context.Context) error {
				return nil
			})
		}
	}, WithStallDetector(5*time.Second, func(rt RunningTask) {
		stalled.Add(1)
	}))

	require.NoError(t, err)
	assert.Equal(t, int32(0), stalled.Load(), "no tasks should be stalled")
}

func TestStallDetector_MultipleStalledTasks(t *testing.T) {
	var mu sync.Mutex
	stalledNames := map[string]bool{}

	sc, sp := New(context.Background(),
		WithStallDetector(50*time.Millisecond, func(rt RunningTask) {
			mu.Lock()
			stalledNames[rt.Name] = true
			mu.Unlock()
		}),
	)

	for _, name := range []string{"task-a", "task-b", "task-c"} {
		sp.Go(name, func(ctx context.Context) error {
			time.Sleep(200 * time.Millisecond)
			return nil
		})
	}

	err := sc.Wait()
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, stalledNames["task-a"], "task-a should be detected as stalled")
	assert.True(t, stalledNames["task-b"], "task-b should be detected as stalled")
	assert.True(t, stalledNames["task-c"], "task-c should be detected as stalled")
}

func TestStallDetector_ElapsedIsPopulated(t *testing.T) {
	var gotElapsed time.Duration

	sc, sp := New(context.Background(),
		WithStallDetector(30*time.Millisecond, func(rt RunningTask) {
			if gotElapsed == 0 {
				gotElapsed = rt.Elapsed
			}
		}),
	)

	sp.Go("slow", func(ctx context.Context) error {
		time.Sleep(150 * time.Millisecond)
		return nil
	})

	_ = sc.Wait()
	assert.True(t, gotElapsed >= 30*time.Millisecond, "elapsed should be >= threshold")
}

func TestStallDetector_Panics(t *testing.T) {
	mustPanic(t, "WithStallDetector requires threshold > 0", func() {
		WithStallDetector(0, func(RunningTask) {})
	})
	mustPanic(t, "WithStallDetector requires non-nil callback", func() {
		WithStallDetector(time.Second, nil)
	})
}

func TestSnapshot_WithTaskTracking(t *testing.T) {
	blocker := make(chan struct{})

	sc, sp := New(context.Background(), WithTaskTracking())

	sp.Go("blocking-task", func(ctx context.Context) error {
		<-blocker
		return nil
	})

	// Give the goroutine time to start and register.
	time.Sleep(30 * time.Millisecond)

	snap := sc.Snapshot()
	assert.Equal(t, int64(1), snap.Metrics.ActiveTasks)
	assert.Len(t, snap.RunningTasks, 1)
	assert.Equal(t, "blocking-task", snap.RunningTasks[0].Name)
	assert.True(t, snap.RunningTasks[0].Elapsed > 0)
	assert.True(t, snap.LongestActive > 0)

	close(blocker)
	_ = sc.Wait()
}

func TestSnapshot_WithoutTracking(t *testing.T) {
	err := Run(context.Background(), func(sp Spawner) {
		// No tracking enabled — can't call Snapshot on Run.
		// Use New instead.
	})
	require.NoError(t, err)

	// Test with New.
	sc, sp := New(context.Background())
	sp.Go("task", func(ctx context.Context) error { return nil })
	_ = sc.Wait()

	snap := sc.Snapshot()
	assert.Nil(t, snap.RunningTasks, "RunningTasks should be nil without tracking")
	assert.Equal(t, time.Duration(0), snap.LongestActive)
}

func TestWithTaskTracking_EnablesTracking(t *testing.T) {
	blocker := make(chan struct{})

	sc, sp := New(context.Background(), WithTaskTracking())

	sp.Go("a", func(ctx context.Context) error {
		<-blocker
		return nil
	})
	sp.Go("b", func(ctx context.Context) error {
		<-blocker
		return nil
	})

	time.Sleep(30 * time.Millisecond)

	snap := sc.Snapshot()
	assert.Len(t, snap.RunningTasks, 2)

	close(blocker)
	_ = sc.Wait()

	snap = sc.Snapshot()
	assert.Len(t, snap.RunningTasks, 0)
}

func TestSnapshot_MetricsIncludeLongestActive(t *testing.T) {
	blocker := make(chan struct{})

	sc, sp := New(context.Background(), WithTaskTracking())

	sp.Go("long", func(ctx context.Context) error {
		<-blocker
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	snap := sc.Snapshot()
	assert.True(t, snap.Metrics.LongestActive >= 40*time.Millisecond,
		"LongestActive in Metrics should reflect running task")

	close(blocker)
	_ = sc.Wait()
}
