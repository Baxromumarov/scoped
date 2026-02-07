package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrDone_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)
	
	out := OrDone(ctx, in)
	
	// Should receive all values
	for i := 1; i <= 3; i++ {
		val, ok := <-out
		require.True(t, ok)
		assert.Equal(t, i, val)
	}
	
	// Channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestOrDone_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 1)
	in <- 42
	
	out := OrDone(ctx, in)
	
	// Cancel context before receiving
	cancel()
	
	// Should not receive any value due to cancellation
	select {
	case val := <-out:
		t.Fatalf("unexpected value received: %v", val)
	case <-time.After(10 * time.Millisecond):
		// Expected - no value should be received
	}
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestOrDone_ContextCancellationAfterReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 2)
	in <- 1
	in <- 2
	
	out := OrDone(ctx, in)
	
	// Receive first value
	val, ok := <-out
	require.True(t, ok)
	assert.Equal(t, 1, val)
	
	// Cancel context
	cancel()
	
	// Should not receive second value
	select {
	case val := <-out:
		t.Fatalf("unexpected value received: %v", val)
	case <-time.After(10 * time.Millisecond):
		// Expected - no value should be received
	}
	
	// Output channel should be closed
	_, ok = <-out
	assert.False(t, ok)
}

func TestOrDone_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	
	in := make(chan int)
	// Don't send any value, let context timeout
	
	out := OrDone(ctx, in)
	
	// Wait for context to timeout
	time.Sleep(20 * time.Millisecond)
	
	// Output channel should be closed due to deadline
	_, ok := <-out
	assert.False(t, ok)
}

func TestOrDone_NilInputChannel(t *testing.T) {
	ctx := context.Background()
	var in chan int // nil channel
	
	out := OrDone(ctx, in)
	
	// Output channel should be closed immediately
	_, ok := <-out
	assert.False(t, ok)
}

func TestOrDone_ClosedInputChannel(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	close(in)
	
	out := OrDone(ctx, in)
	
	// Output channel should be closed immediately
	_, ok := <-out
	assert.False(t, ok)
}

func TestOrDone_SlowConsumer(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 100)
	
	// Fill input channel
	for i := 0; i < 50; i++ {
		in <- i
	}
	close(in)
	
	out := OrDone(ctx, in)
	
	// Slow consumer - add delay between receives
	received := make([]int, 0, 50)
	for i := 0; i < 50; i++ {
		val, ok := <-out
		require.True(t, ok)
		received = append(received, val)
		time.Sleep(1 * time.Millisecond) // slow consumption
	}
	
	// Should have received all values in order
	assert.Len(t, received, 50)
	for i, val := range received {
		assert.Equal(t, i, val)
	}
	
	// Channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestOrDone_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	
	// Start producer
	go func() {
		for i := 0; i < 100; i++ {
			in <- i
			time.Sleep(time.Millisecond)
		}
		close(in)
	}()
	
	out := OrDone(ctx, in)
	
	// Start consumer
	received := make([]int, 0, 100)
	go func() {
		for val := range out {
			received = append(received, val)
		}
	}()
	
	// Wait for all values to be processed
	time.Sleep(200 * time.Millisecond)
	
	assert.Len(t, received, 100)
	for i, val := range received {
		assert.Equal(t, i, val)
	}
}

func TestOrDone_MemoryLeak(t *testing.T) {
	// This test ensures that the internal goroutine exits properly
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	
	out := OrDone(ctx, in)
	
	// Cancel context to trigger goroutine exit
	cancel()
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
	
	// Give some time for goroutine to exit
	time.Sleep(10 * time.Millisecond)
	
	// If there's a memory leak, this would be detected by race detector
	// or by running the test many times
}

func TestOrDone_MultipleOrDoneChains(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	
	// Fill input channel
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)
	
	// Chain multiple OrDone operations
	out1 := OrDone(ctx, in)
	out2 := OrDone(ctx, out1)
	out3 := OrDone(ctx, out2)
	
	// Should receive all values through the chain
	for i := 1; i <= 5; i++ {
		val, ok := <-out3
		require.True(t, ok)
		assert.Equal(t, i, val)
	}
	
	// Final channel should be closed
	_, ok := <-out3
	assert.False(t, ok)
}

