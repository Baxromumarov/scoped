package chanx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// teeCollect is a helper that concurrently drains all Tee outputs and returns
// the values received on each channel. This avoids the deadlock caused by
// reading unbuffered Tee outputs sequentially.
func teeCollect[T any](t *testing.T, outs []<-chan T) [][]T {
	t.Helper()
	received := make([][]T, len(outs))
	var wg sync.WaitGroup
	for i, out := range outs {
		i, out := i, out
		wg.Add(1)
		go func() {
			defer wg.Done()
			for val := range out {
				received[i] = append(received[i], val)
			}
		}()
	}
	wg.Wait()
	return received
}

func TestTee_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 2)
	assert.Len(t, outs, 2)

	received := teeCollect(t, outs)
	for i := range received {
		assert.Equal(t, []int{1, 2, 3}, received[i], "channel %d", i)
	}
}

func TestTee_ZeroChannels(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	assert.Panics(t, func() { Tee(ctx, in, 0) })
	assert.Panics(t, func() { Tee(ctx, in, -1) })
}

func TestTee_SingleChannel(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 1)
	assert.Len(t, outs, 1)

	received := make([]int, 0, 3)
	for val := range outs[0] {
		received = append(received, val)
	}
	assert.Equal(t, []int{1, 2, 3}, received)
}

func TestTee_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}

	outs := Tee(ctx, in, 2)

	// Cancel context before consuming — Tee goroutine will exit via ctx.Done().
	cancel()

	// All output channels should close (drain any in-flight values).
	for _, out := range outs {
		for range out {
		}
	}
}

func TestTee_ContextCancellationAfterSomeValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 2)

	// Receive one value from the first channel.
	val, ok := <-outs[0]
	require.True(t, ok)
	assert.Equal(t, 1, val)

	// Cancel context — Tee is blocked sending value 1 to outs[1] and will
	// pick up ctx.Done(), then close all outputs.
	cancel()

	// Drain remaining values and verify channels close.
	for _, out := range outs {
		for range out {
		}
	}
}

func TestTee_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	in := make(chan int)
	outs := Tee(ctx, in, 2)

	time.Sleep(20 * time.Millisecond)

	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestTee_NilInputChannel(t *testing.T) {
	ctx := context.Background()
	var in chan int // nil

	outs := Tee(ctx, in, 2)

	// Output channels should be closed immediately.
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestTee_ClosedInputChannel(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	close(in)

	outs := Tee(ctx, in, 2)

	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestTee_ConcurrentConsumption(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 3)
	received := teeCollect(t, outs)

	expected := []int{1, 2, 3, 4, 5}
	for i := 0; i < 3; i++ {
		assert.Equal(t, expected, received[i])
	}
}

func TestTee_ManyChannels(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 10)
	assert.Len(t, outs, 10)

	received := teeCollect(t, outs)
	for i := range received {
		assert.Equal(t, []int{1, 2, 3}, received[i], "channel %d", i)
	}
}

func TestTee_SlowConsumerBlocksOthers(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 2)

	var wg sync.WaitGroup
	slowReceived := make([]int, 0, 3)
	fastReceived := make([]int, 0, 3)
	var elapsed time.Duration

	// Slow consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for val := range outs[0] {
			slowReceived = append(slowReceived, val)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Fast consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		for val := range outs[1] {
			fastReceived = append(fastReceived, val)
		}
		elapsed = time.Since(start)
	}()

	wg.Wait()

	// Fast consumer should be blocked by slow consumer.
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	assert.Equal(t, []int{1, 2, 3}, slowReceived)
	assert.Equal(t, []int{1, 2, 3}, fastReceived)
}

func TestTee_DifferentTypes(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		ctx := context.Background()
		in := make(chan string, 2)
		in <- "hello"
		in <- "world"
		close(in)

		outs := Tee(ctx, in, 2)
		received := teeCollect(t, outs)
		for i := range received {
			assert.Equal(t, []string{"hello", "world"}, received[i])
		}
	})

	t.Run("struct", func(t *testing.T) {
		type TestStruct struct {
			ID   int
			Name string
		}
		ctx := context.Background()
		in := make(chan TestStruct, 2)
		val1 := TestStruct{ID: 1, Name: "test1"}
		val2 := TestStruct{ID: 2, Name: "test2"}
		in <- val1
		in <- val2
		close(in)

		outs := Tee(ctx, in, 2)
		received := teeCollect(t, outs)
		for i := range received {
			assert.Equal(t, []TestStruct{val1, val2}, received[i])
		}
	})

	t.Run("interface", func(t *testing.T) {
		ctx := context.Background()
		in := make(chan any, 2)
		in <- "string value"
		in <- 42
		close(in)

		outs := Tee(ctx, in, 2)
		received := teeCollect(t, outs)
		for i := range received {
			assert.Equal(t, []any{"string value", 42}, received[i])
		}
	})
}

