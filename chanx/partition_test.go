package chanx

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPartition_Basic(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		in <- i
	}
	close(in)

	evens, odds := Partition(ctx, in, func(v int) bool { return v%2 == 0 })

	var gotEvens, gotOdds []int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for v := range evens {
			gotEvens = append(gotEvens, v)
		}
	}()
	go func() {
		defer wg.Done()
		for v := range odds {
			gotOdds = append(gotOdds, v)
		}
	}()
	wg.Wait()

	assert.Equal(t, []int{2, 4, 6}, gotEvens)
	assert.Equal(t, []int{1, 3, 5}, gotOdds)
}

func TestPartition_AllMatch(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 2
	in <- 4
	in <- 6
	close(in)

	match, rest := Partition(ctx, in, func(v int) bool { return v%2 == 0 })

	var gotMatch, gotRest []int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for v := range match {
			gotMatch = append(gotMatch, v)
		}
	}()
	go func() {
		defer wg.Done()
		for v := range rest {
			gotRest = append(gotRest, v)
		}
	}()
	wg.Wait()

	assert.Equal(t, []int{2, 4, 6}, gotMatch)
	assert.Empty(t, gotRest)
}

func TestPartition_NoneMatch(t *testing.T) {
	ctx := context.Background()
	in := make(chan int, 3)
	in <- 1
	in <- 3
	in <- 5
	close(in)

	match, rest := Partition(ctx, in, func(v int) bool { return v%2 == 0 })

	var gotMatch, gotRest []int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for v := range match {
			gotMatch = append(gotMatch, v)
		}
	}()
	go func() {
		defer wg.Done()
		for v := range rest {
			gotRest = append(gotRest, v)
		}
	}()
	wg.Wait()

	assert.Empty(t, gotMatch)
	assert.Equal(t, []int{1, 3, 5}, gotRest)
}

func TestPartition_NilInput(t *testing.T) {
	ctx := context.Background()
	match, rest := Partition[int](ctx, nil, func(v int) bool { return true })
	_, ok1 := <-match
	_, ok2 := <-rest
	assert.False(t, ok1)
	assert.False(t, ok2)
}

func TestPartition_NilFnPanic(t *testing.T) {
	assert.Panics(t, func() {
		Partition[int](context.Background(), make(chan int), nil)
	})
}

func TestPartition_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	match, rest := Partition(ctx, in, func(v int) bool { return true })
	cancel()

	// Both channels should close after context cancellation.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range match {
		}
	}()
	go func() {
		defer wg.Done()
		for range rest {
		}
	}()
	wg.Wait()
}