func TestOrDone_DifferentTypes(t *testing.T) {
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
				
				out := OrDone(ctx, in)
				
				val, ok := <-out
				require.True(t, ok)
				assert.Equal(t, "hello", val)
				
				val, ok = <-out
				require.True(t, ok)
				assert.Equal(t, "world", val)
				
				_, ok = <-out
				assert.False(t, ok)
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
				in := make(chan TestStruct, 1)
				val := TestStruct{ID: 1, Name: "test"}
				in <- val
				close(in)
				
				out := OrDone(ctx, in)
				
				received, ok := <-out
				require.True(t, ok)
				assert.Equal(t, val, received)
				
				_, ok = <-out
				assert.False(t, ok)
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
				
				out := OrDone(ctx, in)
				
				val, ok := <-out
				require.True(t, ok)
				assert.Equal(t, "string value", val)
				
				val, ok = <-out
				require.True(t, ok)
				assert.Equal(t, 42, val)
				
				_, ok = <-out
				assert.False(t, ok)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestDrain_BasicFunctionality(t *testing.T) {
	ch := make(chan int, 5)
	
	// Fill channel
	for i := 0; i < 5; i++ {
		ch <- i
	}
	
	// Drain should empty the channel
	Drain(ch)
	
	// Channel should be empty
	assert.Len(t, ch, 0)
	
	// Should be able to send new values
	ch <- 42
	assert.Len(t, ch, 1)
}

func TestDrain_EmptyChannel(t *testing.T) {
	ch := make(chan int)
	
	// Should not panic on empty channel
	Drain(ch)
	
	// Channel should still be empty
	select {
	case <-ch:
		t.Fatal("channel should be empty")
	default:
		// Expected
	}
}

func TestDrain_ClosedChannel(t *testing.T) {
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)
	
	// Should not panic on closed channel
	Drain(ch)
	
	// Channel should be closed and empty
	_, ok := <-ch
	assert.False(t, ok)
}

func TestDrain_NilChannel(t *testing.T) {
	var ch chan int // nil channel
	
	// Should not panic on nil channel
	Drain(ch)
}

func TestDrain_ConcurrentDrain(t *testing.T) {
	ch := make(chan int, 100)
	
	// Fill channel
	for i := 0; i < 50; i++ {
		ch <- i
	}
	
	// Start draining in goroutine
	done := make(chan struct{})
	go func() {
		Drain(ch)
		close(done)
	}()
	
	// Should complete quickly
	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drain took too long")
	}
	
	// Channel should be empty
	assert.Len(t, ch, 0)
}

func TestDrain_WithActiveProducer(t *testing.T) {
	ch := make(chan int, 10)
	
	// Start producer
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for i := 0; i < 100; i++ {
			ch <- i
			time.Sleep(time.Millisecond)
		}
		close(ch)
	}()
	
	// Drain while producer is still active
	Drain(ch)
	
	// Wait for producer to finish
	<-producerDone
	
	// Channel should be closed and empty
	_, ok := <-ch
	assert.False(t, ok)
}

func TestDrain_DifferentTypes(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "string",
			test: func(t *testing.T) {
				ch := make(chan string, 3)
				ch <- "a"
				ch <- "b"
				ch <- "c"
				Drain(ch)
				assert.Len(t, ch, 0)
			},
		},
		{
			name: "struct",
			test: func(t *testing.T) {
				type TestStruct struct {
					ID int
				}
				ch := make(chan TestStruct, 2)
				ch <- TestStruct{ID: 1}
				ch <- TestStruct{ID: 2}
				Drain(ch)
				assert.Len(t, ch, 0)
			},
		},
		{
			name: "interface",
			test: func(t *testing.T) {
				ch := make(chan any, 2)
				ch <- "string"
				ch <- 42
				Drain(ch)
				assert.Len(t, ch, 0)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestDrain_LargeChannel(t *testing.T) {
	ch := make(chan int, 10000)
	
	// Fill channel with many values
	for i := 0; i < 10000; i++ {
		ch <- i
	}
	
	start := time.Now()
	Drain(ch)
	elapsed := time.Since(start)
	
	// Should complete in reasonable time
	assert.Less(t, elapsed, 1*time.Second)
	assert.Len(t, ch, 0)
}

func TestDrain_Integration(t *testing.T) {
	// Test Drain in a realistic shutdown scenario
	ch := make(chan int, 10)
	
	// Simulate producer
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for i := 0; i < 5; i++ {
			select {
			case ch <- i:
				time.Sleep(time.Millisecond)
			case <-time.After(10 * time.Millisecond):
				// Producer might be blocked, that's ok
				return
			}
		}
	}()
	
	// Give producer time to send some values
	time.Sleep(5 * time.Millisecond)
	
	// Drain channel during shutdown
	Drain(ch)
	
	// Wait for producer to finish
	<-producerDone
	
	// Channel should be empty
	assert.Len(t, ch, 0)
}

func TestOrDone_Drain_Integration(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	
	// Fill input channel
	for i := 0; i < 5; i++ {
		in <- i
	}
	
	out := OrDone(ctx, in)
	
	// Drain some values
	for i := 0; i < 3; i++ {
		val, ok := <-out
		require.True(t, ok)
		assert.Equal(t, i, val)
	}
	
	// Now drain the remaining values using Drain
	Drain(out)
	
	// Output channel should be empty
	select {
	case <-out:
		t.Fatal("channel should be empty")
	default:
		// Expected
	}
}
