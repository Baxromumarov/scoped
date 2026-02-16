package scoped

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithOnMetrics(t *testing.T) {
	var mu sync.Mutex
	var snapshots []Metrics

	err := Run(
		context.Background(),
		func(sp Spawner) {
			// 5 success tasks
			for i := range 5 {
				sp.Spawn("ok-"+string(rune('a'+i)), func(ctx context.Context, _ Spawner) error {
					time.Sleep(30 * time.Millisecond)
					return nil
				})
			}
			// 2 error tasks
			for i := range 2 {
				sp.Spawn("err-"+string(rune('a'+i)), func(ctx context.Context, _ Spawner) error {
					time.Sleep(10 * time.Millisecond)
					return errors.New("fail")
				})
			}
			// Give metrics time to fire
			time.Sleep(100 * time.Millisecond)
		},
		WithPolicy(Collect),
		WithOnMetrics(20*time.Millisecond, func(m Metrics) {
			mu.Lock()
			snapshots = append(snapshots, m)
			mu.Unlock()
		}),
	)
	// Collect mode: error is joined
	assert.Error(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, snapshots, "should have received at least one metrics snapshot")

	// Check the last snapshot has reasonable values.
	last := snapshots[len(snapshots)-1]
	assert.Equal(t, int64(7), last.TotalSpawned)
	assert.GreaterOrEqual(t, last.Completed, int64(5))
	assert.GreaterOrEqual(t, last.Errored, int64(2))
}

func TestWithOnMetricsPanics(t *testing.T) {
	t.Run("interval<=0", func(t *testing.T) {
		assert.Panics(t, func() {
			WithOnMetrics(0, func(m Metrics) {})
		})
		assert.Panics(t, func() {
			WithOnMetrics(-time.Second, func(m Metrics) {})
		})
	})
	t.Run("nil fn", func(t *testing.T) {
		assert.Panics(t, func() {
			WithOnMetrics(time.Second, nil)
		})
	})
}
