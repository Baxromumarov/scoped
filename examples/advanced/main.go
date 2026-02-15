// Package main demonstrates advanced scoped patterns: SpawnResult, SpawnTimeout,
// observability hooks, error introspection, context cancellation, MapSlice,
// ForEachSlice, and chanx channel utilities.
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/baxromumarov/scoped"
	"github.com/baxromumarov/scoped/chanx"
)

func main() {
	spawnResultExample()
	spawnTimeoutExample()
	observabilityHooks()
	scopeObservability()
	contextCancellation()
	forEachSliceExample()
	mapSliceExample()
	mapSliceCollectExample()
	errorIntrospection()

	// new features
	spawnScopeExample()
	spawnScopeIsolation()
	maxErrorsExample()
	taskEventExample()

	// chanx utilities
	sendRecvExample()
	mergeExample()
	fanOutExample()
	teeExample()
	chanxMapFilterExample()
	throttleExample()
	bufferExample()
	firstExample()
	orDoneExample()
	closableExample()
	batchSendRecvExample()
}

// spawnResultExample demonstrates typed async results with SpawnResult.
func spawnResultExample() {
	fmt.Println("=== SpawnResult ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// SpawnResult returns a Result[T] that holds the typed value.
		userResult := scoped.SpawnResult(sp, "fetch-user", func(ctx context.Context) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "Alice", nil
		})

		orderResult := scoped.SpawnResult(sp, "fetch-order-count", func(ctx context.Context) (int, error) {
			time.Sleep(15 * time.Millisecond)
			return 42, nil
		})

		// Wait blocks until the task completes and returns the typed value.
		user, err := userResult.Wait()
		if err != nil {
			fmt.Printf("  user error: %v\n", err)
			return
		}

		count, err := orderResult.Wait()
		if err != nil {
			fmt.Printf("  order error: %v\n", err)
			return
		}

		fmt.Printf("  user=%q, orders=%d\n", user, count)
	})

	fmt.Printf("  scope error: %v\n\n", err)
}

// spawnTimeoutExample demonstrates per-task deadlines with SpawnTimeout.
func spawnTimeoutExample() {
	fmt.Println("=== SpawnTimeout ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// This task finishes quickly.
		scoped.SpawnTimeout(sp, "fast-task", 100*time.Millisecond, func(ctx context.Context, _ scoped.Spawner) error {
			fmt.Println("  fast task completed")
			return nil
		})

		// This task exceeds its deadline.
		scoped.SpawnTimeout(sp, "slow-task", 10*time.Millisecond, func(ctx context.Context, _ scoped.Spawner) error {
			select {
			case <-time.After(1 * time.Second):
				return nil
			case <-ctx.Done():
				fmt.Printf("  slow task timed out: %v\n", ctx.Err())
				return ctx.Err()
			}
		})
	}, scoped.WithPolicy(scoped.Collect))

	if err != nil {
		fmt.Printf("  scope error: %v\n", err)
	}
	fmt.Println()
}

// observabilityHooks demonstrates WithOnStart and WithOnDone hooks.
func observabilityHooks() {
	fmt.Println("=== Observability Hooks ===")

	var mu sync.Mutex
	taskLog := make([]string, 0)

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := range 3 {
			sp.Spawn(fmt.Sprintf("worker-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				time.Sleep(time.Duration(5*(i+1)) * time.Millisecond)
				return nil
			})
		}
	},
		scoped.WithOnStart(func(info scoped.TaskInfo) {
			mu.Lock()
			taskLog = append(taskLog, fmt.Sprintf("START %s", info.Name))
			mu.Unlock()
		}),
		scoped.WithOnDone(func(info scoped.TaskInfo, err error, d time.Duration) {
			mu.Lock()
			taskLog = append(taskLog, fmt.Sprintf("DONE  %s (%v, err=%v)", info.Name, d.Round(time.Millisecond), err))
			mu.Unlock()
		}),
	)

	fmt.Printf("  scope error: %v\n", err)
	for _, entry := range taskLog {
		fmt.Printf("  %s\n", entry)
	}
	fmt.Println()
}

