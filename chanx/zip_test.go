package chanx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZipBasic(t *testing.T) {
	ctx := context.Background()
	chA := make(chan int, 3)
	chB := make(chan string, 3)

	chA <- 1
	chA <- 2
	chA <- 3
	close(chA)

	chB <- "a"
	chB <- "b"
	chB <- "c"
	close(chB)

	out := Zip(ctx, chA, chB)

	var pairs []Pair[int, string]
	for p := range out {
		pairs = append(pairs, p)
	}

	require.Len(t, pairs, 3)
	assert.Equal(t, Pair[int, string]{First: 1, Second: "a"}, pairs[0])
	assert.Equal(t, Pair[int, string]{First: 2, Second: "b"}, pairs[1])
	assert.Equal(t, Pair[int, string]{First: 3, Second: "c"}, pairs[2])
}

func TestZipUnequalLength(t *testing.T) {
	ctx := context.Background()

	t.Run("chA shorter", func(t *testing.T) {
		chA := make(chan int, 2)
		chB := make(chan string, 4)

		chA <- 1
		chA <- 2
		close(chA)

		chB <- "a"
		chB <- "b"
		chB <- "c"
		chB <- "d"
		close(chB)

		out := Zip(ctx, chA, chB)

		var pairs []Pair[int, string]
		for p := range out {
			pairs = append(pairs, p)
		}

		require.Len(t, pairs, 2)
		assert.Equal(t, Pair[int, string]{First: 1, Second: "a"}, pairs[0])
		assert.Equal(t, Pair[int, string]{First: 2, Second: "b"}, pairs[1])
	})

	t.Run("chB shorter", func(t *testing.T) {
		chA := make(chan int, 4)
		chB := make(chan string, 1)

		chA <- 10
		chA <- 20
		chA <- 30
		chA <- 40
		close(chA)

		chB <- "x"
		close(chB)

		out := Zip(ctx, chA, chB)

		var pairs []Pair[int, string]
		for p := range out {
			pairs = append(pairs, p)
		}

		require.Len(t, pairs, 1)
		assert.Equal(t, Pair[int, string]{First: 10, Second: "x"}, pairs[0])
	})
}

func TestZipNilInput(t *testing.T) {
	ctx := context.Background()

	t.Run("chA nil", func(t *testing.T) {
		chB := make(chan string, 1)
		chB <- "x"
		close(chB)

		out := Zip[int](ctx, nil, chB)

		_, ok := <-out
		assert.False(t, ok, "output should be closed immediately when chA is nil")
	})

	t.Run("chB nil", func(t *testing.T) {
		chA := make(chan int, 1)
		chA <- 1
		close(chA)

		out := Zip[int, string](ctx, chA, nil)

		_, ok := <-out
		assert.False(t, ok, "output should be closed immediately when chB is nil")
	})

	t.Run("both nil", func(t *testing.T) {
		out := Zip[int, string](ctx, nil, nil)

		_, ok := <-out
		assert.False(t, ok, "output should be closed immediately when both inputs are nil")
	})
}

func TestZipContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	chA := make(chan int)
	chB := make(chan string)

	out := Zip(ctx, chA, chB)

	// Send one pair, then cancel.
	go func() {
		chA <- 1
		chB <- "a"
		// Wait for the pair to be consumed, then cancel.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Read the first pair.
	p, ok := <-out
	require.True(t, ok)
	assert.Equal(t, Pair[int, string]{First: 1, Second: "a"}, p)

	// After cancel, output should close.
	_, ok = <-out
	assert.False(t, ok)
}

func TestZipEmptyChannels(t *testing.T) {
	ctx := context.Background()
	chA := make(chan int)
	chB := make(chan string)

	close(chA)
	close(chB)

	out := Zip(ctx, chA, chB)

	_, ok := <-out
	assert.False(t, ok, "output should be closed immediately when both inputs are empty")
}
