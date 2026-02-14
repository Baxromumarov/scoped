package scoped_test

import (
	"context"
	"fmt"
	"sort"

	"github.com/baxromumarov/scoped"
)

func ExampleRun() {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("greet", func(ctx context.Context, _ scoped.Spawner) error {
			fmt.Println("hello from task")
			return nil
		})
	})
	if err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// hello from task
}

func ExampleRun_collect() {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := range 3 {
			sp.Spawn(fmt.Sprintf("task-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				if i == 1 {
					return fmt.Errorf("task %d failed", i)
				}
				return nil
			})
		}
	}, scoped.WithPolicy(scoped.Collect))

	if err != nil {
		fmt.Println("got errors")
	}
	// Output:
	// got errors
}

func ExampleNew() {
	sc, sp := scoped.New(context.Background())
	sp.Spawn("work", func(ctx context.Context, _ scoped.Spawner) error {
		fmt.Println("doing work")
		return nil
	})
	err := sc.Wait()
	if err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// doing work
}

func ExampleSpawnResult() {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		r := scoped.SpawnResult(sp, "compute", func(ctx context.Context) (int, error) {
			return 42, nil
		})
		val, err := r.Wait()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println("result:", val)
	})
	if err != nil {
		fmt.Println("scope error:", err)
	}
	// Output:
	// result: 42
}

func ExampleForEachSlice() {
	items := []string{"a", "b", "c"}
	err := scoped.ForEachSlice(context.Background(), items, func(ctx context.Context, s string) error {
		// process each item concurrently
		return nil
	}, scoped.WithLimit(2))
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println("done")
	// Output:
	// done
}

func ExampleMapSlice() {
	items := []int{1, 2, 3}
	results, err := scoped.MapSlice(context.Background(), items, func(ctx context.Context, v int) (int, error) {
		return v * 10, nil
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, r := range results {
		fmt.Println(r.Value)
	}
	// Output:
	// 10
	// 20
	// 30
}

func ExampleFromSlice() {
	s := scoped.FromSlice([]int{1, 2, 3, 4, 5})
	res, err := s.Filter(func(v int) bool {
		return v%2 == 0
	}).ToSlice(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(res)
	// Output:
	// [2 4]
}

func ExampleMap() {
	s := scoped.FromSlice([]int{1, 2, 3})
	doubled := scoped.Map(s, func(ctx context.Context, v int) (string, error) {
		return fmt.Sprintf("%d", v*2), nil
	})
	res, err := doubled.ToSlice(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(res)
	// Output:
	// [2 4 6]
}

func ExampleBatch() {
	s := scoped.FromSlice([]int{1, 2, 3, 4, 5})
	batches := scoped.Batch(s, 2)
	res, err := batches.ToSlice(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(res)
	// Output:
	// [[1 2] [3 4] [5]]
}

func ExampleParallelMap() {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		src := scoped.FromSlice([]int{1, 2, 3, 4, 5})
		pm := scoped.ParallelMap(context.Background(), sp, src,
			scoped.StreamOptions{MaxWorkers: 3, Ordered: true},
			func(ctx context.Context, v int) (int, error) {
				return v * 10, nil
			},
		)
		res, err := pm.ToSlice(context.Background())
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(res)
	})
	if err != nil {
		fmt.Println("scope error:", err)
	}
	// Output:
	// [10 20 30 40 50]
}

func ExampleParallelMap_unordered() {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		src := scoped.FromSlice([]int{3, 1, 2})
		pm := scoped.ParallelMap(context.Background(), sp, src,
			scoped.StreamOptions{MaxWorkers: 3},
			func(ctx context.Context, v int) (int, error) {
				return v * 10, nil
			},
		)
		res, err := pm.ToSlice(context.Background())
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		sort.Ints(res)
		fmt.Println(res)
	})
	if err != nil {
		fmt.Println("scope error:", err)
	}
	// Output:
	// [10 20 30]
}

func ExampleStream_Take() {
	s := scoped.FromSlice([]int{1, 2, 3, 4, 5}).Take(3)
	res, err := s.ToSlice(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(res)
	// Output:
	// [1 2 3]
}

func ExampleStream_Skip() {
	s := scoped.FromSlice([]int{1, 2, 3, 4, 5}).Skip(2)
	res, err := s.ToSlice(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(res)
	// Output:
	// [3 4 5]
}

func ExampleStream_ForEach() {
	err := scoped.FromSlice([]int{1, 2, 3}).ForEach(context.Background(), func(v int) error {
		fmt.Println(v)
		return nil
	})
	if err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// 1
	// 2
	// 3
}

func ExampleStream_Count() {
	count, err := scoped.FromSlice([]int{1, 2, 3, 4, 5}).Count(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(count)
	// Output:
	// 5
}

func ExampleAllTaskErrors() {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("a", func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("error a")
		})
		sp.Spawn("b", func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("error b")
		})
	}, scoped.WithPolicy(scoped.Collect))

	taskErrs := scoped.AllTaskErrors(err)
	names := make([]string, len(taskErrs))
	for i, te := range taskErrs {
		names[i] = te.Task.Name
	}
	sort.Strings(names)
	fmt.Println("failed tasks:", names)
	// Output:
	// failed tasks: [a b]
}