// scopeObservability shows ActiveTasks and TotalSpawned counters.
func scopeObservability() {
	fmt.Println("=== Scope Observability ===")

	sc, sp := scoped.New(context.Background())

	for i := range 5 {
		sp.Spawn(fmt.Sprintf("task-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		})
	}

	// Counters are available while tasks are running.
	time.Sleep(5 * time.Millisecond) // let tasks start
	fmt.Printf("  total spawned: %d\n", sc.TotalSpawned())
	fmt.Printf("  active tasks (during): %d\n", sc.ActiveTasks())

	err := sc.Wait()
	fmt.Printf("  active tasks (after): %d\n", sc.ActiveTasks())
	fmt.Printf("  scope error: %v\n\n", err)
}

// contextCancellation demonstrates explicit scope cancellation.
func contextCancellation() {
	fmt.Println("=== Context Cancellation ===")

	sc, sp := scoped.New(context.Background())

	var started atomic.Int32
	for i := range 5 {
		sp.Spawn(fmt.Sprintf("worker-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
			started.Add(1)
			<-ctx.Done()
			return ctx.Err()
		})
	}

	// Wait for all workers to start, then cancel.
	time.Sleep(10 * time.Millisecond)
	fmt.Printf("  started: %d workers\n", started.Load())
	sc.Cancel(fmt.Errorf("manual shutdown"))

	err := sc.Wait()
	fmt.Printf("  scope error: %v\n\n", err)
}

// forEachSliceExample demonstrates the ForEachSlice convenience helper.
func forEachSliceExample() {
	fmt.Println("=== ForEachSlice ===")

	urls := []string{"/api/users", "/api/orders", "/api/products"}
	var mu sync.Mutex
	var fetched []string

	err := scoped.ForEachSlice(context.Background(), urls, func(ctx context.Context, url string) error {
		// Simulate fetching each URL concurrently.
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		fetched = append(fetched, url)
		mu.Unlock()
		return nil
	}, scoped.WithLimit(2))

	fmt.Printf("  fetched %d URLs (err=%v)\n\n", len(fetched), err)
}

// mapSliceExample demonstrates MapSlice with FailFast (default).
func mapSliceExample() {
	fmt.Println("=== MapSlice (FailFast) ===")

	nums := []int{1, 2, 3, 4, 5}
	results, err := scoped.MapSlice(context.Background(), nums, func(ctx context.Context, n int) (int, error) {
		return n * n, nil
	}, scoped.WithLimit(3))

	if err != nil {
		fmt.Printf("  error: %v\n\n", err)
		return
	}
	fmt.Print("  squares: ")
	for _, r := range results {
		fmt.Printf("%d ", r.Value)
	}
	fmt.Printf("(err=%v)\n\n", err)
}

// mapSliceCollectExample shows MapSlice in Collect mode with partial failures.
func mapSliceCollectExample() {
	fmt.Println("=== MapSlice (Collect) ===")

	items := []string{"valid-1", "bad", "valid-2", "bad", "valid-3"}
	results, err := scoped.MapSlice(context.Background(), items, func(ctx context.Context, s string) (string, error) {
		if s == "bad" {
			return "", fmt.Errorf("invalid item: %s", s)
		}
		return fmt.Sprintf("processed(%s)", s), nil
	}, scoped.WithPolicy(scoped.Collect))

	fmt.Printf("  scope error: %v\n", err)
	for i, r := range results {
		if r.Err != nil {
			fmt.Printf("  [%d] ERROR: %v\n", i, r.Err)
		} else {
			fmt.Printf("  [%d] OK:    %s\n", i, r.Value)
		}
	}
	fmt.Println()
}

// errorIntrospection demonstrates TaskError, CauseOf, TaskOf, and AllTaskErrors.
func errorIntrospection() {
	fmt.Println("=== Error Introspection ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("db-query", func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("connection refused")
		})
		sp.Spawn("cache-fetch", func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("cache miss")
		})
		sp.Spawn("ok-task", func(ctx context.Context, _ scoped.Spawner) error {
			return nil
		})
	}, scoped.WithPolicy(scoped.Collect))

	// IsTaskError checks if an error wraps a TaskError.
	fmt.Printf("  is task error: %v\n", scoped.IsTaskError(err))

	// AllTaskErrors extracts every TaskError from the joined error chain.
	taskErrs := scoped.AllTaskErrors(err)
	fmt.Printf("  total task errors: %d\n", len(taskErrs))

	for _, te := range taskErrs {
		// TaskOf extracts the TaskInfo metadata.
		info, _ := scoped.TaskOf(te)
		// CauseOf unwraps the underlying cause.
		cause := scoped.CauseOf(te)
		fmt.Printf("    task=%q cause=%q\n", info.Name, cause)
	}
	fmt.Println()
}

