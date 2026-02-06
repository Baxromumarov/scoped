package scoped

import (
	"context"
	"fmt"
)

// ForEach executes fn for each item in the slice concurrently,
// using the provided options to control concurrency and error policy.
//
// This is a convenience wrapper around [Run] and [Scope.Go].
//
//	err := scoped.ForEach(ctx, urls, func(ctx context.Context, u string) error {
//	    return fetch(ctx, u)
//	}, scoped.WithLimit(10))
func ForEach[T any](ctx context.Context, items []T, fn func(ctx context.Context, item T) error, opts ...Option) error {
	return Run(ctx, func(s *Scope) {
		for i, item := range items {
			item := item // capture for Go < 1.22
			s.Go(fmt.Sprintf("foreach[%d]", i), func(ctx context.Context) error {
				return fn(ctx, item)
			})
		}
	}, opts...)
}

// Map executes fn for each item concurrently and collects the results
// in the same order as the input slice. It uses [FailFast] policy by
// default; pass [WithPolicy]([Collect]) to gather partial results.
//
// On error, Map returns nil and the error. On success, it returns the
// results slice and nil.
//
//	prices, err := scoped.Map(ctx, products, func(ctx context.Context, p Product) (float64, error) {
//	    return fetchPrice(ctx, p)
//	}, scoped.WithLimit(5))
func Map[T, R any](ctx context.Context, items []T, fn func(ctx context.Context, item T) (R, error), opts ...Option) ([]R, error) {
	results := make([]R, len(items))
	err := Run(ctx, func(s *Scope) {
		for i, item := range items {
			i, item := i, item // capture for Go < 1.22
			s.Go(fmt.Sprintf("map[%d]", i), func(ctx context.Context) error {
				r, err := fn(ctx, item)
				if err != nil {
					return err
				}
				results[i] = r // safe: each goroutine writes a unique index
				return nil
			})
		}
	}, opts...)
	if err != nil {
		return nil, err
	}
	return results, nil
}
