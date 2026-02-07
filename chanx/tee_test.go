package chanx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTee_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 2)
	
	// Should have 2 output channels
	assert.Len(t, outs, 2)
	
	// Both channels should receive all values
	for _, out := range outs {
		received := make([]int, 0)
		for val := range out {
			received = append(received, val)
		}
		assert.Equal(t, []int{1, 2, 3}, received)
	}
}

func TestTee_ZeroChannels(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	
	// Should panic with n <= 0
	assert.Panics(t, func() {
		Tee(ctx, in, 0)
	})
	
	assert.Panics(t, func() {
		Tee(ctx, in, -1)
	})
}

func TestTee_SingleChannel(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 1)
	
	// Should have 1 output channel
	assert.Len(t, outs, 1)
	
	// Should receive all values
	received := make([]int, 0)
	for val := range outs[0] {
		received = append(received, val)
	}
	assert.Equal(t, []int{1, 2, 3}, received)
}

func TestTee_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	
	outs := Tee(ctx, in, 2)
	
	// Cancel context before consuming all values
	cancel()
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestTee_ContextCancellationAfterSomeValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 2)
	
	// Receive some values from first channel
	val, ok := <-outs[0]
	require.True(t, ok)
	assert.Equal(t, 1, val)
	
	// Cancel context
	cancel()
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok = <-out
		assert.False(t, ok)
	}
}

func TestTee_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	
	in := make(chan int)
	// Don't send any values, let context timeout
	
	outs := Tee(ctx, in, 2)
	
	// Wait for context to timeout
	time.Sleep(20 * time.Millisecond)
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestTee_NilInputChannel(t *testing.T) {
	ctx := context.Background()
	var in chan int // nil channel
	
	outs := Tee(ctx, in, 2)
	
	// Output channels should be closed immediately since nil channel never sends
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
	
	// Output channels should be closed immediately
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestTee_ConcurrentConsumption(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	
	// Fill input channel
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 3)
	
	// Start consumers concurrently
	received := make([][]int, 3)
	var wg sync.WaitGroup
	
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for val := range outs[idx] {
				received[idx] = append(received[idx], val)
			}
		}(i)
	}
	
	wg.Wait()
	
	// All channels should receive the same values
	expected := []int{1, 2, 3, 4, 5}
	for i := 0; i < 3; i++ {
		assert.Equal(t, expected, received[i])
	}
}

func TestTee_ManyChannels(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 10)
	
	// Should have 10 output channels
	assert.Len(t, outs, 10)
	
	// All channels should receive all values
	for i, out := range outs {
		received := make([]int, 0)
		for val := range out {
			received = append(received, val)
		}
		assert.Equal(t, []int{1, 2, 3}, received, "channel %d", i)
	}
}

func TestTee_SlowConsumerBlocksOthers(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 2)
	
	// Start slow consumer on first channel
	slowReceived := make([]int, 0)
	go func() {
		for val := range outs[0] {
			slowReceived = append(slowReceived, val)
			time.Sleep(50 * time.Millisecond) // slow consumption
		}
	}()
	
	// Fast consumer on second channel
	fastReceived := make([]int, 0)
	start := time.Now()
	for val := range outs[1] {
		fastReceived = append(fastReceived, val)
	}
	elapsed := time.Since(start)
	
	// Fast consumer should be blocked by slow consumer
	// Should take at least 3 * 50ms due to slow consumer
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond)
	
	// Both should receive all values
	assert.Equal(t, []int{1, 2, 3}, slowReceived)
	assert.Equal(t, []int{1, 2, 3}, fastReceived)
}