// spawnScopeExample demonstrates hierarchical sub-scopes with SpawnScope.
// The sub-scope has its own error policy, independent of the parent.
func spawnScopeExample() {
	fmt.Println("=== SpawnScope (Hierarchical Sub-Scopes) ===")

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// Parent uses FailFast, but the sub-scope uses Collect.
		// All sub-tasks run to completion despite individual failures.
		scoped.SpawnScope(sp, "batch-import", func(sub scoped.Spawner) {
			records := []string{"user-1", "invalid", "user-2", "invalid", "user-3"}
			for _, rec := range records {
				sub.Spawn(fmt.Sprintf("import-%s", rec), func(ctx context.Context, _ scoped.Spawner) error {
					if rec == "invalid" {
						return fmt.Errorf("bad record: %s", rec)
					}
					fmt.Printf("  imported %s\n", rec)
					return nil
				})
			}
		}, scoped.WithPolicy(scoped.Collect))
	})

	// Parent sees the sub-scope's joined error.
	if err != nil {
		taskErrs := scoped.AllTaskErrors(err)
		fmt.Printf("  sub-scope had %d errors (all sub-tasks still ran)\n", len(taskErrs))
	}
	fmt.Println()
}

// spawnScopeIsolation shows that a failing sub-scope doesn't block siblings
// when the parent uses Collect policy.
func spawnScopeIsolation() {
	fmt.Println("=== SpawnScope (Isolation) ===")

	var siblingOK atomic.Bool

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// This sub-scope fails...
		scoped.SpawnScope(sp, "failing-batch", func(sub scoped.Spawner) {
			sub.Spawn("fail", func(ctx context.Context, _ scoped.Spawner) error {
				return fmt.Errorf("batch failed")
			})
		})

		// ...but this sibling still runs to completion.
		sp.Spawn("sibling", func(ctx context.Context, _ scoped.Spawner) error {
			time.Sleep(10 * time.Millisecond)
			siblingOK.Store(true)
			return nil
		})
	}, scoped.WithPolicy(scoped.Collect))

	fmt.Printf("  sibling completed: %v\n", siblingOK.Load())
	fmt.Printf("  scope error: %v\n\n", err)
}

// maxErrorsExample demonstrates WithMaxErrors to cap memory usage in Collect mode.
func maxErrorsExample() {
	fmt.Println("=== WithMaxErrors ===")

	sc, sp := scoped.New(context.Background(),
		scoped.WithPolicy(scoped.Collect),
		scoped.WithMaxErrors(3), // only store the first 3 errors
	)

	// Spawn 10 tasks that all fail.
	for i := range 10 {
		sp.Spawn(fmt.Sprintf("job-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("job %d failed", i)
		})
	}

	err := sc.Wait()

	stored := scoped.AllTaskErrors(err)
	dropped := sc.DroppedErrors()

	fmt.Printf("  stored errors: %d\n", len(stored))
	fmt.Printf("  dropped errors: %d\n", dropped)
	fmt.Printf("  total errors: %d\n\n", len(stored)+dropped)
}

// taskEventExample demonstrates the unified WithOnEvent lifecycle hook.
func taskEventExample() {
	fmt.Println("=== TaskEvent (Unified Observability) ===")

	var mu sync.Mutex
	var log []string

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("fast-task", func(ctx context.Context, _ scoped.Spawner) error {
			return nil
		})
		sp.Spawn("slow-task", func(ctx context.Context, _ scoped.Spawner) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})
		sp.Spawn("bad-task", func(ctx context.Context, _ scoped.Spawner) error {
			return fmt.Errorf("something broke")
		})
	},
		scoped.WithPolicy(scoped.Collect),
		scoped.WithOnEvent(func(e scoped.TaskEvent) {
			mu.Lock()
			defer mu.Unlock()
			switch e.Kind {
			case scoped.EventStarted:
				log = append(log, fmt.Sprintf("[%s] %s", e.Kind, e.Task.Name))
			case scoped.EventDone:
				log = append(log, fmt.Sprintf("[%s]    %s (%v)", e.Kind, e.Task.Name, e.Duration.Round(time.Millisecond)))
			case scoped.EventErrored:
				log = append(log, fmt.Sprintf("[%s] %s: %v", e.Kind, e.Task.Name, e.Err))
			case scoped.EventPanicked:
				log = append(log, fmt.Sprintf("[%s] %s: %v", e.Kind, e.Task.Name, e.Err))
			case scoped.EventCancelled:
				log = append(log, fmt.Sprintf("[%s] %s", e.Kind, e.Task.Name))
			}
		}),
	)
	_ = err

	for _, entry := range log {
		fmt.Printf("  %s\n", entry)
	}
	fmt.Println()
}

