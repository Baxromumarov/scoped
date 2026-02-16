package scoped

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolBasic(t *testing.T) {
	ctx := context.Background()
	p := NewPool(ctx, 4)

	var count atomic.Int32
	for range 10 {
		err := p.Submit(func() error {
			count.Add(1)
			return nil
		})
		require.NoError(t, err)
	}

	err := p.Close()
	require.NoError(t, err, "all tasks succeeded; Close should return nil")
	assert.Equal(t, int32(10), count.Load(), "all 10 tasks should have executed")
}

func TestPoolConcurrencyLimit(t *testing.T) {
	const workers = 3
	ctx := context.Background()
	p := NewPool(ctx, workers, WithQueueSize(20))

	var (
		active    atomic.Int32
		maxActive atomic.Int32
		wg        sync.WaitGroup
	)

	for range 20 {
		wg.Add(1)
		err := p.Submit(func() error {
			defer wg.Done()
			cur := active.Add(1)
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
			return nil
		})
		require.NoError(t, err)
	}

	wg.Wait()
	err := p.Close()
	require.NoError(t, err)

	assert.LessOrEqual(t, maxActive.Load(), int32(workers),
		"concurrent tasks should never exceed worker count")
}

func TestPoolContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := NewPool(ctx, 2, WithQueueSize(0))

	// Fill workers with blocking tasks.
	blocker := make(chan struct{})
	for range 2 {
		_ = p.Submit(func() error {
			<-blocker
			return nil
		})
	}

	// Give workers time to pick up tasks.
	time.Sleep(10 * time.Millisecond)

	cancel()

	// Submit should now return context error because the queue is full
	// and the context is cancelled.
	err := p.Submit(func() error { return nil })
	assert.ErrorIs(t, err, context.Canceled)

	close(blocker)
	_ = p.Close()
}

func TestPoolPanicRecovery(t *testing.T) {
	ctx := context.Background()
	p := NewPool(ctx, 2)

	err := p.Submit(func() error {
		panic("task panic!")
	})
	require.NoError(t, err)

	// Submit a normal task to verify the pool still works.
	var ran atomic.Bool
	err = p.Submit(func() error {
		ran.Store(true)
		return nil
	})
	require.NoError(t, err)

	closeErr := p.Close()
	require.Error(t, closeErr, "panic should surface as error in Close")

	var pe *PanicError
	assert.True(t, errors.As(closeErr, &pe), "error should be a PanicError")
	assert.True(t, ran.Load(), "subsequent tasks should still run after panic")
}

func TestPoolSubmitAfterClose(t *testing.T) {
	ctx := context.Background()
	p := NewPool(ctx, 2)

	err := p.Close()
	require.NoError(t, err)

	err = p.Submit(func() error { return nil })
	assert.ErrorIs(t, err, ErrPoolClosed)
}

func TestPoolTrySubmit(t *testing.T) {
	ctx := context.Background()
	p := NewPool(ctx, 1, WithQueueSize(0))

	blocker := make(chan struct{})
	// Use blocking Submit so we know the worker has picked up the task.
	err := p.Submit(func() error {
		<-blocker
		return nil
	})
	require.NoError(t, err)

	// Worker is busy with blocker and queue is unbuffered, so TrySubmit should fail.
	ok := p.TrySubmit(func() error { return nil })
	assert.False(t, ok, "TrySubmit should return false when queue is full")

	close(blocker)
	_ = p.Close()

	// After close, TrySubmit should also return false.
	ok = p.TrySubmit(func() error { return nil })
	assert.False(t, ok, "TrySubmit should return false after Close")
}

func TestPoolStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const (
		taskCount   = 1000
		workerCount = 10
	)

	ctx := context.Background()
	p := NewPool(ctx, workerCount)

	var count atomic.Int32
	sentinel := errors.New("intentional")
	var errCount atomic.Int32

	for i := range taskCount {
		err := p.Submit(func() error {
			count.Add(1)
			if i%100 == 0 {
				errCount.Add(1)
				return sentinel
			}
			return nil
		})
		require.NoError(t, err)
	}

	closeErr := p.Close()
	assert.Equal(t, int32(taskCount), count.Load(), "all tasks should have run")

	if errCount.Load() > 0 {
		require.Error(t, closeErr)
	}
}

func TestPoolPanicOnInvalidN(t *testing.T) {
	mustPanic(t, "NewPool requires n > 0", func() {
		NewPool(context.Background(), 0)
	})

	mustPanic(t, "NewPool requires n > 0", func() {
		NewPool(context.Background(), -1)
	})
}
