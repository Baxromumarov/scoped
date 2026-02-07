package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendBatch_BasicFunctionality(t *testing.T) {
	ch := make(chan int, 5)
	err := SendBatch(context.Background(), ch, []int{1, 2, 3, 4, 5})
	require.NoError(t, err)
	close(ch)

	var got []int
	for v := range ch {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, got)
}

func TestSendBatch_EmptySlice(t *testing.T) {
	ch := make(chan int, 5)
	err := SendBatch(context.Background(), ch, []int{})
	require.NoError(t, err)
	assert.Len(t, ch, 0)
}

func TestSendBatch_NilSlice(t *testing.T) {
	ch := make(chan int, 5)
	err := SendBatch(context.Background(), ch, nil)
	require.NoError(t, err)
	assert.Len(t, ch, 0)
}

func TestSendBatch_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan int) // unbuffered — blocks on first send

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := SendBatch(ctx, ch, []int{1, 2, 3})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSendBatch_ContextCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan int) // unbuffered — send always blocks until context fires
	err := SendBatch(ctx, ch, []int{1, 2, 3})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRecvBatch_BasicFunctionality(t *testing.T) {
	ch := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		ch <- i
	}
	close(ch)

	got, err := RecvBatch(context.Background(), ch, 5)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, got)
}

func TestRecvBatch_ChannelClosedEarly(t *testing.T) {
	ch := make(chan int, 2)
	ch <- 10
	ch <- 20
	close(ch)

	got, err := RecvBatch(context.Background(), ch, 5)
	require.NoError(t, err)
	assert.Equal(t, []int{10, 20}, got)
}

func TestRecvBatch_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan int, 1)
	ch <- 1

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	got, err := RecvBatch(ctx, ch, 5)
	assert.ErrorIs(t, err, context.Canceled)
	// Should have received 1 value before cancel.
	assert.Equal(t, []int{1}, got)
}

func TestRecvBatch_PanicsOnZero(t *testing.T) {
	assert.Panics(t, func() {
		RecvBatch(context.Background(), make(chan int), 0)
	})
}

func TestRecvBatch_PanicsOnNegative(t *testing.T) {
	assert.Panics(t, func() {
		RecvBatch(context.Background(), make(chan int), -1)
	})
}

func TestRecvBatch_EmptyClosedChannel(t *testing.T) {
	ch := make(chan int)
	close(ch)

	got, err := RecvBatch(context.Background(), ch, 5)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSendRecvBatch_RoundTrip(t *testing.T) {
	ch := make(chan string, 10)
	values := []string{"alpha", "beta", "gamma"}

	err := SendBatch(context.Background(), ch, values)
	require.NoError(t, err)
	close(ch)

	got, err := RecvBatch(context.Background(), ch, 10)
	require.NoError(t, err)
	assert.Equal(t, values, got)
}

func TestRecvBatch_ExactN(t *testing.T) {
	ch := make(chan int, 10)
	for i := 0; i < 10; i++ {
		ch <- i
	}

	got, err := RecvBatch(context.Background(), ch, 3)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 1, 2}, got)
	// 7 values remain in the channel.
	assert.Len(t, ch, 7)
}
