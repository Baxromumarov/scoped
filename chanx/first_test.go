package chanx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirst_BasicFunctionality(t *testing.T) {
	ch1 := make(chan int, 1)
	ch2 := make(chan int, 1)
	ch3 := make(chan int, 1)

	ch2 <- 42

	out := First(context.Background(), ch1, ch2, ch3)

	val, ok := <-out
	require.True(t, ok)
	assert.Equal(t, 42, val)

	// Channel should close after delivering first value.
	_, ok = <-out
	assert.False(t, ok)
}

func TestFirst_SingleChannel(t *testing.T) {
	ch := make(chan string, 1)
	ch <- "hello"

	out := First(context.Background(), ch)

	val, ok := <-out
	require.True(t, ok)
	assert.Equal(t, "hello", val)
}

func TestFirst_NoChannels(t *testing.T) {
	out := First[int](context.Background())
	_, ok := <-out
	assert.False(t, ok)
}

func TestFirst_AllNilChannels(t *testing.T) {
	var ch1, ch2 chan int
	out := First(context.Background(), ch1, ch2)
	_, ok := <-out
	assert.False(t, ok)
}

func TestFirst_MixedNilChannels(t *testing.T) {
	var nilCh chan int
	validCh := make(chan int, 1)
	validCh <- 7

	out := First(context.Background(), nilCh, validCh, nilCh)

	val, ok := <-out
	require.True(t, ok)
	assert.Equal(t, 7, val)
}

func TestFirst_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan int) // no values, blocks forever

	out := First(ctx, ch)
	cancel()

	// Output should close with no value.
	_, ok := <-out
	assert.False(t, ok)
}

func TestFirst_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	ch := make(chan int)
	out := First(ctx, ch)

	_, ok := <-out
	assert.False(t, ok)
}

func TestFirst_ClosedChannelFirst(t *testing.T) {
	ch := make(chan int)
	close(ch) // closed channel returns zero value with ok=false

	out := First(context.Background(), ch)

	// Since the channel was closed (ok=false), First should not send a value.
	_, ok := <-out
	assert.False(t, ok)
}

func TestFirst_OnlyFirstValue(t *testing.T) {
	ch1 := make(chan int, 1)
	ch2 := make(chan int, 1)
	ch1 <- 1
	ch2 <- 2

	out := First(context.Background(), ch1, ch2)

	val, ok := <-out
	require.True(t, ok)
	// Should be 1 or 2 — whichever reflect.Select picks.
	assert.Contains(t, []int{1, 2}, val)

	// Only one value should come through.
	_, ok = <-out
	assert.False(t, ok)
}

func TestFirst_ConcurrentSenders(t *testing.T) {
	const n = 10
	chs := make([]<-chan int, n)
	for i := 0; i < n; i++ {
		ch := make(chan int, 1)
		ch <- i
		chs[i] = ch
	}

	out := First(context.Background(), chs...)

	val, ok := <-out
	require.True(t, ok)
	assert.GreaterOrEqual(t, val, 0)
	assert.Less(t, val, n)

	// Only one value.
	_, ok = <-out
	assert.False(t, ok)
}

func TestFirst_StreamingValues(t *testing.T) {
	ch := make(chan int)

	go func() {
		time.Sleep(10 * time.Millisecond)
		ch <- 99
	}()

	out := First(context.Background(), ch)

	var received atomic.Int32
	for range out {
		received.Add(1)
	}
	assert.Equal(t, int32(1), received.Load())
}
