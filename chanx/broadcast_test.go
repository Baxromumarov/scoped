package chanx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// broadcastCollect concurrently drains all broadcast outputs and returns
// the values received on each channel.
func broadcastCollect[T any](t *testing.T, outs []<-chan T) [][]T {
	t.Helper()
	received := make([][]T, len(outs))
	var wg sync.WaitGroup
	for i, out := range outs {
		wg.Go(func() {
			for val := range out {
				received[i] = append(received[i], val)
			}
		})
	}
	wg.Wait()
	return received
}

func TestBroadcastBasic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	outs := Broadcast(ctx, in, 3, 10)

	received := broadcastCollect(t, outs)

	expected := []int{1, 2, 3, 4, 5}
	for i := range 3 {
		assert.Equal(t, expected, received[i], "consumer %d", i)
	}
}

func TestBroadcastSlowConsumer(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	// bufSize=10 is large enough to absorb all 5 values even if one consumer
	// is delayed, so the fast consumers should still complete promptly.
	outs := Broadcast(ctx, in, 3, 10)

	var wg sync.WaitGroup
	received := make([][]int, 3)

	// Fast consumers
	for i := range 2 {
		i := i
		wg.Go(func() {
			for val := range outs[i] {
				received[i] = append(received[i], val)
			}
		})
	}

	// Slow consumer
	wg.Go(func() {
		for val := range outs[2] {
			received[2] = append(received[2], val)
			time.Sleep(50 * time.Millisecond)
		}
	})

	wg.Wait()

	expected := []int{1, 2, 3, 4, 5}
	for i := range 3 {
		assert.Equal(t, expected, received[i], "consumer %d", i)
	}
}

func TestBroadcastNilInput(t *testing.T) {
	ctx := context.Background()

	outs := Broadcast[int](ctx, nil, 3, 5)

	require.Len(t, outs, 3)
	for i, out := range outs {
		_, ok := <-out
		assert.False(t, ok, "consumer %d output should be closed immediately", i)
	}
}

func TestBroadcastContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	outs := Broadcast(ctx, in, 2, 5)

	// Send some values, then cancel.
	go func() {
		in <- 1
		in <- 2
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// All output channels should eventually close after cancel.
	for _, out := range outs {
		for range out {
		}
	}
	// If we reach here, all channels closed -- test passes.
}

func TestBroadcastPanics(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	t.Run("n<=0", func(t *testing.T) {
		assert.Panics(t, func() { Broadcast(ctx, in, 0, 5) })
		assert.Panics(t, func() { Broadcast(ctx, in, -1, 5) })
	})

	t.Run("bufSize<=0", func(t *testing.T) {
		assert.Panics(t, func() { Broadcast(ctx, in, 2, 0) })
		assert.Panics(t, func() { Broadcast(ctx, in, 2, -1) })
	})
}
