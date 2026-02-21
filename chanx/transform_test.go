package chanx

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Map tests ---

func TestMap_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)

	out := Map(ctx, in, func(v int) int { return v * 2 })

	var got []int
	for v := range out {
		got = append(got, v)
	}
	assert.Equal(t, []int{2, 4, 6}, got)
}

func TestMap_TypeConversion(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 42
	in <- 100
	close(in)

	out := Map(ctx, in, strconv.Itoa)

	var got []string
	for v := range out {
		got = append(got, v)
	}
	assert.Equal(t, []string{"1", "42", "100"}, got)
}

func TestMap_NilInput(t *testing.T) {
	out := Map(context.Background(), nil, func(v int) int { return v })
	_, ok := <-out
	assert.False(t, ok)
}

func TestMap_ClosedInput(t *testing.T) {
	in := make(chan int)
	close(in)

	out := Map(context.Background(), in, func(v int) int { return v })
	_, ok := <-out
	assert.False(t, ok)
}

func TestMap_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	out := Map(ctx, in, func(v int) int { return v * 2 })
	cancel()

	// Output should close after cancellation.
	for range out {
	}
}

func TestMap_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	in := make(chan int) // no values sent
	out := Map(ctx, in, func(v int) int { return v })

	time.Sleep(20 * time.Millisecond)

	_, ok := <-out
	assert.False(t, ok)
}

func TestMap_EmptyInput(t *testing.T) {
	in := make(chan int)
	close(in)

	out := Map(context.Background(), in, func(v int) int { return v })

	var got []int
	for v := range out {
		got = append(got, v)
	}
	assert.Empty(t, got)
}

func TestMap_Integration_WithFilter(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		in <- i
	}
	close(in)

	// Filter evens, then double them.
	evens := Filter(ctx, in, func(v int) bool { return v%2 == 0 })
	doubled := Map(ctx, evens, func(v int) int { return v * 2 })

	var got []int
	for v := range doubled {
		got = append(got, v)
	}
	assert.Equal(t, []int{4, 8, 12}, got)
}

// --- Filter tests ---

func TestFilter_BasicFunctionality(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		in <- i
	}
	close(in)

	out := Filter(ctx, in, func(v int) bool { return v%2 == 0 })

	var got []int
	for v := range out {
		got = append(got, v)
	}
	assert.Equal(t, []int{2, 4, 6}, got)
}

func TestFilter_AllPass(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)

	out := Filter(ctx, in, func(v int) bool { return true })

	var got []int
	for v := range out {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2, 3}, got)
}

func TestFilter_NonePass(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)

	out := Filter(ctx, in, func(v int) bool { return false })

	var got []int
	for v := range out {
		got = append(got, v)
	}
	assert.Empty(t, got)
}

func TestFilter_NilInput(t *testing.T) {
	out := Filter(context.Background(), nil, func(v int) bool { return true })
	_, ok := <-out
	assert.False(t, ok)
}

func TestFilter_ClosedInput(t *testing.T) {
	in := make(chan int)
	close(in)

	out := Filter(context.Background(), in, func(v int) bool { return true })
	_, ok := <-out
	assert.False(t, ok)
}

func TestFilter_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)

	out := Filter(ctx, in, func(v int) bool { return true })
	cancel()

	for range out {
	}
}

func TestFilter_DifferentTypes(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		in := make(chan string, 4)
		in <- "go"
		in <- "rust"
		in <- "zig"
		in <- "python"
		close(in)

		out := Filter(context.Background(), in, func(s string) bool {
			return len(s) <= 3
		})

		var got []string
		for v := range out {
			got = append(got, v)
		}
		assert.Equal(t, []string{"go", "zig"}, got)
	})
}

func TestFilter_Integration_WithMap(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	// Double values, then filter those > 6.
	doubled := Map(ctx, in, func(v int) int { return v * 2 })
	big := Filter(ctx, doubled, func(v int) bool { return v > 6 })

	var got []int
	for v := range big {
		got = append(got, v)
	}
	assert.Equal(t, []int{8, 10}, got)
}

func TestMap_Streaming(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	out := Map(ctx, in, func(v int) int { return v + 10 })

	go func() {
		for i := range 5 {
			in <- i
		}
		close(in)
	}()

	var got []int
	for v := range out {
		got = append(got, v)
	}
	require.Len(t, got, 5)
	assert.Equal(t, []int{10, 11, 12, 13, 14}, got)
}

// --- Take tests ---

func TestTake_Basic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	var got []int
	for v := range Take(ctx, in, 3) {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2, 3}, got)
}

func TestTake_Zero(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	close(in)

	var got []int
	for v := range Take(ctx, in, 0) {
		got = append(got, v)
	}
	assert.Empty(t, got)
}

func TestTake_MoreThanAvailable(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 2)
	in <- 1
	in <- 2
	close(in)

	var got []int
	for v := range Take(ctx, in, 10) {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2}, got)
}

func TestTake_NilInput(t *testing.T) {
	ctx := context.Background()
	out := Take[int](ctx, nil, 5)
	_, ok := <-out
	assert.False(t, ok)
}

func TestTake_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	out := Take(ctx, in, 10)
	cancel()
	_, ok := <-out
	assert.False(t, ok)
}

func TestTake_NegativePanic(t *testing.T) {
	assert.Panics(t, func() {
		Take[int](context.Background(), nil, -1)
	})
}

// --- Skip tests ---

func TestSkip_Basic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	var got []int
	for v := range Skip(ctx, in, 2) {
		got = append(got, v)
	}
	assert.Equal(t, []int{3, 4, 5}, got)
}

func TestSkip_Zero(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)

	var got []int
	for v := range Skip(ctx, in, 0) {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 2, 3}, got)
}

func TestSkip_MoreThanAvailable(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 2)
	in <- 1
	in <- 2
	close(in)

	var got []int
	for v := range Skip(ctx, in, 10) {
		got = append(got, v)
	}
	assert.Empty(t, got)
}

func TestSkip_NilInput(t *testing.T) {
	ctx := context.Background()
	out := Skip[int](ctx, nil, 5)
	_, ok := <-out
	assert.False(t, ok)
}

func TestSkip_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	out := Skip(ctx, in, 0)
	cancel()
	_, ok := <-out
	assert.False(t, ok)
}

func TestSkip_NegativePanic(t *testing.T) {
	assert.Panics(t, func() {
		Skip[int](context.Background(), nil, -1)
	})
}

// --- Scan tests ---

func TestScan_RunningSum(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 4)
	in <- 1
	in <- 2
	in <- 3
	in <- 4
	close(in)

	var got []int
	for v := range Scan(ctx, in, 0, func(acc, v int) int { return acc + v }) {
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 3, 6, 10}, got)
}

func TestScan_Empty(t *testing.T) {
	ctx := context.Background()
	in := make(chan int)
	close(in)

	var got []int
	for v := range Scan(ctx, in, 0, func(acc, v int) int { return acc + v }) {
		got = append(got, v)
	}
	assert.Empty(t, got)
}

func TestScan_NilInput(t *testing.T) {
	ctx := context.Background()
	out := Scan[int, int](ctx, nil, 0, func(acc, v int) int { return acc + v })
	_, ok := <-out
	assert.False(t, ok)
}

func TestScan_NilFnPanic(t *testing.T) {
	assert.Panics(t, func() {
		Scan[int, int](context.Background(), make(chan int), 0, nil)
	})
}

func TestScan_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	out := Scan(ctx, in, 0, func(acc, v int) int { return acc + v })
	cancel()
	_, ok := <-out
	assert.False(t, ok)
}
