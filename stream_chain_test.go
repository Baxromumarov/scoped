package scoped

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamOperatorChain(t *testing.T) {
	ctx := context.Background()

	t.Run("MapFilterTakeBatch", func(t *testing.T) {
		// Input: 0..99
		// Map: x * 3
		// Filter: even only
		// Take: first 10
		// Batch: groups of 3
		items := make([]int, 100)
		for i := range items {
			items[i] = i
		}

		s := FromSlice(items)
		mapped := Map(s, func(_ context.Context, v int) (int, error) {
			return v * 3, nil
		})
		filtered := mapped.Filter(func(v int) bool { return v%2 == 0 })
		taken := filtered.Take(10)
		batched := Batch(taken, 3)

		result, err := batched.ToSlice(ctx)
		require.NoError(t, err)

		expected := [][]int{
			{0, 6, 12},
			{18, 24, 30},
			{36, 42, 48},
			{54},
		}
		assert.Equal(t, expected, result)
	})

	t.Run("SkipDistinctReduce", func(t *testing.T) {
		items := []int{1, 2, 2, 3, 3, 3, 4, 5}
		s := FromSlice(items).Skip(1)
		d := Distinct(s)

		result, err := Reduce(ctx, d, 0, func(acc, v int) int { return acc + v })
		require.NoError(t, err)
		// After Skip(1): [2, 2, 3, 3, 3, 4, 5]
		// After Distinct: [2, 3, 4, 5]
		// Reduce sum: 14
		assert.Equal(t, 14, result)
	})

	t.Run("FlatMapFilterTake", func(t *testing.T) {
		items := []int{1, 2, 3}
		fm := FlatMap(FromSlice(items), func(_ context.Context, v int) *Stream[int] {
			expanded := make([]int, v)
			for i := range expanded {
				expanded[i] = v
			}
			return FromSlice(expanded)
		})
		// FlatMap produces: [1, 2, 2, 3, 3, 3]
		taken := fm.Filter(func(v int) bool { return v > 1 }).Take(4)

		result, err := taken.ToSlice(ctx)
		require.NoError(t, err)
		assert.Equal(t, []int{2, 2, 3, 3}, result)
	})

	t.Run("ScanTakeWhile", func(t *testing.T) {
		items := []int{1, 2, 3, 4, 5}
		s := Scan(FromSlice(items), 0, func(acc, v int) int { return acc + v })
		// Scan produces: [1, 3, 6, 10, 15]
		tw := s.TakeWhile(func(v int) bool { return v < 10 })

		result, err := tw.ToSlice(ctx)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 3, 6}, result)
	})

	t.Run("CancelledContextStopsChain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel() // pre-cancelled

		items := make([]int, 1000)
		for i := range items {
			items[i] = i
		}
		s := FromSlice(items)
		mapped := Map(s, func(_ context.Context, v int) (int, error) {
			return v * 2, nil
		})

		_, err := mapped.ToSlice(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}
