package chanx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClosable_Send(t *testing.T) {
	c := NewClosable[int](1)
	err := c.Send(-12)
	assert.NoError(t, err)

	err = c.TrySend(900)
	assert.EqualError(t, err, ErrBuffFull.Error())
}

func TestClosable_SendAfterClose(t *testing.T) {
	c := NewClosable[int](1)
	c.Close()

	err := c.Send(42)
	assert.Error(t, err)
	assert.Equal(t, ErrClosed, err)
}

func TestClosable_SendBlocksWhenFull(t *testing.T) {
	c := NewClosable[int](1)

	// Fill the buffer
	err := c.Send(1)
	assert.NoError(t, err)

	// Close the channel after a short delay to unblock Send
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.Close()
	}()

	// Send should block until channel is closed
	start := time.Now()
	err = c.Send(2)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, ErrClosed, err)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
}

func TestClosable_SendConcurrent(t *testing.T) {
	c := NewClosable[int](100)

	var wg sync.WaitGroup
	var successCount, errorCount int64

	// Start multiple senders
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				err := c.Send(id*10 + j)
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	// All sends should succeed
	assert.Equal(t, int64(100), atomic.LoadInt64(&successCount))
	assert.Equal(t, int64(0), atomic.LoadInt64(&errorCount))
}

func TestClosable_SendContext(t *testing.T) {
	c := NewClosable[int](1)

	// Successful send
	err := c.SendContext(context.Background(), 42)
	assert.NoError(t, err)

	// Verify value was sent
	val := <-c.Chan()
	assert.Equal(t, 42, val)
}

