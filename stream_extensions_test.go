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

// --- Empty ---

func TestEmpty(t *testing.T) {
	ctx := context.Background()
	result, err := Empty[int]().ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

// --- Repeat ---

func TestRepeatN(t *testing.T) {
	ctx := context.Background()
	result, err := Repeat(7, 3).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{7, 7, 7}, result)
}

func TestRepeatZero(t *testing.T) {
	ctx := context.Background()
	result, err := Repeat(1, 0).ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRepeatInfiniteWithTake(t *testing.T) {
	ctx := context.Background()
	result, err := Repeat(42, -1).Take(5).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{42, 42, 42, 42, 42}, result)
}

func TestRepeatContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Repeat(1, -1).First(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Generate ---

func TestGenerate(t *testing.T) {
	ctx := context.Background()
	result, err := Generate(1, func(v int) int { return v + 1 }).Take(5).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, result)
}

func TestGenerateNilPanic(t *testing.T) {
	mustPanicContains(t, "non-nil fn", func() {
		Generate[int](0, nil)
	})
}

func TestGenerateContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Generate(0, func(v int) int { return v + 1 }).First(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- TakeWhile ---

func TestTakeWhileBasic(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3, 4, 5}).
		TakeWhile(func(v int) bool { return v < 4 }).
		ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, result)
}

func TestTakeWhileAllPass(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3}).
		TakeWhile(func(v int) bool { return true }).
		ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, result)
}

func TestTakeWhileNonePass(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3}).
		TakeWhile(func(v int) bool { return false }).
		ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestTakeWhileNilPanic(t *testing.T) {
	mustPanicContains(t, "non-nil predicate", func() {
		FromSlice([]int{1}).TakeWhile(nil)
	})
}

// --- DropWhile ---

func TestDropWhileBasic(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3, 4, 5}).
		DropWhile(func(v int) bool { return v < 3 }).
		ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{3, 4, 5}, result)
}

func TestDropWhileAllDrop(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3}).
		DropWhile(func(v int) bool { return true }).
		ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDropWhileNoneDrop(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3}).
		DropWhile(func(v int) bool { return false }).
		ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, result)
}

func TestDropWhileNilPanic(t *testing.T) {
	mustPanicContains(t, "non-nil predicate", func() {
		FromSlice([]int{1}).DropWhile(nil)
	})
}

// --- Any ---

func TestAnyTrue(t *testing.T) {
	ctx := context.Background()
	found, err := FromSlice([]int{1, 2, 3, 4}).Any(ctx, func(v int) bool { return v == 3 })
	require.NoError(t, err)
	assert.True(t, found)
}

