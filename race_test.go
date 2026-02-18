package scoped

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRaceFirstWins(t *testing.T) {
	ctx := context.Background()
	val, err := Race(ctx,
		func(ctx context.Context) (int, error) {
			return 1, nil // fast
		},
		func(ctx context.Context) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, val)
}

func TestRaceAllFail(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("fail")
	_, err := Race(ctx,
		func(ctx context.Context) (int, error) { return 0, sentinel },
		func(ctx context.Context) (int, error) { return 0, errors.New("other") },
	)
	assert.Error(t, err)
}

func TestRaceEmpty(t *testing.T) {
	ctx := context.Background()
	val, err := Race[int](ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, val)
}

func TestRaceContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Race(ctx,
		func(ctx context.Context) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
	)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRaceNilTaskPanics(t *testing.T) {
	mustPanicContains(t, "must not be nil", func() {
		Race(context.Background(),
			func(ctx context.Context) (int, error) { return 1, nil },
			nil,
		)
	})
}

func TestRaceSingleTask(t *testing.T) {
	ctx := context.Background()
	val, err := Race(ctx,
		func(ctx context.Context) (int, error) { return 42, nil },
	)
	require.NoError(t, err)
	assert.Equal(t, 42, val)
}

func TestRaceCancelsLosers(t *testing.T) {
	ctx := context.Background()
	val, err := Race(ctx,
		func(ctx context.Context) (int, error) {
			return 1, nil
		},
		func(ctx context.Context) (int, error) {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(5 * time.Second):
				t.Error("loser was not cancelled")
				return 0, fmt.Errorf("timeout")
			}
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, val)
}
