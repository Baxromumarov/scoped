package scoped

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Peek edge cases ---

func TestPeekWithError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("peek-error")
	count := 0
	var peeked []int

	s := NewStream(func(ctx context.Context) (int, error) {
		count++
		if count <= 2 {
			return count, nil
		}
		return 0, sentinel
	}).Peek(func(v int) {
		peeked = append(peeked, v)
	})

	result, err := s.ToSlice(ctx)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, []int{1, 2}, result)
	assert.Equal(t, []int{1, 2}, peeked, "peek should only be called for non-error items")
}

func TestPeekContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	var peeked []int
	s := FromSlice([]int{1, 2, 3}).Peek(func(v int) {
		peeked = append(peeked, v)
	})

	_, err := s.ToSlice(ctx)
	assert.Error(t, err)
	assert.Empty(t, peeked, "peek should not be called when context is already cancelled")
}

func TestPeekWithTake(t *testing.T) {
	ctx := context.Background()
	var peeked []int

	s := FromSlice([]int{1, 2, 3, 4, 5}).
		Peek(func(v int) { peeked = append(peeked, v) }).
		Take(2)

	result, err := s.ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, result)
	assert.Equal(t, []int{1, 2}, peeked, "peek should be called exactly twice")
}

// --- Skip edge cases ---

func TestSkipZero(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3}).Skip(0).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, result)
}

func TestSkipMoreThanLength(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3}).Skip(10).ToSlice(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestSkipWithError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("skip-error")
	count := 0
	s := NewStream(func(ctx context.Context) (int, error) {
		count++
		if count <= 1 {
			return count, nil
		}
		return 0, sentinel
	}).Skip(5) // try to skip 5 but error occurs at item 2

	_, err := s.ToSlice(ctx)
	assert.ErrorIs(t, err, sentinel)
}

func TestSkipThenTake(t *testing.T) {
	ctx := context.Background()
	result, err := FromSlice([]int{1, 2, 3, 4, 5}).Skip(2).Take(2).ToSlice(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{3, 4}, result)
}

func TestSkipContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FromSlice([]int{1, 2, 3}).Skip(1).ToSlice(ctx)
	assert.Error(t, err)
}
