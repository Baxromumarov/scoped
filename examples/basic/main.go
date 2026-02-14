// Package main demonstrates core scoped concurrency primitives:
// Run, Spawn, error policies, concurrency limits, and panic recovery.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/baxromumarov/scoped"
)

func main() {
	basicRun()
	failFastPolicy()
	collectPolicy()
	concurrencyLimit()
	nestedTasks()
	panicRecovery()
	manualScope()
}

// basicRun shows the simplest usage: spawn tasks and wait for completion.
func basicRun() {
	fmt.Println("=== Basic Run ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("fetch-users", func(ctx context.Context, _ scoped.Spawner) error {
			fmt.Println("  fetching users...")
			time.Sleep(10 * time.Millisecond)
			fmt.Println("  users fetched")
			return nil
		})

		sp.Spawn("fetch-orders", func(ctx context.Context, _ scoped.Spawner) error {
			fmt.Println("  fetching orders...")
			time.Sleep(15 * time.Millisecond)
			fmt.Println("  orders fetched")
			return nil
		})
	})

	fmt.Printf("  result: %v\n\n", err)
}

// failFastPolicy demonstrates the default error policy: the first error
// cancels all sibling tasks immediately.
func failFastPolicy() {
	fmt.Println("=== FailFast Policy ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("will-fail", func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("database connection refused")
		})

		sp.Spawn("will-be-cancelled", func(ctx context.Context, _ scoped.Spawner) error {
			// This task's context gets cancelled when "will-fail" errors.
			<-ctx.Done()
			fmt.Println("  cancelled:", ctx.Err())
			return ctx.Err()
		})
	})

	// Only the first error is returned.
	fmt.Printf("  error: %v\n", err)

	// Extract task metadata from the error.
	if info, ok := scoped.TaskOf(err); ok {
		fmt.Printf("  failed task: %q\n", info.Name)
	}
	fmt.Println()
}

// collectPolicy demonstrates gathering all errors without cancelling siblings.
func collectPolicy() {
	fmt.Println("=== Collect Policy ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := range 5 {
			sp.Spawn(fmt.Sprintf("task-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				if i%2 == 0 {
					return fmt.Errorf("task %d failed", i)
				}
				return nil
			})
		}
	}, scoped.WithPolicy(scoped.Collect))

	// All errors are collected and joined.
	taskErrs := scoped.AllTaskErrors(err)
	fmt.Printf("  total errors: %d\n", len(taskErrs))
	for _, te := range taskErrs {
		fmt.Printf("    - %s: %v\n", te.Task.Name, scoped.CauseOf(te))
	}
	fmt.Println()
}

// concurrencyLimit shows how to bound the number of concurrent goroutines.
func concurrencyLimit() {
	fmt.Println("=== Concurrency Limit ===")

	start := time.Now()
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// Spawn 10 tasks but only allow 3 to run at a time.
		for i := range 10 {
			sp.Spawn(fmt.Sprintf("job-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				time.Sleep(20 * time.Millisecond)
				return nil
			})
		}
	}, scoped.WithLimit(3))

	fmt.Printf("  10 jobs with limit=3 took %v (err=%v)\n\n", time.Since(start).Round(time.Millisecond), err)
}

// nestedTasks demonstrates hierarchical task spawning.
func nestedTasks() {
	fmt.Println("=== Nested Tasks ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("parent", func(ctx context.Context, childSp scoped.Spawner) error {
			fmt.Println("  parent started")

			// The child spawner lets the parent spawn sub-tasks.
			for i := range 3 {
				childSp.Spawn(fmt.Sprintf("child-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
					fmt.Printf("    child-%d running\n", i)
					time.Sleep(time.Duration(rand.IntN(10)) * time.Millisecond)
					return nil
				})
			}

			fmt.Println("  parent waiting for children (scope handles this)")
			return nil
		})
	})

	fmt.Printf("  result: %v\n\n", err)
}

// panicRecovery shows how panics in tasks are captured and surfaced.
func panicRecovery() {
	fmt.Println("=== Panic Recovery ===")

	// Default: panic is re-raised in Run.
	func() {
		defer func() {
			if r := recover(); r != nil {
				pe := r.(*scoped.PanicError)
				fmt.Printf("  caught panic: %v\n", pe.Value)
			}
		}()
		_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
			sp.Spawn("panicker", func(ctx context.Context, _ scoped.Spawner) error {
				panic("something went wrong")
			})
		})
	}()

	// WithPanicAsError: panic is returned as a regular error.
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("panicker", func(ctx context.Context, _ scoped.Spawner) error {
			panic("oops")
		})
	}, scoped.WithPanicAsError())

	var pe *scoped.PanicError
	if errors.As(err, &pe) {
		fmt.Printf("  panic as error: %v\n", pe.Value)
	}
	fmt.Println()
}

// manualScope demonstrates using New/Wait for manual lifecycle control.
func manualScope() {
	fmt.Println("=== Manual Scope (New/Wait) ===")

	sc, sp := scoped.New(context.Background(), scoped.WithLimit(2))

	// Spawn tasks from outside the Run callback.
	for i := range 5 {
		sp.Spawn(fmt.Sprintf("worker-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
			time.Sleep(5 * time.Millisecond)
			return nil
		})
	}

	// Explicitly wait for all tasks to finish.
	err := sc.Wait()
	fmt.Printf("  result: %v\n\n", err)
}
