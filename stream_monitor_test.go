package scoped

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamStats_BasicCount(t *testing.T) {
	s := FromSlice([]int{1, 2, 3, 4, 5})
	items, err := s.ToSlice(context.Background())
	require.NoError(t, err)
	assert.Len(t, items, 5)

	stats := s.Stats()
	assert.Equal(t, int64(5), stats.ItemsRead)
	assert.Equal(t, int64(0), stats.Errors)
	assert.False(t, stats.StartTime.IsZero())
	assert.False(t, stats.LastReadAt.IsZero())
	assert.True(t, stats.Throughput > 0)
}

func TestStreamStats_ErrorCounting(t *testing.T) {
	sentinel := errors.New("boom")
	callCount := 0
	s := NewStream(func(ctx context.Context) (int, error) {
		callCount++
		if callCount <= 3 {
			return callCount, nil
		}
		if callCount == 4 {
			return 0, sentinel
		}
		return 0, io.EOF
	})

	_, _ = s.ToSlice(context.Background())

	stats := s.Stats()
	assert.Equal(t, int64(3), stats.ItemsRead)
	assert.Equal(t, int64(1), stats.Errors)
}

func TestStreamStats_Empty(t *testing.T) {
	s := Empty[int]()
	_, _ = s.ToSlice(context.Background())

	stats := s.Stats()
	assert.Equal(t, int64(0), stats.ItemsRead)
	assert.False(t, stats.StartTime.IsZero(), "startTime set even for empty stream")
	assert.True(t, stats.LastReadAt.IsZero(), "no items read")
	assert.Equal(t, float64(0), stats.Throughput)
}

func TestStreamStats_NeverCalled(t *testing.T) {
	s := FromSlice([]int{1, 2, 3})
	stats := s.Stats()
	assert.Equal(t, int64(0), stats.ItemsRead)
	assert.True(t, stats.StartTime.IsZero())
}

func TestStreamStats_Throughput(t *testing.T) {
	s := NewStream(func(ctx context.Context) (int, error) {
		time.Sleep(time.Millisecond)
		return 1, nil
	})

	ctx := context.Background()
	for range 5 {
		_, err := s.Next(ctx)
		require.NoError(t, err)
	}

	stats := s.Stats()
	assert.Equal(t, int64(5), stats.ItemsRead)
	assert.True(t, stats.Throughput > 0)
	// With 1ms sleep per item, throughput should be roughly < 2000/sec
	assert.True(t, stats.Throughput < 2000, "throughput should be bounded by sleep")
}

func TestObserve_FiresOnEachItem(t *testing.T) {
	var count atomic.Int64
	s := Observe(FromSlice([]int{10, 20, 30}), func(e StreamEvent[int]) {
		count.Add(1)
	})

	items, err := s.ToSlice(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []int{10, 20, 30}, items)
	// 3 items + 1 EOF event
	assert.Equal(t, int64(4), count.Load())
}

func TestObserve_SequenceNumbers(t *testing.T) {
	var seqs []int64
	s := Observe(FromSlice([]int{1, 2, 3}), func(e StreamEvent[int]) {
		seqs = append(seqs, e.Seq)
	})

	_, _ = s.ToSlice(context.Background())
	assert.Equal(t, []int64{0, 1, 2, 3}, seqs)
}

func TestObserve_EOF(t *testing.T) {
	var gotEOF bool
	s := Observe(FromSlice([]int{1}), func(e StreamEvent[int]) {
		if e.EOF {
			gotEOF = true
		}
	})

	_, _ = s.ToSlice(context.Background())
	assert.True(t, gotEOF, "should have received EOF event")
}

func TestObserve_Error(t *testing.T) {
	sentinel := errors.New("fail")
	callCount := 0
	src := NewStream(func(ctx context.Context) (int, error) {
		callCount++
		if callCount == 1 {
			return 1, nil
		}
		return 0, sentinel
	})

	var gotErr error
	s := Observe(src, func(e StreamEvent[int]) {
		if e.Err != nil {
			gotErr = e.Err
		}
	})

	_, _ = s.ToSlice(context.Background())
	assert.ErrorIs(t, gotErr, sentinel)
}

func TestObserve_IncludesDuration(t *testing.T) {
	callCount := 0
	src := NewStream(func(ctx context.Context) (int, error) {
		callCount++
		if callCount <= 1 {
			time.Sleep(5 * time.Millisecond)
			return 1, nil
		}
		return 0, io.EOF
	})

	var dur time.Duration
	s := Observe(src, func(e StreamEvent[int]) {
		if !e.EOF && e.Err == nil {
			dur = e.Duration
		}
	})

	_, _ = s.ToSlice(context.Background())
	assert.True(t, dur >= 5*time.Millisecond, "duration should reflect sleep time")
}

func TestObserve_Panics(t *testing.T) {
	mustPanicContains(t, "non-nil stream", func() {
		Observe[int](nil, func(StreamEvent[int]) {})
	})
	mustPanicContains(t, "non-nil callback", func() {
		Observe(FromSlice([]int{1}), nil)
	})
}
