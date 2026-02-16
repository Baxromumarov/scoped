package scoped

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallelMapOrderedStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const n = 1000
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}

	ctx := context.Background()
	err := Run(ctx, func(sp Spawner) {
		src := FromSlice(items)
		pm := ParallelMap(ctx, sp, src, StreamOptions{
			MaxWorkers: 20,
			Ordered:    true,
		}, func(ctx context.Context, v int) (int, error) {
			// Random delays to force out-of-order completion.
			time.Sleep(time.Duration(rand.IntN(5)) * time.Millisecond)
			return v * 10, nil
		})

		result, err := pm.ToSlice(ctx)
		require.NoError(t, err)
		require.Len(t, result, n)

		// Verify strict ordering.
		for i, v := range result {
			assert.Equal(t, i*10, v, "item %d out of order", i)
		}
	})
	require.NoError(t, err)
}
