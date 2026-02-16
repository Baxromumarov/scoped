package scoped

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// ForEachSlice executes fn for each item in the slice concurrently,
// using the provided options to control concurrency and error policy.
//
// This is a convenience wrapper around [Run] and [Scope.Spawn].
//
//	err := scoped.ForEachSlice(ctx, URLs, func(ctx context.Context, u string) error {
//	    return fetch(ctx, u)
//	}, scoped.WithLimit(10))
func ForEachSlice[T any](
	ctx context.Context,
	items []T,
	fn func(ctx context.Context, item T) error,
	opts ...Option,
) error {
	return Run(
		ctx,
		func(sp Spawner) {
			for i, item := range items {
				sp.Spawn(
					fmt.Sprintf("for-each [%d]", i),
					func(ctx context.Context, _ Spawner) error {
						return fn(ctx, item)
					},
				)
			}
		},
		opts...,
	)
}

// ItemResult holds the outcome of processing an individual item in [MapSlice].
// In [Collect] mode, function-level failures are captured in Err and successful
// items still have Value populated.
type ItemResult[R any] struct {
	Value R
	Err   error
}

// MapSlice executes fn for each item concurrently and collects the results
// in the same order as the input slice. It uses [FailFast] policy by default;
// pass [Collect] Policy to gather partial results and errors that happened during execution.
//
// Result semantics:
//
//   - [FailFast]: the first function error fails the whole call and MapSlice
//     returns nil results with that error.
//
//   - [Collect]: function errors are captured per item in [ItemResult.Err],
//     and MapSlice still returns the partial results slice.
//
//   - The outer returned error is reserved for scope/infrastructure failures
//     (for example context cancellation, panic-as-error, etc.).
//
//     prices, err := scoped.MapSlice(ctx, products, func(ctx context.Context, p Product) (float64, error) {
//     return fetchPrice(ctx, p)
//     }, scoped.WithLimit(5))
func MapSlice[T, R any](
	ctx context.Context,
	items []T,
	fn func(ctx context.Context, item T) (R, error),
	opts ...Option,
) (
	[]ItemResult[R],
	error,
) {
	sc, sp := New(ctx, opts...)
	policy := sc.s.cfg.policy
	results := make([]ItemResult[R], len(items))

	for i, item := range items {
		sp.Spawn(
			fmt.Sprintf("map[%d]", i),
			func(ctx context.Context, _ Spawner) error {
				r, err := fn(ctx, item)
				if err != nil {
					results[i].Err = err // safe: each goroutine writes a unique index
					if policy == Collect {
						return nil
					}
					return err
				}
				results[i].Value = r // safe: each goroutine writes a unique index
				return nil
			},
		)
	}

	err := sc.Wait()
	if err != nil {
		if policy == FailFast {
			return nil, err
		}
		return results, err
	}
	return results, nil
}

// SpawnTimeout spawns a task with a per-task deadline. If the task does not
// complete within d, its context is cancelled with [context.DeadlineExceeded].
//
// The timeout only affects this task's context; it does not cancel the scope.
func SpawnTimeout(sp Spawner, name string, d time.Duration, fn TaskFunc) {
	sp.Spawn(name, func(ctx context.Context, child Spawner) error {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		return fn(ctx, child)
	})
}

// SpawnScope spawns a sub-scope as a single task within the parent scope.
// The sub-scope has its own error policy and options, allowing hierarchical
// error handling. The sub-scope's aggregated error (if any) is propagated
// to the parent scope as the task's error.
//
// This enables patterns like running a batch of tasks with [Collect] policy
// inside a parent scope using [FailFast]:
//
//	scoped.Run(
//		ctx,
//		func(sp scoped.Spawner) {
//	    scoped.SpawnScope(sp, "batch", func(sub scoped.Spawner) {
//	        for _, item := range items {
//	            sub.Spawn(item.Name, item.Process)
//	        }
//	    }, scoped.WithPolicy(scoped.Collect))
//	})
func SpawnScope(sp Spawner, name string, fn func(sp Spawner), opts ...Option) {
	sp.Spawn(
		name,
		func(ctx context.Context, _ Spawner) error {
			return Run(ctx, fn, opts...)
		},
	)
}

// SpawnRetry spawns a task that retries on failure up to n times with
// exponential backoff starting from the given base duration.
// The backoff doubles on each retry: base, base*2, base*4, ...
// Retries stop immediately if the context is cancelled.
//
// SpawnRetry panics if n < 0 or backoff <= 0.
func SpawnRetry(
	sp Spawner,
	name string,
	n int,
	backoff time.Duration,
	fn TaskFunc,
) {
	if n < 0 {
		panic("scoped: SpawnRetry requires n >= 0")
	}
	if backoff <= 0 {
		panic("scoped: SpawnRetry requires backoff > 0")
	}

	sp.Spawn(
		name,
		func(ctx context.Context, child Spawner) error {
			var lastErr error
			delay := backoff
			for attempt := 0; attempt <= n; attempt++ {
				if attempt > 0 {
					t := time.NewTimer(delay)
					select {
					case <-t.C:
						delay *= 2
					case <-ctx.Done():
						t.Stop()
						return ctx.Err()
					}
				}
				lastErr = fn(ctx, child)
				if lastErr == nil {
					return nil
				}
			}
			return lastErr
		},
	)
}

type atomicError struct{ v atomic.Value }

type storedError struct {
	err error
}

func (a *atomicError) Store(err error) {
	a.v.Store(storedError{err: err})
}

func (a *atomicError) Load() error {
	v := a.v.Load()
	if v == nil {
		return nil
	}
	return v.(storedError).err
}