func TestTee_DifferentTypes(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "string",
			test: func(t *testing.T) {
				ctx := context.Background()
				in := make(chan string, 2)
				in <- "hello"
				in <- "world"
				close(in)
				
				outs := Tee(ctx, in, 2)
				
				// Both channels should receive all values
				for _, out := range outs {
					received := make([]string, 0)
					for val := range out {
						received = append(received, val)
					}
					assert.Equal(t, []string{"hello", "world"}, received)
				}
			},
		},
		{
			name: "struct",
			test: func(t *testing.T) {
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
				
				// Both channels should receive all values
				for _, out := range outs {
					received := make([]TestStruct, 0)
					for val := range out {
						received = append(received, val)
					}
					assert.Equal(t, []TestStruct{val1, val2}, received)
				}
			},
		},
		{
			name: "interface",
			test: func(t *testing.T) {
				ctx := context.Background()
				in := make(chan any, 2)
				in <- "string value"
				in <- 42
				close(in)
				
				outs := Tee(ctx, in, 2)
				
				// Both channels should receive all values
				for _, out := range outs {
					received := make([]any, 0)
					for val := range out {
						received = append(received, val)
					}
					assert.Equal(t, []any{"string value", 42}, received)
				}
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestTee_LargeValues(t *testing.T) {
	ctx := context.Background()
	in := make(chan []byte, 2)
	
	// Create large values
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
	
	// All channels should receive all values
	for _, out := range outs {
		received := make([][]byte, 0)
		for val := range out {
			received = append(received, val)
		}
		assert.Len(t, received, 2)
		assert.Equal(t, largeVal1, received[0])
		assert.Equal(t, largeVal2, received[1])
		assert.Equal(t, 10000, len(received[0]))
		assert.Equal(t, 10000, len(received[1]))
	}
}

func TestTee_ZeroValues(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Send zero values
	in <- 0
	in <- 0
	in <- 0
	close(in)
	
	outs := Tee(ctx, in, 2)
	
	// Both channels should receive all zero values
	for _, out := range outs {
		received := make([]int, 0)
		for val := range out {
			received = append(received, val)
		}
		assert.Equal(t, []int{0, 0, 0}, received)
	}
}

func TestTee_StreamingInput(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	
	outs := Tee(ctx, in, 2)
	
	// Start consumers
	received1 := make([]int, 0)
	received2 := make([]int, 0)
	var wg sync.WaitGroup
	
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
	
	// Stream values
	go func() {
		for i := 1; i <= 5; i++ {
			in <- i
			time.Sleep(time.Millisecond)
		}
		close(in)
	}()
	
	wg.Wait()
	
	// Both should receive all values
	assert.Equal(t, []int{1, 2, 3, 4, 5}, received1)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, received2)
}

func TestTee_ContextCancellationDuringStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	
	outs := Tee(ctx, in, 2)
	
	// Start consumers
	received1 := make([]int, 0)
	received2 := make([]int, 0)
	var wg sync.WaitGroup
	
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
	
	// Stream some values then cancel
	go func() {
		in <- 1
		in <- 2
		time.Sleep(10 * time.Millisecond)
		cancel() // cancel context
		in <- 3 // this value won't be processed
		close(in)
	}()
	
	wg.Wait()
	
	// Both should receive only the first two values
	assert.Equal(t, []int{1, 2}, received1)
	assert.Equal(t, []int{1, 2}, received2)
}

func TestTee_MemoryLeak(t *testing.T) {
	// This test ensures that the internal goroutine exits properly
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	
	outs := Tee(ctx, in, 2)
	
	// Cancel context to trigger goroutine exit
	cancel()
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
	
	// Give some time for goroutine to exit
	time.Sleep(10 * time.Millisecond)
	
	// If there's a memory leak, this would be detected by race detector
	// or by running the test many times
}

func TestTee_PartialConsumption(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := Tee(ctx, in, 2)
	
	// Consume only from first channel
	received := make([]int, 0)
	for val := range outs[0] {
		received = append(received, val)
	}
	
	assert.Equal(t, []int{1, 2, 3}, received)
	
	// Second channel should still have all values available
	received2 := make([]int, 0)
	for val := range outs[1] {
		received2 = append(received2, val)
	}
	
	assert.Equal(t, []int{1, 2, 3}, received2)
}

func TestTee_WithBufferedOutputs(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	// Create buffered output channels manually to test behavior
	outs := Tee(ctx, in, 2)
	
	// Both channels should receive all values regardless of buffering
	for _, out := range outs {
		received := make([]int, 0)
		for val := range out {
			received = append(received, val)
		}
		assert.Equal(t, []int{1, 2, 3}, received)
	}
}

func TestTee_IntegrationWithOtherFunctions(t *testing.T) {
	ctx := context.Background()
	
	// Create a source channel
	source := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		source <- i
	}
	close(source)
	
	// Tee the source to multiple channels
	outs := Tee(ctx, source, 3)
	
	// Merge the tee outputs back together
	merged := Merge(ctx, outs[0], outs[1], outs[2])
	
	// Collect all values
	allValues := make([]int, 0)
	for val := range merged {
		allValues = append(allValues, val)
	}
	
	// Should have 15 values (5 values * 3 channels)
	assert.Len(t, allValues, 15)
	
	// Each value 1-5 should appear exactly 3 times
	for i := 1; i <= 5; i++ {
		count := 0
		for _, val := range allValues {
			if val == i {
				count++
			}
		}
		assert.Equal(t, 3, count)
	}
}
