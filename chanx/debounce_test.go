package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebounceBasic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	out := Debounce(ctx, in, 100*time.Millisecond)

	// Send a rapid burst of values; only the last should be emitted.
	go func() {
		for i := 1; i <= 5; i++ {
			in <- i
			time.Sleep(10 * time.Millisecond) // well under the 100ms debounce window
		}
		// Wait for the debounce timer to fire, then close.
		time.Sleep(200 * time.Millisecond)
		close(in)
	}()

	var received []int
	for v := range out {
		received = append(received, v)
	}

	require.Len(t, received, 1)
	assert.Equal(t, 5, received[0], "only the last value of the burst should be emitted")
}

func TestDebounceMultipleBursts(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	out := Debounce(ctx, in, 80*time.Millisecond)

	go func() {
		// First burst
		in <- 1
		time.Sleep(10 * time.Millisecond)
		in <- 2
		time.Sleep(10 * time.Millisecond)
		in <- 3

		// Wait longer than the debounce period so value 3 is emitted.
		time.Sleep(150 * time.Millisecond)

		// Second burst
		in <- 10
		time.Sleep(10 * time.Millisecond)
		in <- 20

		// Wait for the debounce to emit value 20, then close.
		time.Sleep(150 * time.Millisecond)
		close(in)
	}()

	var received []int
	for v := range out {
		received = append(received, v)
	}

	require.Len(t, received, 2, "should emit one value per burst")
	assert.Equal(t, 3, received[0])
	assert.Equal(t, 20, received[1])
}

func TestDebounceNilInput(t *testing.T) {
	ctx := context.Background()
	out := Debounce[int](ctx, nil, 100*time.Millisecond)

	_, ok := <-out
	assert.False(t, ok, "output should be closed immediately for nil input")
}

func TestDebounceContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	out := Debounce(ctx, in, 100*time.Millisecond)

	// Send a value, then cancel during the quiet period.
	go func() {
		in <- 42
		time.Sleep(30 * time.Millisecond) // mid quiet period
		cancel()
	}()

	// Output should close without emitting anything (cancel came before timer).
	var received []int
	for v := range out {
		received = append(received, v)
	}

	// Context was cancelled during the quiet period, so the pending value
	// may or may not have been emitted depending on race. The critical
	// assertion is that the channel closes and does not hang.
	assert.LessOrEqual(t, len(received), 1)
}

func TestDebounceFlushOnClose(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	out := Debounce(ctx, in, 200*time.Millisecond)

	// Send a value and close the input immediately before the timer fires.
	go func() {
		in <- 99
		time.Sleep(20 * time.Millisecond) // far less than debounce window
		close(in)
	}()

	var received []int
	for v := range out {
		received = append(received, v)
	}

	require.Len(t, received, 1, "pending value should be flushed on input close")
	assert.Equal(t, 99, received[0])
}

func TestDebouncePanics(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)

	assert.Panics(t, func() { Debounce(ctx, in, 0) })
	assert.Panics(t, func() { Debounce(ctx, in, -1*time.Millisecond) })
}
