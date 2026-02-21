package scoped

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepNestedSpawnScope(t *testing.T) {
	t.Run("AllLevelsSucceed", func(t *testing.T) {
		var reached [4]bool

		err := Run(context.Background(), func(sp Spawner) {
			reached[0] = true
			SpawnScope(sp, "level-1", func(sp1 Spawner) {
				reached[1] = true
				SpawnScope(sp1, "level-2", func(sp2 Spawner) {
					reached[2] = true
					SpawnScope(sp2, "level-3", func(sp3 Spawner) {
						reached[3] = true
						sp3.Go("leaf", func(ctx context.Context) error {
							return nil
						})
					})
				})
			})
		})

		require.NoError(t, err)
		for i, r := range reached {
			assert.True(t, r, "level %d not reached", i)
		}
	})

	t.Run("ErrorAtDeepestLevelPropagates", func(t *testing.T) {
		deepErr := errors.New("deep failure")

		err := Run(context.Background(), func(sp Spawner) {
			SpawnScope(sp, "level-1", func(sp1 Spawner) {
				SpawnScope(sp1, "level-2", func(sp2 Spawner) {
					SpawnScope(sp2, "level-3", func(sp3 Spawner) {
						sp3.Go("failing-leaf", func(ctx context.Context) error {
							return deepErr
						})
					})
				})
			})
		})

		require.Error(t, err)
		assert.True(t, IsTaskError(err))
		assert.ErrorIs(t, CauseOf(err), deepErr)
	})

	t.Run("MixedPolicies", func(t *testing.T) {
		err1 := errors.New("err-1")
		err2 := errors.New("err-2")

		err := Run(context.Background(), func(sp Spawner) {
			SpawnScope(sp, "collect-scope", func(sp1 Spawner) {
				sp1.Go("task-a", func(ctx context.Context) error {
					return err1
				})
				sp1.Go("task-b", func(ctx context.Context) error {
					return err2
				})
			}, WithPolicy(Collect))
		})

		require.Error(t, err)
		// Both errors should be collected in the inner scope and
		// propagated as a joined error through the outer FailFast scope.
		cause := CauseOf(err)
		assert.ErrorIs(t, cause, err1)
		assert.ErrorIs(t, cause, err2)
	})

	t.Run("CancelAtMidLevelCancelsDescendants", func(t *testing.T) {
		deepStarted := make(chan struct{})
		cancelled := make(chan struct{})

		err := Run(context.Background(), func(sp Spawner) {
			// Sibling that waits until deep-task starts, then errors.
			sp.Go("fail-sibling", func(ctx context.Context) error {
				<-deepStarted
				return errors.New("sibling failure")
			})
			// Nested scope with a long-running task that should be cancelled.
			SpawnScope(sp, "level-1", func(sp1 Spawner) {
				SpawnScope(sp1, "level-2", func(sp2 Spawner) {
					sp2.Go("deep-task", func(ctx context.Context) error {
						close(deepStarted)
						<-ctx.Done()
						close(cancelled)
						return ctx.Err()
					})
				})
			})
		})

		require.Error(t, err)
		<-cancelled // deep task was cancelled by sibling's failure
	})
}
