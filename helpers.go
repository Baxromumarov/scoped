package scoped

import (
	"context"
	"fmt"
	"sync/atomic"
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
				item := item // capture for Spawn < 1.22
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
		i, item := i, item // capture for Spawn < 1.22
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
