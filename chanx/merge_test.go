package chanx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerge_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	ch1 := make(chan int, 2)
	ch2 := make(chan int, 2)
	
	ch1 <- 1
	ch1 <- 2
	ch2 <- 3
	ch2 <- 4
	close(ch1)
	close(ch2)
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Should receive all values (order not guaranteed)
	received := make([]int, 0, 4)
	for i := 0; i < 4; i++ {
		val, ok := <-out
		require.True(t, ok)
		received = append(received, val)
	}
	
	// Should contain all values
	assert.Contains(t, received, 1)
	assert.Contains(t, received, 2)
	assert.Contains(t, received, 3)
	assert.Contains(t, received, 4)
	assert.Len(t, received, 4)
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_NoChannels(t *testing.T) {
	ctx := context.Background()
	out := Merge[int](ctx)
	
	// Should be closed immediately
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_SingleChannel(t *testing.T) {
	ctx := context.Background()
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	close(ch)
	
	out := Merge[int](ctx, ch)
	
	// Should receive all values in order
	val, ok := <-out
	require.True(t, ok)
	assert.Equal(t, 1, val)
	
	val, ok = <-out
	require.True(t, ok)
	assert.Equal(t, 2, val)
	
	// Output channel should be closed
	_, ok = <-out
	assert.False(t, ok)
}

func TestMerge_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch1 := make(chan int)
	ch2 := make(chan int)
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Cancel context before any values
	cancel()
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_ContextCancellationAfterValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch1 := make(chan int, 2)
	ch2 := make(chan int, 2)
	
	ch1 <- 1
	ch2 <- 2
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Receive first value
	val, ok := <-out
	require.True(t, ok)
	assert.Contains(t, []int{1, 2}, val)
	
	// Cancel context
	cancel()
	
	// Output channel should be closed
	_, ok = <-out
	assert.False(t, ok)
}

