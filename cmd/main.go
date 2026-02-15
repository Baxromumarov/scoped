/*
package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/baxromumarov/scoped"
)

func main() {
	start := time.Now()
	var arr = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()

	var res []string
	sc, sp := scoped.New(ctx)
	stream := scoped.ParallelMap(ctx, sp,
		scoped.FromSlice(arr),
		scoped.StreamOptions{MaxWorkers: 10, Ordered: true},
		func(ctx context.Context, v int) (string, error) {
			time.Sleep(1 * time.Second)
			return strconv.Itoa(v), nil
		},
	)
	res, _ = stream.ToSlice(ctx)
	if err := sc.Wait(); err != nil {
		panic(err)
	}

	fmt.Printf("  Map result: %v\n", res)
	fmt.Printf("  Execution time: %v\n", time.Since(start))
}
*/


// Native version without using the library, for comparison

package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

func main() {
	start := time.Now()
	arr := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	res := make([]string, len(arr))
	sem := make(chan struct{}, 10) // MaxWorkers
	var wg sync.WaitGroup

	for i, v := range arr {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, v int) {
			defer func() { <-sem; wg.Done() }()
			time.Sleep(1 * time.Second)
			res[i] = strconv.Itoa(v) // index preserves order
		}(i, v)
	}
	wg.Wait()

	fmt.Printf("  Map result: %v\n", res)
	fmt.Printf("  Execution time: %v\n", time.Since(start))
}
