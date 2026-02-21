package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuffer_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	for i := range 10 {
		in <- i
	}
	close(in)

	out := Buffer(ctx, in, 3, time.Second)

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	// 10 items / size 3 = [3, 3, 3, 1]
	require.Len(t, batches, 4)
	assert.Len(t, batches[0], 3)
	assert.Len(t, batches[1], 3)
	assert.Len(t, batches[2], 3)
	assert.Len(t, batches[3], 1) // partial flush on close
}

func TestBuffer_ExactBatchSize(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 6)
	for i := range 6 {
		in <- i
	}
	close(in)

	out := Buffer(ctx, in, 3, time.Second)

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	require.Len(t, batches, 2)
	assert.Equal(t, []int{0, 1, 2}, batches[0])
	assert.Equal(t, []int{3, 4, 5}, batches[1])
}

func TestBuffer_TimeoutFlush(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	out := Buffer(ctx, in, 100, 50*time.Millisecond)

	// Send 2 items, then wait for timeout to flush.
	go func() {
		in <- 1
		in <- 2
		time.Sleep(100 * time.Millisecond)
		close(in)
	}()

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	require.GreaterOrEqual(t, len(batches), 1)
	assert.Equal(t, []int{1, 2}, batches[0])
}

func TestBuffer_NilInput(t *testing.T) {
	out := Buffer[int](context.Background(), nil, 5, time.Second)
	_, ok := <-out
	assert.False(t, ok)
}

func TestBuffer_ClosedInput(t *testing.T) {
	in := make(chan int)
	close(in)

	out := Buffer(context.Background(), in, 5, time.Second)

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}
	assert.Empty(t, batches)
}

func TestBuffer_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	out := Buffer(ctx, in, 5, time.Second)
	cancel()

	for range out {
	}
}

func TestBuffer_PanicsOnZeroSize(t *testing.T) {
	assert.Panics(t, func() {
		Buffer[int](context.Background(), nil, 0, time.Second)
	})
}

func TestBuffer_PanicsOnZeroTimeout(t *testing.T) {
	assert.Panics(t, func() {
		Buffer[int](context.Background(), nil, 5, 0)
	})
}

func TestBuffer_SingleItem(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)

	out := Buffer(ctx, in, 1, time.Second)

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	require.Len(t, batches, 3)
	assert.Equal(t, []int{1}, batches[0])
	assert.Equal(t, []int{2}, batches[1])
	assert.Equal(t, []int{3}, batches[2])
}

func TestBuffer_PartialFlushOnClose(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 2)
	in <- 10
	in <- 20
	close(in)

	out := Buffer(ctx, in, 5, time.Second)

	var batches [][]int
	for batch := range out {
		batches = append(batches, batch)
	}

	require.Len(t, batches, 1)
	assert.Equal(t, []int{10, 20}, batches[0])
}
