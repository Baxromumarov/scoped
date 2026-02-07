package chanx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSend(t *testing.T) {
	ch := make(chan int, 1)

	err := Send(context.Background(), ch, 42)
	assert.NoError(t, err)

	val := <-ch
	assert.Equal(t, 42, val)
}

func TestSend_ContextCanceled(t *testing.T) {
	ch := make(chan int) // unbuffered - will block

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Send(ctx, ch, 42)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestSend_ContextTimeout(t *testing.T) {
	ch := make(chan int) // unbuffered - will block

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := Send(ctx, ch, 42)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
}

func TestSend_NestedContext(t *testing.T) {
	ch := make(chan int) // unbuffered

	// Create nested contexts
	parent, parentCancel := context.WithCancel(context.Background())
	child, childCancel := context.WithTimeout(parent, 100*time.Millisecond)
	defer childCancel()

	// Cancel parent after 10ms
	go func() {
		time.Sleep(10 * time.Millisecond)
		parentCancel()
	}()

	start := time.Now()
	err := Send(child, ch, 42)
	elapsed := time.Since(start)

	// Should be canceled by parent, not child timeout
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Less(t, elapsed, 50*time.Millisecond)
}

func TestSend_Concurrent(t *testing.T) {
	ch := make(chan int, 100)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			err := Send(ctx, ch, val)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()
	close(ch)

	// Verify all values were sent
	count := 0
	for range ch {
		count++
	}
	assert.Equal(t, 100, count)
}

func TestTrySend(t *testing.T) {
	ch := make(chan int, 1)

	ok := TrySend(ch, 42)
	assert.True(t, ok)

	val := <-ch
	assert.Equal(t, 42, val)
}

func TestTrySend_Full(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 1 // fill the buffer

	ok := TrySend(ch, 42)
	assert.False(t, ok)
}

func TestTrySend_Unbuffered(t *testing.T) {
	ch := make(chan int) // unbuffered, no receiver

	ok := TrySend(ch, 42)
	assert.False(t, ok)
}

func TestTrySend_UnbufferedWithReceiver(t *testing.T) {
	ch := make(chan int)

	// Start a receiver
	go func() {
		<-ch
	}()

	// Give the receiver time to block on channel
	time.Sleep(10 * time.Millisecond)

	ok := TrySend(ch, 42)
	assert.True(t, ok)
}

func TestSendTimeout(t *testing.T) {
	ch := make(chan int, 1)

	err := SendTimeout(ch, 42, 100*time.Millisecond)
	assert.NoError(t, err)

	val := <-ch
	assert.Equal(t, 42, val)
}

func TestSendTimeout_Expires(t *testing.T) {
	ch := make(chan int) // unbuffered

	start := time.Now()
	err := SendTimeout(ch, 42, 20*time.Millisecond)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
}

func TestRecv(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42

	val, ok, err := Recv(context.Background(), ch)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 42, val)
}

func TestRecv_ChannelClosed(t *testing.T) {
	ch := make(chan int)
	close(ch)

	val, ok, err := Recv(context.Background(), ch)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 0, val)
}

func TestRecv_ContextCanceled(t *testing.T) {
	ch := make(chan int) // unbuffered, empty

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	val, ok, err := Recv(ctx, ch)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.False(t, ok)
	assert.Equal(t, 0, val)
}

func TestRecv_ContextTimeout(t *testing.T) {
	ch := make(chan int) // unbuffered, empty

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, ok, err := Recv(ctx, ch)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.False(t, ok)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
}

func TestRecv_NestedContext(t *testing.T) {
	ch := make(chan int)

	// Create nested contexts
	parent, parentCancel := context.WithCancel(context.Background())
	child, childCancel := context.WithTimeout(parent, 100*time.Millisecond)
	defer childCancel()

	// Cancel parent after 10ms
	go func() {
		time.Sleep(10 * time.Millisecond)
		parentCancel()
	}()

	start := time.Now()
	_, _, err := Recv(child, ch)
	elapsed := time.Since(start)

	// Should be canceled by parent, not child timeout
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Less(t, elapsed, 50*time.Millisecond)
}