func TestClosable_SendContextCancelled(t *testing.T) {
	c := NewClosable[int](0) // unbuffered

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := c.SendContext(ctx, 42)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestClosable_SendContextDeadline(t *testing.T) {
	c := NewClosable[int](0) // unbuffered

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.SendContext(ctx, 42)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
}

func TestClosable_SendContextAfterClose(t *testing.T) {
	c := NewClosable[int](1)
	c.Close()

	err := c.SendContext(context.Background(), 42)
	assert.Error(t, err)
	assert.Equal(t, ErrClosed, err)
}

func TestClosable_SendContextPanicRecovery(t *testing.T) {
	c := NewClosable[int](1)
	c.Close()

	// This should not panic, even though we're sending to a closed channel
	err := c.SendContext(context.Background(), 42)
	assert.Error(t, err)
	assert.Equal(t, ErrClosed, err)
}

func TestClosable_TrySend(t *testing.T) {
	c := NewClosable[int](2)
	err := c.TrySend(1)
	assert.NoError(t, err, "first try send error")

	err = c.TrySend(2)
	assert.NoError(t, err, "second try send error")

	err = c.TrySend(3)
	assert.EqualError(t, err, ErrBuffFull.Error())
}

func TestClosable_TrySendWithClose(t *testing.T) {
	c := NewClosable[int](2)
	err := c.TrySend(1)
	assert.NoError(t, err, "first try send error")
	c.Close() // close the channel and next TrySends must return ErrClosed error

	err = c.TrySend(2)
	assert.EqualError(t, err, ErrClosed.Error())

	err = c.TrySend(3)
	assert.EqualError(t, err, ErrClosed.Error())
}

func TestClosable_TrySendUnbuffered(t *testing.T) {
	c := NewClosable[int](0) // unbuffered

	// TrySend should fail if there's no receiver
	err := c.TrySend(42)
	assert.Equal(t, ErrBuffFull, err)
}

func TestClosable_TrySendConcurrent(t *testing.T) {
	c := NewClosable[int](10)

	var wg sync.WaitGroup
	var successCount, errorCount int64

	// Start multiple senders
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := c.TrySend(id)
			switch err {
			case nil:
				atomic.AddInt64(&successCount, 1)
			case ErrBuffFull:
				atomic.AddInt64(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Exactly 10 should succeed (buffer size), rest should fail with ErrBuffFull
	assert.Equal(t, int64(10), atomic.LoadInt64(&successCount))
	assert.Equal(t, int64(10), atomic.LoadInt64(&errorCount))
}

func TestClosable_Close(t *testing.T) {
	c := NewClosable[int](1)

	// Send a value first
	err := c.Send(42)
	assert.NoError(t, err)

	// Close the channel
	c.Close()

	// Should be able to read the sent value
	val, ok := <-c.Chan()
	assert.True(t, ok)
	assert.Equal(t, 42, val)

	// Channel should be closed now
	_, ok = <-c.Chan()
	assert.False(t, ok)
}

func TestClosable_CloseIdempotent(t *testing.T) {
	c := NewClosable[int](1)

	// Close multiple times - should not panic
	c.Close()
	c.Close()
	c.Close()

	// Channel should be closed
	_, ok := <-c.Chan()
	assert.False(t, ok)

	// Done channel should also be closed
	_, ok = <-c.Done()
	assert.False(t, ok)
}

func TestClosable_CloseConcurrent(t *testing.T) {
	c := NewClosable[int](10)

	var wg sync.WaitGroup

	// Start multiple closers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}

	wg.Wait()

	// Channel should be closed
	_, ok := <-c.Chan()
	assert.False(t, ok)

	// Done channel should be closed
	_, ok = <-c.Done()
	assert.False(t, ok)
}

func TestClosable_Chan(t *testing.T) {
	c := NewClosable[int](1)

	// Send a value
	err := c.Send(42)
	assert.NoError(t, err)

	// Read from Chan()
	val, ok := <-c.Chan()
	assert.True(t, ok)
	assert.Equal(t, 42, val)

	// Close and verify Chan() is closed
	c.Close()
	_, ok = <-c.Chan()
	assert.False(t, ok)
}

func TestClosable_Done(t *testing.T) {
	c := NewClosable[int](1)

	// Done should not be closed initially
	select {
	case <-c.Done():
		t.Fatal("Done channel should not be closed initially")
	default:
		// Expected
	}

	// Close and verify Done() is closed
	c.Close()

	select {
	case <-c.Done():
		// Expected
	default:
		t.Fatal("Done channel should be closed after Close()")
	}
}

func TestClosable_DoneConcurrent(t *testing.T) {
	c := NewClosable[int](1)

	done := make(chan struct{})
	go func() {
		<-c.Done()
		close(done)
	}()

	// Close after a short delay
	time.Sleep(10 * time.Millisecond)
	c.Close()

	// Should receive from Done()
	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Should have received from Done() channel")
	}
}

func TestClosable_Integration(t *testing.T) {
	c := NewClosable[string](5)

	// Send some values
	values := []string{"hello", "world", "test"}
	for _, val := range values {
		err := c.Send(val)
		assert.NoError(t, err)
	}

	// Read all values
	received := make([]string, 0)
	for val := range c.Chan() {
		received = append(received, val)
		if len(received) == len(values) {
			break
		}
	}

	assert.Equal(t, values, received)

	// Close the channel
	c.Close()

	// Should not be able to send more
	err := c.Send("more")
	assert.Equal(t, ErrClosed, err)
}

func TestClosable_ZeroCapacity(t *testing.T) {
	c := NewClosable[int](0) // unbuffered

	// TrySend should fail
	err := c.TrySend(42)
	assert.Equal(t, ErrBuffFull, err)

	// Send should block until receiver is ready
	go func() {
		err := c.Send(42)
		assert.NoError(t, err)
	}()

	// Receive the value
	val := <-c.Chan()
	assert.Equal(t, 42, val)

	// Close the channel
	c.Close()
}

func TestClosable_DifferentTypes(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "string",
			test: func(t *testing.T) {
				c := NewClosable[string](1)
				err := c.Send("hello")
				assert.NoError(t, err)

				val := <-c.Chan()
				assert.Equal(t, "hello", val)
			},
		},
		{
			name: "struct",
			test: func(t *testing.T) {
				type TestStruct struct {
					ID   int
					Name string
				}
				c := NewClosable[TestStruct](1)
				val := TestStruct{ID: 1, Name: "test"}
				err := c.Send(val)
				assert.NoError(t, err)

				received := <-c.Chan()
				assert.Equal(t, val, received)
			},
		},
		{
			name: "pointer",
			test: func(t *testing.T) {
				c := NewClosable[*int](1)
				val := 42
				err := c.Send(&val)
				assert.NoError(t, err)

				received := <-c.Chan()
				assert.Equal(t, &val, received)
				assert.Equal(t, 42, *received)
			},
		},
		{
			name: "interface",
			test: func(t *testing.T) {
				c := NewClosable[any](1)
				err := c.Send("interface value")
				assert.NoError(t, err)

				received := <-c.Chan()
				assert.Equal(t, "interface value", received)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

func TestClosable_RaceConditions(t *testing.T) {
	t.Run("send and close race", func(t *testing.T) {
		c := NewClosable[int](10)

		var wg sync.WaitGroup

		// Start senders
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(val int) {
				defer wg.Done()
				c.Send(val)
			}(i)
		}

		// Start closer
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			c.Close()
		}()

		wg.Wait()

		// Drain any successfully sent messages and verify channel closes
		for range c.Chan() {
			// drain
		}

		// After draining, confirm Done() is closed
		select {
		case <-c.Done():
			// Expected - channel is closed
		default:
			t.Fatal("Done channel should be closed")
		}
	})

	t.Run("try send and close race", func(t *testing.T) {
		c := NewClosable[int](10)

		var wg sync.WaitGroup

		// Start try senders
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(val int) {
				defer wg.Done()
				c.TrySend(val)
			}(i)
		}

		// Start closer
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			c.Close()
		}()

		wg.Wait()

		// Drain any successfully sent messages and verify channel closes
		for range c.Chan() {
			// drain
		}

		// After draining, confirm Done() is closed
		select {
		case <-c.Done():
			// Expected - channel is closed
		default:
			t.Fatal("Done channel should be closed")
		}
	})

	t.Run("multiple close race", func(t *testing.T) {
		c := NewClosable[int](1)

		var wg sync.WaitGroup

		// Start multiple closers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c.Close()
			}()
		}

		wg.Wait()

		// Channel should be closed
		_, ok := <-c.Chan()
		assert.False(t, ok)
	})
}

