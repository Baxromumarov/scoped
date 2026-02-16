package scoped

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustPanic(t *testing.T, contains string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic")
		require.Contains(t, fmt.Sprint(r), contains)
	}()
	fn()
}

func TestSemaphoreBasic(t *testing.T) {
	sem := NewSemaphore(3)
	assert.Equal(t, 3, sem.Available(), "all slots should be available initially")

	err := sem.Acquire(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, sem.Available(), "one slot consumed")

	err = sem.Acquire(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sem.Available(), "two slots consumed")

	sem.Release()
	assert.Equal(t, 2, sem.Available(), "one slot released")

	sem.Release()
	assert.Equal(t, 3, sem.Available(), "all slots available again")
}

func TestSemaphoreTryAcquire(t *testing.T) {
	sem := NewSemaphore(2)

	ok := sem.TryAcquire()
	assert.True(t, ok, "first TryAcquire should succeed")

	ok = sem.TryAcquire()
	assert.True(t, ok, "second TryAcquire should succeed")

	ok = sem.TryAcquire()
	assert.False(t, ok, "third TryAcquire should fail; semaphore full")

	assert.Equal(t, 0, sem.Available())

	sem.Release()
	ok = sem.TryAcquire()
	assert.True(t, ok, "TryAcquire should succeed after release")
}

func TestSemaphoreContextCancel(t *testing.T) {
	sem := NewSemaphore(1)

	// Fill the single slot.
	err := sem.Acquire(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	err = sem.Acquire(ctx)
	assert.ErrorIs(t, err, context.Canceled, "acquire on cancelled context should return context.Canceled")
	assert.Equal(t, 0, sem.Available(), "no extra slot should have been consumed")

	sem.Release()
}

func TestSemaphoreConcurrency(t *testing.T) {
	const (
		total = 50
		limit = 5
	)

	sem := NewSemaphore(limit)
	var (
		active    atomic.Int32
		maxActive atomic.Int32
		wg        sync.WaitGroup
	)

	wg.Add(total)
	for range total {
		go func() {
			defer wg.Done()

			err := sem.Acquire(context.Background())
			if err != nil {
				return
			}
			defer sem.Release()

			cur := active.Add(1)
			// Atomically update high-water mark.
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}

			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
		}()
	}

	wg.Wait()

	assert.LessOrEqual(t, maxActive.Load(), int32(limit),
		"concurrent goroutines should never exceed the semaphore limit")
	assert.Equal(t, limit, sem.Available(), "all slots should be returned")
}

func TestSemaphorePanicOnOverRelease(t *testing.T) {
	sem := NewSemaphore(1)

	mustPanic(t, "Release called without matching Acquire", func() {
		sem.Release()
	})
}

func TestSemaphorePanicOnInvalidN(t *testing.T) {
	mustPanic(t, "NewSemaphore requires n > 0", func() {
		NewSemaphore(0)
	})

	mustPanic(t, "NewSemaphore requires n > 0", func() {
		NewSemaphore(-5)
	})
}