// --- chanx examples ---

// sendRecvExample demonstrates context-aware Send, Recv, and their variants.
func sendRecvExample() {
	fmt.Println("=== chanx: Send / Recv ===")

	ch := make(chan int, 1)

	// Context-aware send.
	err := chanx.Send(context.Background(), ch, 42)
	fmt.Printf("  Send: err=%v\n", err)

	// Context-aware receive.
	v, open, err := chanx.Recv(context.Background(), ch)
	fmt.Printf("  Recv: v=%d, open=%v, err=%v\n", v, open, err)

	// TrySend / TryRecv (non-blocking).
	ok := chanx.TrySend(ch, 99)
	fmt.Printf("  TrySend: ok=%v\n", ok)

	val, open2, received := chanx.TryRecv(ch)
	fmt.Printf("  TryRecv: v=%d, open=%v, received=%v\n", val, open2, received)

	// Timeout variants.
	err = chanx.SendTimeout(ch, 100, 50*time.Millisecond)
	fmt.Printf("  SendTimeout: err=%v\n", err)

	tVal, tOpen, tErr := chanx.RecvTimeout(ch, 50*time.Millisecond)
	fmt.Printf("  RecvTimeout: v=%d, open=%v, err=%v\n\n", tVal, tOpen, tErr)
}

// mergeExample demonstrates fan-in: combining multiple channels into one.
func mergeExample() {
	fmt.Println("=== chanx: Merge ===")

	ch1 := make(chan int, 3)
	ch2 := make(chan int, 3)
	ch3 := make(chan int, 3)

	for i := 1; i <= 3; i++ {
		ch1 <- i
		ch2 <- i * 10
		ch3 <- i * 100
	}
	close(ch1)
	close(ch2)
	close(ch3)

	merged := chanx.Merge(context.Background(), ch1, ch2, ch3)

	var values []int
	for v := range merged {
		values = append(values, v)
	}
	fmt.Printf("  merged (order varies): %v\n\n", values)
}

// fanOutExample demonstrates distributing work across multiple channels.
func fanOutExample() {
	fmt.Println("=== chanx: FanOut ===")

	in := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		in <- i
	}
	close(in)

	// Distribute to 3 workers (round-robin).
	outs := chanx.FanOut(context.Background(), in, 3)

	// Must read ALL outputs concurrently to avoid deadlock,
	// because FanOut uses round-robin with small buffers.
	var wg sync.WaitGroup
	results := make([][]int, 3)
	for i, ch := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range ch {
				results[i] = append(results[i], v)
			}
		}()
	}
	wg.Wait()

	for i, items := range results {
		fmt.Printf("  worker %d received: %v\n", i, items)
	}
	fmt.Println()
}

// teeExample demonstrates broadcasting one channel to multiple consumers.
func teeExample() {
	fmt.Println("=== chanx: Tee ===")

	in := make(chan int, 3)
	in <- 10
	in <- 20
	in <- 30
	close(in)

	// Broadcast to 2 consumers.
	outs := chanx.Tee(context.Background(), in, 2)

	// Must read ALL outputs concurrently to avoid deadlock.
	var wg sync.WaitGroup
	results := make([][]int, 2)

	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range outs[i] {
				results[i] = append(results[i], v)
			}
		}()
	}
	wg.Wait()

	fmt.Printf("  consumer 0: %v\n", results[0])
	fmt.Printf("  consumer 1: %v\n\n", results[1])
}

// chanxMapFilterExample demonstrates chanx.Map and chanx.Filter.
func chanxMapFilterExample() {
	fmt.Println("=== chanx: Map + Filter ===")

	in := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		in <- i
	}
	close(in)

	// Filter even numbers, then double them.
	evens := chanx.Filter(context.Background(), in, func(v int) bool {
		return v%2 == 0
	})
	doubled := chanx.Map(context.Background(), evens, func(v int) int {
		return v * 2
	})

	var results []int
	for v := range doubled {
		results = append(results, v)
	}
	fmt.Printf("  even numbers doubled: %v\n\n", results)
}

