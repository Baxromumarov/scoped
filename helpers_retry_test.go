package scoped

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpawnRetrySuccessFirstAttempt(t *testing.T) {
	var calls atomic.Int32

	err := Run(context.Background(), func(sp Spawner) {
		SpawnRetry(sp, "immediate", 3, 10*time.Millisecond, func(ctx context.Context, _ Spawner) error {
			calls.Add(1)
			return nil
		})
	})

	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load(), "fn should be called exactly once on first success")
}

func TestSpawnRetrySuccessAfterRetries(t *testing.T) {
	var calls atomic.Int32

	err := Run(context.Background(), func(sp Spawner) {
		SpawnRetry(sp, "retry-then-ok", 5, 1*time.Millisecond, func(ctx context.Context, _ Spawner) error {
			n := calls.Add(1)
			if n <= 2 {
				return errors.New("transient failure")
			}
			return nil
		})
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load(), "fn should be called 3 times: 2 failures + 1 success")
}

func TestSpawnRetryAllFail(t *testing.T) {
	var calls atomic.Int32
	lastErr := errors.New("final failure")

	err := Run(context.Background(), func(sp Spawner) {
		SpawnRetry(sp, "all-fail", 2, 1*time.Millisecond, func(ctx context.Context, _ Spawner) error {
			calls.Add(1)
			return lastErr
		})
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, lastErr, "should return the last error after exhausting retries")
	assert.Equal(t, int32(3), calls.Load(), "fn should be called n+1 times (initial + 2 retries)")
}

func TestSpawnRetryContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32

	err := Run(ctx, func(sp Spawner) {
		SpawnRetry(sp, "cancel-during-backoff", 10, 500*time.Millisecond, func(ctx context.Context, _ Spawner) error {
			n := calls.Add(1)
			if n == 1 {
				// Cancel during the upcoming backoff sleep.
				go func() {
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
				return errors.New("trigger retry")
			}
			return nil
		})
	})

	assert.ErrorIs(t, err, context.Canceled,
		"should return context.Canceled when cancelled during backoff")
	assert.Equal(t, int32(1), calls.Load(),
		"fn should only be called once before cancellation during backoff")
}

func TestSpawnRetryPanicsOnInvalidArgs(t *testing.T) {
	mustPanic(t, "SpawnRetry requires n >= 0", func() {
		Run(context.Background(), func(sp Spawner) {
			SpawnRetry(sp, "bad-n", -1, time.Millisecond, func(ctx context.Context, _ Spawner) error {
				return nil
			})
		})
	})

	mustPanic(t, "SpawnRetry requires backoff > 0", func() {
		Run(context.Background(), func(sp Spawner) {
			SpawnRetry(sp, "bad-backoff", 1, 0, func(ctx context.Context, _ Spawner) error {
				return nil
			})
		})
	})

	mustPanic(t, "SpawnRetry requires backoff > 0", func() {
		Run(context.Background(), func(sp Spawner) {
			SpawnRetry(sp, "negative-backoff", 1, -time.Second, func(ctx context.Context, _ Spawner) error {
				return nil
			})
		})
	})
}
