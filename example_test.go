package scoped_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baxromumarov/scoped"
)

func ExampleRun() {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("hello", func(ctx context.Context) error {
			fmt.Println("hello")
			return nil
		})
		s.Go("world", func(ctx context.Context) error {
			fmt.Println("world")
			return nil
		})
	})
	if err != nil {
		fmt.Println("error:", err)
	}
	// Unordered output:
	// hello
	// world
}

func ExampleRun_failFast() {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("quick-fail", func(ctx context.Context) error {
			return errors.New("something went wrong")
		})
		s.Go("long-task", func(ctx context.Context) error {
			// This task will be cancelled when quick-fail returns an error.
			<-ctx.Done()
			return nil
		})
	})
	fmt.Println(err)
	// Output: something went wrong
}

func ExampleRun_collect() {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 3; i++ {
			i := i
			s.Go(fmt.Sprintf("task-%d", i), func(ctx context.Context) error {
				return fmt.Errorf("error from task %d", i)
			})
		}
	}, scoped.WithPolicy(scoped.Collect))
	fmt.Println("got errors:", err != nil)
	// Output: got errors: true
}

func ExampleRun_bounded() {
	start := time.Now()
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 6; i++ {
			s.Go("worker", func(ctx context.Context) error {
				time.Sleep(50 * time.Millisecond)
				return nil
			})
		}
	}, scoped.WithLimit(3))
	if err != nil {
		fmt.Println("error:", err)
	}
	// With limit=3 and 6 tasks sleeping 50ms, takes ~100ms (2 batches).
	elapsed := time.Since(start)
	fmt.Println("completed in <200ms:", elapsed < 200*time.Millisecond)
	// Output: completed in <200ms: true
}

func ExampleGoResult() {
	var r *scoped.Result[int]
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		r = scoped.GoResult(s, "compute", func(ctx context.Context) (int, error) {
			return 42, nil
		})
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	val, _ := r.Wait()
	fmt.Println("result:", val)
	// Output: result: 42
}

func ExampleForEach() {
	urls := []string{"a", "b", "c", "d"}
	err := scoped.ForEach(context.Background(), urls, func(ctx context.Context, url string) error {
		fmt.Println("fetching", url)
		return nil
	}, scoped.WithLimit(2))
	if err != nil {
		fmt.Println("error:", err)
	}
	// Unordered output:
	// fetching a
	// fetching b
	// fetching c
	// fetching d
}

func ExampleMap() {
	items := []int{1, 2, 3, 4, 5}
	results, err := scoped.Map(context.Background(), items, func(ctx context.Context, n int) (int, error) {
		return n * n, nil
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(results)
	// Output: [1 4 9 16 25]
}
