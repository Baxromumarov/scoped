package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferWithReasonSize(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	for i := 1; i <= 10; i++ {
		in <- i
	}
	close(in)

	out := BufferWithReason(ctx, in, 5, time.Second)

	var results []BatchResult[int]
	for batch := range out {
		results = append(results, batch)
	}

	require.Len(t, results, 2)
	assert.Equal(t, FlushSize, results[0].Reason)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, results[0].Items)
	assert.Equal(t, FlushSize, results[1].Reason)
	assert.Equal(t, []int{6, 7, 8, 9, 10}, results[1].Items)
}

func TestBufferWithReasonTimeout(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	out := BufferWithReason(ctx, in, 100, 80*time.Millisecond)

	go func() {
		in <- 1
		in <- 2
		// Don't send enough to fill the batch; let timeout fire.
		time.Sleep(200 * time.Millisecond)
		close(in)
	}()

	var results []BatchResult[int]
	for batch := range out {
		results = append(results, batch)
	}

	require.NotEmpty(t, results)
	assert.Equal(t, FlushTimeout, results[0].Reason)
	assert.Equal(t, []int{1, 2}, results[0].Items)
}

func TestBufferWithReasonClose(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 10
	in <- 20
	in <- 30
	close(in)

	// Size is larger than items, so flush on close.
	out := BufferWithReason(ctx, in, 100, time.Second)

	var results []BatchResult[int]
	for batch := range out {
		results = append(results, batch)
	}

	require.Len(t, results, 1)
	assert.Equal(t, FlushClose, results[0].Reason)
	assert.Equal(t, []int{10, 20, 30}, results[0].Items)
}

func TestBufferWithReasonNilInput(t *testing.T) {
	ctx := context.Background()
	out := BufferWithReason[int](ctx, nil, 5, time.Second)

	_, ok := <-out
	assert.False(t, ok, "output should be closed immediately for nil input")
}

func TestBufferWithReasonPanics(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	t.Run("size<=0", func(t *testing.T) {
		assert.Panics(t, func() { BufferWithReason(ctx, in, 0, time.Second) })
		assert.Panics(t, func() { BufferWithReason(ctx, in, -1, time.Second) })
	})
	t.Run("timeout<=0", func(t *testing.T) {
		assert.Panics(t, func() { BufferWithReason(ctx, in, 5, 0) })
		assert.Panics(t, func() { BufferWithReason(ctx, in, 5, -time.Second) })
	})
}