func TestAnyFalse(t *testing.T) {
	ctx := context.Background()
	found, err := FromSlice([]int{1, 2, 3}).Any(ctx, func(v int) bool { return v == 99 })
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAnyEmpty(t *testing.T) {
	ctx := context.Background()
	found, err := FromSlice([]int{}).Any(ctx, func(v int) bool { return true })
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAnyNilPanic(t *testing.T) {
	ctx := context.Background()
	mustPanicContains(t, "non-nil predicate", func() {
		FromSlice([]int{1}).Any(ctx, nil)
	})
}

// --- All ---

func TestAllTrue(t *testing.T) {
	ctx := context.Background()
	ok, err := FromSlice([]int{2, 4, 6}).All(ctx, func(v int) bool { return v%2 == 0 })
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestAllFalse(t *testing.T) {
	ctx := context.Background()
	ok, err := FromSlice([]int{2, 3, 6}).All(ctx, func(v int) bool { return v%2 == 0 })
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestAllEmpty(t *testing.T) {
	ctx := context.Background()
	ok, err := FromSlice([]int{}).All(ctx, func(v int) bool { return false })
	require.NoError(t, err)
	assert.True(t, ok) // vacuous truth
}

func TestAllNilPanic(t *testing.T) {
	ctx := context.Background()
	mustPanicContains(t, "non-nil predicate", func() {
		FromSlice([]int{1}).All(ctx, nil)
	})
}

// --- First ---

func TestFirstBasic(t *testing.T) {
	ctx := context.Background()
	val, err := FromSlice([]int{10, 20, 30}).First(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10, val)
}

func TestFirstEmpty(t *testing.T) {
	ctx := context.Background()
	val, err := FromSlice([]int{}).First(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, val)
}

func TestFirstContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := FromSlice([]int{1, 2, 3}).First(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Last ---

func TestLastBasic(t *testing.T) {
	ctx := context.Background()
	val, err := FromSlice([]int{10, 20, 30}).Last(ctx)
	require.NoError(t, err)
	assert.Equal(t, 30, val)
}

func TestLastEmpty(t *testing.T) {
	ctx := context.Background()
	val, err := FromSlice([]int{}).Last(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, val)
}

func TestLastSingle(t *testing.T) {
	ctx := context.Background()
	val, err := FromSlice([]int{42}).Last(ctx)
	require.NoError(t, err)
	assert.Equal(t, 42, val)
}

// --- Scan ---

func TestScanSum(t *testing.T) {
	ctx := context.Background()
	s := Scan(FromSlice([]int{1, 2, 3, 4}), 0, func(acc, v int) int { return acc + v })
	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 3, 6, 10}, result)
}

func TestScanEmpty(t *testing.T) {
	ctx := context.Background()
	s := Scan(FromSlice([]int{}), 0, func(acc, v int) int { return acc + v })
	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestScanNilPanics(t *testing.T) {
	t.Run("nil stream", func(t *testing.T) {
		mustPanicContains(t, "non-nil source", func() {
			Scan[int, int](nil, 0, func(a, b int) int { return a })
		})
	})
	t.Run("nil fn", func(t *testing.T) {
		mustPanicContains(t, "non-nil accumulator", func() {
			Scan[int, int](FromSlice([]int{1}), 0, nil)
		})
	})
}

// --- Zip ---

func TestZipBasic(t *testing.T) {
	ctx := context.Background()
	a := FromSlice([]int{1, 2, 3})
	b := FromSlice([]string{"a", "b", "c"})
	result, err := Zip(a, b).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Pair[int, string]{
		{First: 1, Second: "a"},
		{First: 2, Second: "b"},
		{First: 3, Second: "c"},
	}, result)
}

func TestZipUnequalFirst(t *testing.T) {
	ctx := context.Background()
	a := FromSlice([]int{1, 2})
	b := FromSlice([]string{"a", "b", "c"})
	result, err := Zip(a, b).ToSlice(ctx)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestZipUnequalSecond(t *testing.T) {
	ctx := context.Background()
	a := FromSlice([]int{1, 2, 3})
	b := FromSlice([]string{"a"})
	result, err := Zip(a, b).ToSlice(ctx)
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestZipEmpty(t *testing.T) {
	ctx := context.Background()
	a := FromSlice([]int{})
	b := FromSlice([]string{"a"})
	result, err := Zip(a, b).ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestZipNilPanics(t *testing.T) {
	t.Run("nil first", func(t *testing.T) {
		mustPanicContains(t, "non-nil first", func() {
			Zip[int, string](nil, FromSlice([]string{"a"}))
		})
	})
	t.Run("nil second", func(t *testing.T) {
		mustPanicContains(t, "non-nil second", func() {
			Zip(FromSlice([]int{1}), (*Stream[string])(nil))
		})
	})
}

// --- SpawnResult nil guards ---

func TestSpawnResultNilPanics(t *testing.T) {
	t.Run("nil spawner", func(t *testing.T) {
		mustPanicContains(t, "non-nil spawner", func() {
			SpawnResult[int](nil, "test", func(ctx context.Context) (int, error) { return 0, nil })
		})
	})
	t.Run("nil fn", func(t *testing.T) {
		mustPanicContains(t, "non-nil fn", func() {
			_ = Run(context.Background(), func(sp Spawner) {
				SpawnResult[int](sp, "test", nil)
			})
		})
	})
}
