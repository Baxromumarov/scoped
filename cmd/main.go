package main

import (
	"context"
	"fmt"
	"time"

	"github.com/baxromumarov/scoped"
)

func w1(ctx context.Context) error {
	select {
	case <-time.After(1 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func w2(ctx context.Context) error {
	select {
	case <-time.After(1 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func w3(_ context.Context) error {
	return fmt.Errorf("w3 failed")
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	arr := []func(context.Context) error{w3, w1, w2}

	now := time.Now()

	err := scoped.Run(
		ctx,
		func(sp scoped.Spawner) {
			for idx, f := range arr {
				sp.Spawn(
					fmt.Sprintf("%d index", idx),
					func(ctx context.Context, _ scoped.Spawner) error {
						return f(ctx)
					},
				)
			}
		},
		scoped.WithPolicy(scoped.FailFast),
		scoped.WithPanicAsError(),
	)

	if err != nil {
		fmt.Println("Final error:", err)
	}

	fmt.Println("Elapsed time:", time.Since(now))
}
