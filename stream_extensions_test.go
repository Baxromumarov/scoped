package scoped

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustPanicContains(t *testing.T, contains string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic")
		require.Contains(t, fmt.Sprint(r), contains)
	}()
	fn()
}

// --- Reduce ---

func TestReduceSum(t *testing.T) {
	ctx := context.Background()
	s := FromSlice([]int{1, 2, 3, 4, 5})
	result, err := Reduce(ctx, s, 0, func(acc, v int) int { return acc + v })
	require.NoError(t, err)
	assert.Equal(t, 15, result)
}

func TestReduceEmpty(t *testing.T) {
	ctx := context.Background()
	s := FromSlice([]int{})
	result, err := Reduce(ctx, s, 42, func(acc, v int) int { return acc + v })
	require.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestReduceError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("boom")
	count := 0
	s := NewStream(func(ctx context.Context) (int, error) {
		count++
		if count <= 3 {
			return count, nil
		}
		return 0, sentinel
	})
	result, err := Reduce(ctx, s, 0, func(acc, v int) int { return acc + v })
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 6, result) // 1+2+3
}

func TestReduceNilPanics(t *testing.T) {
	ctx := context.Background()
	t.Run("nil stream", func(t *testing.T) {
		mustPanicContains(t, "non-nil source", func() {
			Reduce[int, int](ctx, nil, 0, func(a, b int) int { return a + b })
		})
	})
	t.Run("nil fn", func(t *testing.T) {
		mustPanicContains(t, "non-nil accumulator", func() {
			Reduce[int, int](ctx, FromSlice([]int{1}), 0, nil)
		})
	})
}

// --- FlatMap ---

func TestFlatMapBasic(t *testing.T) {
	ctx := context.Background()
	s := FromSlice([]int{1, 2, 3})
	fm := FlatMap(s, func(ctx context.Context, v int) *Stream[int] {
		return FromSlice([]int{v, v * 10})
	})
	result, err := fm.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 10, 2, 20, 3, 30}, result)
}

func TestFlatMapEmpty(t *testing.T) {
	ctx := context.Background()
	s := FromSlice([]int{})
	fm := FlatMap(s, func(ctx context.Context, v int) *Stream[int] {
		return FromSlice([]int{v})
	})
	result, err := fm.ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFlatMapNilSubStream(t *testing.T) {
	ctx := context.Background()
	s := FromSlice([]int{1, 2, 3})
	fm := FlatMap(s, func(ctx context.Context, v int) *Stream[int] {
		if v == 2 {
			return nil // skip
		}
		return FromSlice([]int{v * 10})
	})
	result, err := fm.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{10, 30}, result)
}

func TestFlatMapError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("sub-error")
	s := FromSlice([]int{1, 2})
	fm := FlatMap(s, func(ctx context.Context, v int) *Stream[int] {
		if v == 2 {
			return NewStream(func(ctx context.Context) (int, error) {
				return 0, sentinel
			})
		}
		return FromSlice([]int{v * 10})
	})
	result, err := fm.ToSlice(ctx)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, []int{10}, result)
}

func TestFlatMapNilPanics(t *testing.T) {
	t.Run("nil stream", func(t *testing.T) {
		mustPanicContains(t, "non-nil source", func() {
			FlatMap[int, int](nil, func(ctx context.Context, v int) *Stream[int] {
				return FromSlice([]int{v})
			})
		})
	})
	t.Run("nil fn", func(t *testing.T) {
		mustPanicContains(t, "non-nil mapper", func() {
			FlatMap[int, int](FromSlice([]int{1}), nil)
		})
	})
}

// --- Distinct ---

func TestDistinctBasic(t *testing.T) {
	ctx := context.Background()
	s := Distinct(FromSlice([]int{1, 2, 2, 3, 1, 4}))
	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4}, result)
}

func TestDistinctAllSame(t *testing.T) {
	ctx := context.Background()
	s := Distinct(FromSlice([]int{5, 5, 5}))
	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{5}, result)
}

func TestDistinctEmpty(t *testing.T) {
	ctx := context.Background()
	s := Distinct(FromSlice([]int{}))
	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDistinctStrings(t *testing.T) {
	ctx := context.Background()
	s := Distinct(FromSlice([]string{"a", "b", "a", "c", "b"}))
	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestDistinctNilPanics(t *testing.T) {
	mustPanicContains(t, "non-nil source", func() {
		Distinct[int](nil)
	})
}

// --- FlatMap stop ---

func TestFlatMapStop(t *testing.T) {
	ctx := context.Background()
	// Take 3 items from a flatmap producing 2 per source item
	s := FromSlice([]int{1, 2, 3, 4, 5})
	fm := FlatMap(s, func(ctx context.Context, v int) *Stream[int] {
		return FromSlice([]int{v, v * 10})
	})
	result, err := fm.Take(3).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 10, 2}, result)
}

// --- Reduce context cancel ---

func TestReduceContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	s := NewStream(func(ctx context.Context) (int, error) {
		return 0, ctx.Err()
	})
	_, err := Reduce(ctx, s, 0, func(a, v int) int { return a + v })
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Distinct with error source ---

func TestDistinctWithError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("oops")
	count := 0
	s := Distinct(NewStream(func(ctx context.Context) (int, error) {
		count++
		if count <= 3 {
			return count, nil
		}
		return 0, sentinel
	}))
	result, err := s.ToSlice(ctx)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, []int{1, 2, 3}, result)
}

// --- FlatMap with EOF sub-stream ---

func TestFlatMapEmptySubStreams(t *testing.T) {
	ctx := context.Background()
	s := FromSlice([]int{1, 2, 3})
	fm := FlatMap(s, func(ctx context.Context, v int) *Stream[int] {
		if v == 2 {
			// Return an empty stream (immediate EOF)
			return NewStream(func(ctx context.Context) (int, error) {
				return 0, io.EOF
			})
		}
		return FromSlice([]int{v * 100})
	})
	result, err := fm.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{100, 300}, result)
}