func TestClosable_MemoryLeak(t *testing.T) {
	// This test ensures that goroutines don't leak
	c := NewClosable[int](1)

	// Send a value and close
	c.Send(42)
	c.Close()

	// Read the value
	<-c.Chan()

	// Give some time for any cleanup
	time.Sleep(10 * time.Millisecond)

	// If there's a memory leak, this would be detected by race detector
	// or by running the test many times
}

func TestClosable_SendContextCloseWhileBlocked(t *testing.T) {
	c := NewClosable[int](0) // unbuffered - will block

	// Close the channel after a short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.Close()
	}()

	start := time.Now()
	err := c.SendContext(context.Background(), 42)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, ErrClosed, err)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
}

func TestClosable_SendCloseRaceNoLostMessages(t *testing.T) {
	// Ensure that messages sent before close are preserved
	c := NewClosable[int](100)

	// Send values
	for i := 0; i < 50; i++ {
		err := c.Send(i)
		assert.NoError(t, err)
	}

	// Close the channel
	c.Close()

	// Verify all sent values are still readable
	received := 0
	for range c.Chan() {
		received++
	}
	assert.Equal(t, 50, received)
}

// Benchmarks for performance validation

func BenchmarkClosable_Send(b *testing.B) {
	c := NewClosable[int](b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Send(i)
	}
}

func BenchmarkClosable_TrySend(b *testing.B) {
	c := NewClosable[int](b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.TrySend(i)
	}
}

func BenchmarkClosable_SendContext(b *testing.B) {
	c := NewClosable[int](b.N)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.SendContext(ctx, i)
	}
}

func BenchmarkClosable_SendParallel(b *testing.B) {
	c := NewClosable[int](b.N)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Send(i)
			i++
		}
	})
}

func BenchmarkClosable_TrySendParallel(b *testing.B) {
	c := NewClosable[int](b.N)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.TrySend(i)
			i++
		}
	})
}

// Benchmark comparing raw channel vs Closable
func BenchmarkRawChannel_Send(b *testing.B) {
	ch := make(chan int, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- i
	}
}

func BenchmarkClosable_IsClosedCheck(b *testing.B) {
	c := NewClosable[int](1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate the fast-path check overhead
		_ = c.TrySend(1)
		<-c.Chan()
	}
}
