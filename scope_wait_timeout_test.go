package scoped

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitTimeoutExpires(t *testing.T) {
	sc, sp := New(context.Background())

	sp.Spawn("blocker", func(ctx context.Context, _ Spawner) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	})

	err := sc.WaitTimeout(50 * time.Millisecond)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"WaitTimeout should return DeadlineExceeded when tasks are still running")

	// Clean up: cancel the scope so the blocker exits.
	sc.Cancel(errors.New("test cleanup"))
	_ = sc.Wait()
}

func TestWaitTimeoutSuccess(t *testing.T) {
	sc, sp := New(context.Background())

	sp.Spawn("fast", func(ctx context.Context, _ Spawner) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	})

	err := sc.WaitTimeout(1 * time.Second)
	assert.NoError(t, err, "WaitTimeout should succeed when tasks finish before deadline")
}

func TestWaitTimeoutThenWait(t *testing.T) {
	sc, sp := New(context.Background())

	sentinel := errors.New("delayed error")
	sp.Spawn("slow", func(ctx context.Context, _ Spawner) error {
		time.Sleep(100 * time.Millisecond)
		return sentinel
	})

	// Timeout first.
	err := sc.WaitTimeout(10 * time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// Wait should eventually return the actual error.
	err = sc.Wait()
	assert.ErrorIs(t, err, sentinel,
		"Wait after WaitTimeout should return the task error")
}

func TestWaitTimeoutWithError(t *testing.T) {
	sc, sp := New(context.Background())

	sentinel := errors.New("quick failure")
	sp.Spawn("fail-fast", func(ctx context.Context, _ Spawner) error {
		return sentinel
	})

	err := sc.WaitTimeout(1 * time.Second)
	assert.ErrorIs(t, err, sentinel,
		"WaitTimeout should return the task error when task completes within timeout")
}