// throttleExample demonstrates rate-limiting channel values.
func throttleExample() {
	fmt.Println("=== chanx: Throttle ===")

	in := make(chan int, 10)
	for i := 1; i <= 10; i++ {
		in <- i
	}
	close(in)

	// Allow 5 items per 100ms.
	start := time.Now()
	throttled := chanx.Throttle(context.Background(), in, 5, 100*time.Millisecond)

	var count int
	for range throttled {
		count++
	}

	fmt.Printf("  processed %d items in %v\n\n", count, time.Since(start).Round(time.Millisecond))
}

// bufferExample demonstrates time-or-size based batching from a channel.
func bufferExample() {
	fmt.Println("=== chanx: Buffer ===")

	in := make(chan int, 7)
	for i := 1; i <= 7; i++ {
		in <- i
	}
	close(in)

	// Buffer into batches of 3, with 50ms timeout.
	batches := chanx.Buffer(context.Background(), in, 3, 50*time.Millisecond)

	var i int
	for batch := range batches {
		fmt.Printf("  batch %d: %v\n", i, batch)
		i++
	}
	fmt.Println()
}

// firstExample demonstrates racing multiple channels for the first value.
func firstExample() {
	fmt.Println("=== chanx: First ===")

	ch1 := make(chan string, 1)
	ch2 := make(chan string, 1)
	ch3 := make(chan string, 1)

	// Simulate different response times.
	go func() {
		time.Sleep(50 * time.Millisecond)
		ch1 <- "slow"
	}()
	go func() {
		time.Sleep(5 * time.Millisecond)
		ch2 <- "fast"
	}()
	go func() {
		time.Sleep(30 * time.Millisecond)
		ch3 <- "medium"
	}()

	winner := <-chanx.First(context.Background(), ch1, ch2, ch3)
	fmt.Printf("  first response: %q\n\n", winner)
}

// orDoneExample wraps a channel with context cancellation awareness.
func orDoneExample() {
	fmt.Println("=== chanx: OrDone ===")

	in := make(chan int)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Producer sends values slowly.
	go func() {
		for i := 1; ; i++ {
			select {
			case in <- i:
			case <-ctx.Done():
				close(in)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// OrDone will stop when the context is cancelled.
	var received []int
	for v := range chanx.OrDone(ctx, in) {
		received = append(received, v)
	}
	fmt.Printf("  received before timeout: %v\n\n", received)
}

// closableExample demonstrates the Closable channel wrapper.
func closableExample() {
	fmt.Println("=== chanx: Closable ===")

	c := chanx.NewClosable[string](5)

	// Send values.
	_ = c.Send("hello")
	_ = c.Send("world")

	fmt.Printf("  buffered: %d, closed: %v\n", c.Len(), c.IsClosed())

	// Close is idempotent.
	c.Close()
	c.Close() // safe, no panic

	// Send after close returns ErrClosed.
	err := c.Send("after-close")
	fmt.Printf("  send after close: %v\n", err)

	// TrySend after close.
	err = c.TrySend("try-after")
	fmt.Printf("  try send after close: %v\n", err)

	// Existing buffered values can still be read.
	select {
	case v := <-c.Chan():
		fmt.Printf("  read buffered: %q\n", v)
	case <-c.Done():
		fmt.Println("  closed (no data)")
	}

	fmt.Println()
}

// batchSendRecvExample shows SendBatch and RecvBatch utilities.
func batchSendRecvExample() {
	fmt.Println("=== chanx: SendBatch / RecvBatch ===")

	ch := make(chan int, 10)

	// Send a batch of values.
	err := chanx.SendBatch(context.Background(), ch, []int{10, 20, 30, 40, 50})
	fmt.Printf("  SendBatch: err=%v\n", err)

	// Receive a batch.
	values, err := chanx.RecvBatch(context.Background(), ch, 3)
	fmt.Printf("  RecvBatch(3): %v (err=%v)\n", values, err)

	// Receive remaining.
	close(ch)
	rest, err := chanx.RecvBatch(context.Background(), ch, 10)
	fmt.Printf("  RecvBatch(10) after close: %v (err=%v)\n\n", rest, err)
}
