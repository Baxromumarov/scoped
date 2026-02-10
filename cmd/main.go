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

func w3(ctx context.Context) error {
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
				f := f
				sp.Go(
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

// func main() {
// 	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
// 	defer cancel()

// 	arr := []func(context.Context) error{w3, w1, w2, w3}

// 	now := time.Now()

// 	scope, ctx := scoped.New(ctx, scoped.WithPanicAsError())

// 	for idx, f := range arr {
// 		f := f
// 		scope.Go(
// 			fmt.Sprintf("worker: %d", idx),
// 			func(ctx context.Context) error {
// 				return f(ctx)
// 			},
// 		)
// 	}

// 	if err := scope.Wait(); err != nil {
// 		fmt.Println(err)
// 	}

// 	fmt.Println("Elapsed time: ", time.Since(now).Seconds())
// }

// func main() {
// 	arr := []func() error{w1, w2, w3}

// 	now := time.Now()
// 	for _, f := range arr {
// 		if err := f(); err != nil {
// 			fmt.Println(">>>>>>> ", err)
// 		}
// 	}

// 	fmt.Println("Elapsed time: ", time.Since(now).Seconds())
// }

// func main() {
// 	ch := make(chan int, 5)
// 	for i := 0; i < 5; i++ {
// 		ch <- i
// 	}
// 	close(ch)

// 	out := chanx.Map(
// 		context.Background(),
// 		ch,
// 		strconv.Itoa,
// 	)

// 	for v := range out {
// 		fmt.Println(v)
// 	}
// }

// func main() {
// 	ch := make(chan int, 5)
// 	for i := 0; i < 5; i++ {
// 		ch <- i
// 	}
// 	close(ch)

// 	out := chanx.Filter(
// 		context.Background(),
// 		ch,
// 		func(i int) bool {
// 			return i%2 == 0
// 		},
// 	)

// 	for v := range out {
// 		println(v)
// 	}
// }

// func main() {
// 	ch := make(chan int)
// 	out := chanx.Map(
// 		context.Background(),
// 		ch,
// 		func(in int) int {
// 			return in * 2
// 		},
// 	)

// 	go func() {
// 		for i := 0; i < 5; i++ {
// 			ch <- i
// 		}
// 		close(ch)
// 	}()

// 	for val := range out {
// 		fmt.Println(val)
// 	}
// }

// func main() {
// 	c := chanx.NewClosable[int](5)
// 	for i := 0; i < 10; i++ {
// 		err := c.Send(i)
// 		if err != nil {
// 			fmt.Println(err)
// 		}
// 	}

// 	//ch := make(chan int, 4)
// 	//ch <- 1
// 	//ch <- 2
// 	//ch <- 3
// 	//ch <- 4
// 	//ch <- 5
// 	//close(ch)
// 	//chanx.Drain(ch)
// 	//chanx.Merge()
// 	//
// 	//val, ok := <-ch
// 	//fmt.Println(val, ok)
// }

/*
func main() {
	err := scoped.Run(
		context.Background(),
		func(s *scoped.Scope) {
			s.Go("first child", func(ctx context.Context) error {
				fmt.Println("first child")

				s.Go("second child", func(ctx context.Context) error {
					fmt.Println("second child")
					s.Go("third child", func(ctx context.Context) error {
						fmt.Println("third child")

						err := scoped.Run(ctx, func(s *scoped.Scope) {
							s.Go("fourth child from another parent", func(ctx context.Context) error {
								fmt.Println("fourth child from another parent")
								time.Sleep(1 * time.Second)
								panic("error just test")
								return nil
							})
						})

						return err

					})

					return nil
				})

				return nil
			})
			if err := s.Wait(); err != nil {
				fmt.Println("SOME function didn't work, ", err)
				return
			}
		},
		scoped.WithPolicy(scoped.Collect),
		scoped.WithPanicAsError(),
	)

	if err != nil {
	}
}
*/
