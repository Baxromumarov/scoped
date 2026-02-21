package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThrottle_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	// 5 items at 100/sec = 10ms per item. With burst=5, all should come fast.
	out := Throttle(ctx, in, 100, time.Second)

	var got []int
	for v := range out {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, got)
}

func TestThrottle_RateLimiting(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 10)
	for i := range 10 {
		in <- i
	}
	close(in)

	// 2 items per 100ms = 1 item per 50ms. Burst of 2 means first 2 are fast.
	// Remaining 8 items need 8 * 50ms = 400ms.
	start := time.Now()
	out := Throttle(ctx, in, 2, 100*time.Millisecond)

	var got []int
	for v := range out {
		got = append(got, v)
	}
	elapsed := time.Since(start)

	require.Len(t, got, 10)
	// At least 300ms for the 8 post-burst items (some tolerance).
	assert.GreaterOrEqual(t, elapsed, 300*time.Millisecond)
}

func TestThrottle_NilInput(t *testing.T) {
	out := Throttle[int](context.Background(), nil, 10, time.Second)
	_, ok := <-out
	assert.False(t, ok)
}

func TestThrottle_ClosedInput(t *testing.T) {
	in := make(chan int)
	close(in)

	out := Throttle(context.Background(), in, 10, time.Second)
	_, ok := <-out
	assert.False(t, ok)
}

func TestThrottle_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	out := Throttle(ctx, in, 1, time.Second)
	cancel()

	for range out {
	}
}

func TestThrottle_PanicsOnZeroN(t *testing.T) {
	assert.Panics(t, func() {
		Throttle[int](context.Background(), nil, 0, time.Second)
	})
}

func TestThrottle_PanicsOnNegativeN(t *testing.T) {
	assert.Panics(t, func() {
		Throttle[int](context.Background(), nil, -1, time.Second)
	})
}

func TestThrottle_PanicsOnZeroPer(t *testing.T) {
	assert.Panics(t, func() {
		Throttle[int](context.Background(), nil, 10, 0)
	})
}

func TestThrottle_PanicsOnNegativePer(t *testing.T) {
	assert.Panics(t, func() {
		Throttle[int](context.Background(), nil, 10, -time.Second)
	})
}

func TestThrottle_BurstCapacity(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	// Burst of 5, 5 per second. All 5 should come through almost instantly.
	start := time.Now()
	out := Throttle(ctx, in, 5, time.Second)

	var got []int
	for v := range out {
		got = append(got, v)
	}
	elapsed := time.Since(start)

	assert.Equal(t, []int{1, 2, 3, 4, 5}, got)
	// Should be very fast since burst covers all 5 items.
	assert.Less(t, elapsed, 500*time.Millisecond)
}