func TestTryRecv(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42

	val, open, received := TryRecv(ch)
	assert.True(t, received)
	assert.True(t, open)
	assert.Equal(t, 42, val)
}

func TestTryRecv_Empty(t *testing.T) {
	ch := make(chan int, 1) // empty

	val, open, received := TryRecv(ch)
	assert.False(t, received)
	assert.True(t, open)
	assert.Equal(t, 0, val)
}

func TestTryRecv_Closed(t *testing.T) {
	ch := make(chan int)
	close(ch)

	val, open, received := TryRecv(ch)
	assert.False(t, received)
	assert.False(t, open)
	assert.Equal(t, 0, val)
}

func TestTryRecv_ClosedWithValue(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42
	close(ch)

	// First receive should get the value
	val, open, received := TryRecv(ch)
	assert.True(t, received)
	assert.True(t, open)
	assert.Equal(t, 42, val)

	// Second receive should detect closure
	val, open, received = TryRecv(ch)
	assert.False(t, received)
	assert.False(t, open)
	assert.Equal(t, 0, val)
}

func TestRecvTimeout(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42

	val, ok, err := RecvTimeout(ch, 100*time.Millisecond)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 42, val)
}

func TestRecvTimeout_Expires(t *testing.T) {
	ch := make(chan int) // empty

	start := time.Now()
	_, ok, err := RecvTimeout(ch, 20*time.Millisecond)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.False(t, ok)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
}

func TestSendRecv_RoundTrip(t *testing.T) {
	ch := make(chan string, 1)
	ctx := context.Background()

	err := Send(ctx, ch, "hello")
	assert.NoError(t, err)

	val, ok, err := Recv(ctx, ch)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "hello", val)
}

func TestSendRecv_ConcurrentProducerConsumer(t *testing.T) {
	ch := make(chan int, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const numItems = 1000
	var wg sync.WaitGroup

	// Producer
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(ch)
		for i := 0; i < numItems; i++ {
			err := Send(ctx, ch, i)
			if err != nil {
				t.Errorf("Send failed: %v", err)
				return
			}
		}
	}()

	// Consumer
	received := make([]int, 0, numItems)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			val, ok, err := Recv(ctx, ch)
			if err != nil {
				t.Errorf("Recv failed: %v", err)
				return
			}
			if !ok {
				return // channel closed
			}
			received = append(received, val)
		}
	}()

	wg.Wait()
	assert.Equal(t, numItems, len(received))
}

func TestNestedContext_DeepHierarchy(t *testing.T) {
	ch := make(chan int)

	// Create a deep context hierarchy
	ctx1 := context.Background()
	ctx2, cancel2 := context.WithCancel(ctx1)
	ctx3, cancel3 := context.WithTimeout(ctx2, 1*time.Second)
	ctx4, cancel4 := context.WithCancel(ctx3)
	defer cancel3()
	defer cancel4()

	// Cancel ctx2 (middle of hierarchy) after 10ms
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel2()
	}()

	start := time.Now()
	err := Send(ctx4, ch, 42)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func BenchmarkSend(b *testing.B) {
	ch := make(chan int, b.N)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Send(ctx, ch, i)
	}
}

func BenchmarkTrySend(b *testing.B) {
	ch := make(chan int, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		TrySend(ch, i)
	}
}

func BenchmarkRecv(b *testing.B) {
	ch := make(chan int, b.N)
	for i := 0; i < b.N; i++ {
		ch <- i
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Recv(ctx, ch)
	}
}

func BenchmarkTryRecv(b *testing.B) {
	ch := make(chan int, b.N)
	for i := 0; i < b.N; i++ {
		ch <- i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		TryRecv(ch)
	}
}