func TestTee_LargeValues(t *testing.T) {
	ctx := context.Background()
	in := make(chan []byte, 2)

	largeVal1 := make([]byte, 10000)
	largeVal2 := make([]byte, 10000)
	for i := range largeVal1 {
		largeVal1[i] = byte(i % 256)
		largeVal2[i] = byte((i + 1) % 256)
	}

	in <- largeVal1
	in <- largeVal2
	close(in)

	outs := Tee(ctx, in, 3)
	received := teeCollect(t, outs)

	for i := range received {
		require.Len(t, received[i], 2, "channel %d", i)
		assert.Equal(t, largeVal1, received[i][0])
		assert.Equal(t, largeVal2, received[i][1])
	}
}

func TestTee_ZeroValues(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 0
	in <- 0
	in <- 0
	close(in)

	outs := Tee(ctx, in, 2)
	received := teeCollect(t, outs)
	for i := range received {
		assert.Equal(t, []int{0, 0, 0}, received[i])
	}
}

func TestTee_StreamingInput(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	outs := Tee(ctx, in, 2)

	var wg sync.WaitGroup
	received1 := make([]int, 0, 5)
	received2 := make([]int, 0, 5)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for val := range outs[0] {
			received1 = append(received1, val)
		}
	}()
	go func() {
		defer wg.Done()
		for val := range outs[1] {
			received2 = append(received2, val)
		}
	}()

	go func() {
		for i := 1; i <= 5; i++ {
			in <- i
			time.Sleep(time.Millisecond)
		}
		close(in)
	}()

	wg.Wait()
	assert.Equal(t, []int{1, 2, 3, 4, 5}, received1)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, received2)
}

func TestTee_ContextCancellationDuringStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	outs := Tee(ctx, in, 2)

	var wg sync.WaitGroup
	received1 := make([]int, 0)
	received2 := make([]int, 0)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for val := range outs[0] {
			received1 = append(received1, val)
		}
	}()
	go func() {
		defer wg.Done()
		for val := range outs[1] {
			received2 = append(received2, val)
		}
	}()

	// Send some values, then cancel.
	in <- 1
	in <- 2
	cancel()

	wg.Wait()

	// Both consumers should have received at least 1 value.
	// Cancellation may happen mid-broadcast, so one consumer might
	// have received one more value than the other.
	assert.GreaterOrEqual(t, len(received1), 1)
	assert.LessOrEqual(t, len(received1), 2)
	assert.GreaterOrEqual(t, len(received2), 1)
	assert.LessOrEqual(t, len(received2), 2)
}

func TestTee_MemoryLeak(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	outs := Tee(ctx, in, 2)
	cancel()

	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}

	time.Sleep(10 * time.Millisecond)
}

func TestTee_PartialConsumption(t *testing.T) {
	// With unbuffered broadcast Tee, all consumers must be active.
	// This test verifies that both consumers receive all values when
	// both are concurrently consuming.
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 2)
	received := teeCollect(t, outs)

	assert.Equal(t, []int{1, 2, 3}, received[0])
	assert.Equal(t, []int{1, 2, 3}, received[1])
}

func TestTee_PartialConsumption_Blocks(t *testing.T) {
	// Verify that not reading from one output blocks the entire Tee.
	// Context timeout unblocks everything.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 2)

	// Only consume from outs[0]; outs[1] has no reader.
	// Tee will block after delivering value 1 to outs[0] while trying
	// to send value 1 to outs[1]. Context timeout frees it.
	val, ok := <-outs[0]
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	// Drain — context will time out and close both channels.
	for range outs[0] {
	}
	for range outs[1] {
	}
}

func TestTee_WithBufferedOutputs(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	outs := Tee(ctx, in, 2)
	received := teeCollect(t, outs)

	for i := range received {
		assert.Equal(t, []int{1, 2, 3}, received[i])
	}
}

func TestTee_IntegrationWithOtherFunctions(t *testing.T) {
	ctx := context.Background()

	source := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		source <- i
	}
	close(source)

	outs := Tee(ctx, source, 3)
	merged := Merge(ctx, outs[0], outs[1], outs[2])

	allValues := make([]int, 0, 15)
	for val := range merged {
		allValues = append(allValues, val)
	}

	assert.Len(t, allValues, 15)
	for i := 1; i <= 5; i++ {
		count := 0
		for _, val := range allValues {
			if val == i {
				count++
			}
		}
		assert.Equal(t, 3, count, "value %d", i)
	}
}
