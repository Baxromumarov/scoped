package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowTumblingBasic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	// Window duration of 100ms.
	out := Window(ctx, in, 100*time.Millisecond, Tumbling)

	go func() {
		// First window: send 3 items within the first 100ms.
		in <- 1
		in <- 2
		in <- 3
		// Wait for the first window tick to fire and start a second window.
		time.Sleep(150 * time.Millisecond)
		// Second window: send 2 items.
		in <- 4
		in <- 5
		// Wait for the second tick, then close.
		time.Sleep(150 * time.Millisecond)
		close(in)
	}()

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	require.GreaterOrEqual(t, len(batches), 2, "should have at least two batches")

	// Flatten to verify all items arrived.
	var all []int
	for _, b := range batches {
		all = append(all, b...)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, all)
}

func TestWindowTumblingFlush(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 2)

	in <- 10
	in <- 20
	close(in)

	out := Window(ctx, in, 500*time.Millisecond, Tumbling)

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	// Input was closed before the first tick, so a partial window flush should happen.
	require.Len(t, batches, 1)
	assert.Equal(t, []int{10, 20}, batches[0])
}

func TestWindowSlidingBasic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	// Use a large window (500ms) so items don't expire due to timing jitter.
	// Items are sent within ~100ms, well within the window.
	out := Window(ctx, in, 500*time.Millisecond, Sliding)

	go func() {
		// Small delay so item 1 is clearly inside the window (not at the boundary).
		time.Sleep(50 * time.Millisecond)
		in <- 1
		time.Sleep(50 * time.Millisecond)
		in <- 2
		time.Sleep(50 * time.Millisecond)
		in <- 3
		// Wait long enough for at least one tick to fire, then close.
		time.Sleep(600 * time.Millisecond)
		close(in)
	}()

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	// At least one batch should have been emitted.
	require.GreaterOrEqual(t, len(batches), 1)

	// All items should appear across the collected batches.
	seen := make(map[int]bool)
	for _, b := range batches {
		for _, v := range b {
			seen[v] = true
		}
	}
	assert.True(t, seen[1], "item 1 should appear in at least one batch")
	assert.True(t, seen[2], "item 2 should appear in at least one batch")
	assert.True(t, seen[3], "item 3 should appear in at least one batch")
}

func TestWindowNilInput(t *testing.T) {
	ctx := context.Background()

	t.Run("Tumbling", func(t *testing.T) {
		out := Window[int](ctx, nil, 100*time.Millisecond, Tumbling)
		_, ok := <-out
		assert.False(t, ok, "output should be closed immediately for nil input")
	})

	t.Run("Sliding", func(t *testing.T) {
		out := Window[int](ctx, nil, 100*time.Millisecond, Sliding)
		_, ok := <-out
		assert.False(t, ok, "output should be closed immediately for nil input")
	})
}

func TestWindowContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	out := Window(ctx, in, 100*time.Millisecond, Tumbling)

	go func() {
		in <- 1
		in <- 2
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Drain output -- channel should close after cancel.
	for range out {
	}
	// Reaching here means the output channel closed successfully.
}

func TestWindowPanics(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	assert.Panics(t, func() { Window(ctx, in, 0, Tumbling) })
	assert.Panics(t, func() { Window(ctx, in, -1*time.Millisecond, Tumbling) })
	assert.Panics(t, func() { Window(ctx, in, 0, Sliding) })
	assert.Panics(t, func() { Window(ctx, in, -1*time.Millisecond, Sliding) })
}
