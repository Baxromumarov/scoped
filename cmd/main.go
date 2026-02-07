package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/baxromumarov/scoped/chanx"
)

func main() {
	ch := make(chan int, 5)
	for i := 0; i < 5; i++ {
		ch <- i
	}
	close(ch)

	out := chanx.Map(
		context.Background(),
		ch,
		strconv.Itoa,
	)

	for v := range out {
		fmt.Println(v)
	}
}

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