func TestMerge_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	
	ch1 := make(chan int)
	ch2 := make(chan int)
	// Don't send any values, let context timeout
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Wait for context to timeout
	time.Sleep(20 * time.Millisecond)
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_NilChannels(t *testing.T) {
	ctx := context.Background()
	var ch1, ch2 chan int
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Should be closed immediately since nil channels never send
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_MixedNilAndValidChannels(t *testing.T) {
	ctx := context.Background()
	var ch1 chan int // nil
	ch2 := make(chan int, 1)
	ch2 <- 42
	close(ch2)
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Should receive value from valid channel
	val, ok := <-out
	require.True(t, ok)
	assert.Equal(t, 42, val)
	
	// Output channel should be closed
	_, ok = <-out
	assert.False(t, ok)
}

func TestMerge_ClosedChannels(t *testing.T) {
	ctx := context.Background()
	ch1 := make(chan int)
	ch2 := make(chan int)
	close(ch1)
	close(ch2)
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Should be closed immediately
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_ConcurrentProduction(t *testing.T) {
	ctx := context.Background()
	ch1 := make(chan int, 10)
	ch2 := make(chan int, 10)
	
	// Start producers
	go func() {
		for i := 0; i < 10; i++ {
			ch1 <- i
			time.Sleep(time.Millisecond)
		}
		close(ch1)
	}()
	
	go func() {
		for i := 10; i < 20; i++ {
			ch2 <- i
			time.Sleep(time.Millisecond)
		}
		close(ch2)
	}()
	
	out := Merge[int](ctx, ch1, ch2)
	
	// Should receive all values
	received := make([]int, 0, 20)
	for i := 0; i < 20; i++ {
		val, ok := <-out
		require.True(t, ok)
		received = append(received, val)
	}
	
	// Should contain all values from 0-19
	for i := 0; i < 20; i++ {
		assert.Contains(t, received, i)
	}
	assert.Len(t, received, 20)
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_ManyChannels(t *testing.T) {
	ctx := context.Background()
	numChannels := 10
	channels := make([]chan int, numChannels)
	
	// Create and fill channels
	for i := 0; i < numChannels; i++ {
		channels[i] = make(chan int, 1)
		channels[i] <- i
		close(channels[i])
	}
	
	// Convert to <-chan type
	readOnlyChannels := make([]<-chan int, numChannels)
	for i, ch := range channels {
		readOnlyChannels[i] = ch
	}
	
	out := Merge[int](ctx, readOnlyChannels...)
	
	// Should receive all values
	received := make([]int, 0, numChannels)
	for i := 0; i < numChannels; i++ {
		val, ok := <-out
		require.True(t, ok)
		received = append(received, val)
	}
	
	// Should contain all values
	for i := 0; i < numChannels; i++ {
		assert.Contains(t, received, i)
	}
	assert.Len(t, received, numChannels)
	
	// Output channel should be closed
	_, ok := <-out
	assert.False(t, ok)
}

func TestMerge_DifferentTypes(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "string",
			test: func(t *testing.T) {
				ctx := context.Background()
				ch1 := make(chan string, 1)
				ch2 := make(chan string, 1)
				ch1 <- "hello"
				ch2 <- "world"
				close(ch1)
				close(ch2)
				
				out := Merge[string](ctx, ch1, ch2)
				
				received := make([]string, 0, 2)
				for i := 0; i < 2; i++ {
					val, ok := <-out
					require.True(t, ok)
					received = append(received, val)
				}
				
				assert.Contains(t, received, "hello")
				assert.Contains(t, received, "world")
				assert.Len(t, received, 2)
				
				_, ok := <-out
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
				ch1 := make(chan TestStruct, 1)
				ch2 := make(chan TestStruct, 1)
				val1 := TestStruct{ID: 1, Name: "test1"}
				val2 := TestStruct{ID: 2, Name: "test2"}
				ch1 <- val1
				ch2 <- val2
				close(ch1)
				close(ch2)
				
				out := Merge[TestStruct](ctx, ch1, ch2)
				
				received := make([]TestStruct, 0, 2)
				for i := 0; i < 2; i++ {
					val, ok := <-out
					require.True(t, ok)
					received = append(received, val)
				}
				
				assert.Contains(t, received, val1)
				assert.Contains(t, received, val2)
				assert.Len(t, received, 2)
				
				_, ok := <-out
				assert.False(t, ok)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestFanOut_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	
	// Fill input channel
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)
	
	outs := FanOut(ctx, in, 3)
	
	// Should have 3 output channels
	assert.Len(t, outs, 3)
	
	// Each channel should receive all values in round-robin order
	expected := [][]int{
		{1, 4}, // Channel 0 gets values 1, 4
		{2, 5}, // Channel 1 gets values 2, 5
		{3},    // Channel 2 gets value 3
	}
	
	for i, out := range outs {
		received := make([]int, 0)
		for val := range out {
			received = append(received, val)
		}
		assert.Equal(t, expected[i], received)
	}
}

func TestFanOut_ZeroChannels(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	
	// Should panic with n <= 0
	assert.Panics(t, func() {
		FanOut(ctx, in, 0)
	})
	
	assert.Panics(t, func() {
		FanOut(ctx, in, -1)
	})
}

func TestFanOut_SingleChannel(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	
	// Fill input channel
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	
	outs := FanOut(ctx, in, 1)
	
	// Should have 1 output channel
	assert.Len(t, outs, 1)
	
	// Should receive all values
	received := make([]int, 0)
	for val := range outs[0] {
		received = append(received, val)
	}
	assert.Equal(t, []int{1, 2, 3}, received)
}

func TestFanOut_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 5)
	
	// Fill input channel
	for i := 1; i <= 5; i++ {
		in <- i
	}
	
	outs := FanOut(ctx, in, 3)
	
	// Cancel context before consuming all values
	cancel()
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestFanOut_ContextCancellationAfterSomeValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int, 5)
	
	// Fill input channel
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)
	
	outs := FanOut(ctx, in, 3)
	
	// Receive some values
	for i := 0; i < 2; i++ {
		_, ok := <-outs[0]
		require.True(t, ok)
	}
	
	// Cancel context
	cancel()
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestFanOut_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	
	in := make(chan int)
	// Don't send any values, let context timeout
	
	outs := FanOut(ctx, in, 2)
	
	// Wait for context to timeout
	time.Sleep(20 * time.Millisecond)
	
	// All output channels should be closed
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestFanOut_NilInputChannel(t *testing.T) {
	ctx := context.Background()
	var in chan int // nil channel
	
	outs := FanOut(ctx, in, 2)
	
	// Output channels should be closed immediately since nil channel never sends
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestFanOut_ClosedInputChannel(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	close(in)
	
	outs := FanOut(ctx, in, 2)
	
	// Output channels should be closed immediately
	for _, out := range outs {
		_, ok := <-out
		assert.False(t, ok)
	}
}

func TestFanOut_ConcurrentConsumption(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	
	// Fill input channel
	for i := 1; i <= 10; i++ {
		in <- i
	}
	close(in)
	
	outs := FanOut(ctx, in, 3)
	
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
	
	// Check round-robin distribution
	expected := [][]int{
		{1, 4, 7, 10}, // Channel 0 gets values 1, 4, 7, 10
		{2, 5, 8},     // Channel 1 gets values 2, 5, 8
		{3, 6, 9},     // Channel 2 gets values 3, 6, 9
	}
	
	for i, vals := range received {
		assert.Equal(t, expected[i], vals)
	}
}

func TestFanOut_ManyChannels(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	
	// Fill input channel
	for i := 1; i <= 10; i++ {
		in <- i
	}
	close(in)
	
	outs := FanOut(ctx, in, 10)
	
	// Should have 10 output channels
	assert.Len(t, outs, 10)
	
	// Each channel should get exactly one value
	for i, out := range outs {
		val, ok := <-out
		require.True(t, ok)
		assert.Equal(t, i+1, val)
		
		// Channel should be closed after one value
		_, ok = <-out
		assert.False(t, ok)
	}
}

func TestFanOut_DifferentTypes(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "string",
			test: func(t *testing.T) {
				ctx := context.Background()
				in := make(chan string, 3)
				in <- "a"
				in <- "b"
				in <- "c"
				close(in)
				
				outs := FanOut(ctx, in, 2)
				
				// Channel 0 gets "a", "c"
				val, ok := <-outs[0]
				require.True(t, ok)
				assert.Equal(t, "a", val)
				
				val, ok = <-outs[0]
				require.True(t, ok)
				assert.Equal(t, "c", val)
				
				_, ok = <-outs[0]
				assert.False(t, ok)
				
				// Channel 1 gets "b"
				val, ok = <-outs[1]
				require.True(t, ok)
				assert.Equal(t, "b", val)
				
				_, ok = <-outs[1]
				assert.False(t, ok)
			},
		},
		{
			name: "struct",
			test: func(t *testing.T) {
				type TestStruct struct {
					ID int
				}
				ctx := context.Background()
				in := make(chan TestStruct, 3)
				in <- TestStruct{ID: 1}
				in <- TestStruct{ID: 2}
				in <- TestStruct{ID: 3}
				close(in)
				
				outs := FanOut(ctx, in, 2)
				
				// Channel 0 gets ID=1, ID=3
				val, ok := <-outs[0]
				require.True(t, ok)
				assert.Equal(t, TestStruct{ID: 1}, val)
				
				val, ok = <-outs[0]
				require.True(t, ok)
				assert.Equal(t, TestStruct{ID: 3}, val)
				
				_, ok = <-outs[0]
				assert.False(t, ok)
				
				// Channel 1 gets ID=2
				val, ok = <-outs[1]
				require.True(t, ok)
				assert.Equal(t, TestStruct{ID: 2}, val)
				
				_, ok = <-outs[1]
				assert.False(t, ok)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestFanOut_SlowConsumer(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	
	// Fill input channel
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)
	
	outs := FanOut(ctx, in, 2)
	
	// Consume from first channel slowly
	received := make([]int, 0)
	go func() {
		for val := range outs[0] {
			received = append(received, val)
			time.Sleep(10 * time.Millisecond) // slow consumption
		}
	}()
	
	// Consume from second channel normally
	received2 := make([]int, 0)
	for val := range outs[1] {
		received2 = append(received2, val)
	}
	
	// Wait for slow consumer
	time.Sleep(100 * time.Millisecond)
	
	// Should have received values in round-robin order
	assert.Equal(t, []int{1, 3, 5}, received)
	assert.Equal(t, []int{2, 4}, received2)
}

func TestMerge_FanOut_Integration(t *testing.T) {
	ctx := context.Background()
	
	// Create multiple input channels
	ch1 := make(chan int, 3)
	ch2 := make(chan int, 3)
	ch3 := make(chan int, 3)
	
	// Fill input channels
	for i := 0; i < 3; i++ {
		ch1 <- i
		ch2 <- i + 10
		ch3 <- i + 20
	}
	close(ch1)
	close(ch2)
	close(ch3)
	
	// Merge all channels
	merged := Merge[int](ctx, ch1, ch2, ch3)
	
	// Fan out to multiple consumers
	outs := FanOut(ctx, merged, 3)
	
	// Collect all values from all output channels
	allValues := make([]int, 0)
	for _, out := range outs {
		for val := range out {
			allValues = append(allValues, val)
		}
	}
	
	// Should contain all values from 0-2, 10-12, 20-22
	for i := 0; i < 3; i++ {
		assert.Contains(t, allValues, i)
		assert.Contains(t, allValues, i+10)
		assert.Contains(t, allValues, i+20)
	}
	assert.Len(t, allValues, 9)
}
