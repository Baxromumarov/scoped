package scoped_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baxromumarov/scoped"
)

// Chaos tests

func TestNestedTasks(t *testing.T) {
	type task struct {
		name string
		idx  int
	}

	var tasks = []task{
		{name: "a", idx: 1},
		{name: "b", idx: 2},
		{name: "c", idx: 3},
		{name: "d", idx: 4},
		{name: "e", idx: 5},
		{name: "f", idx: 6},
		{name: "g", idx: 7},
		{name: "h", idx: 8},
	}

	err := scoped.Run(
		context.Background(),
		func(sp scoped.Spawner) {
			for _, tk := range tasks {
				sp.Spawn(
					tk.name,
					func(ctx context.Context, sp scoped.Spawner) error {
						if tk.idx == 5 {
							panic("just test panic")
						}
						if tk.idx%2 == 0 {
							sp.Spawn(
								fmt.Sprintf("%s-child", tk.name),
								func(ctx context.Context, _ scoped.Spawner) error {
									time.Sleep(10 * time.Millisecond)
									return nil
								},
							)
						}

						if tk.idx == 3 {
							return errors.New("just test error")
						}

						return nil
					},
				)
			}
		},
		scoped.WithPanicAsError(),
		scoped.WithPolicy(scoped.FailFast),
	)

	if err == nil {
		t.Fatal("expected error from panic or task failure, got nil")
	}

}

func TestRunAllSuccess(t *testing.T) {
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < 10; i++ {
			sp.Spawn("task", func(ctx context.Context, _ scoped.Spawner) error {
				count.Add(1)
				return nil
			})
		}
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := count.Load(); got != 10 {
		t.Fatalf("expected 10 tasks completed, got %d", got)
	}
}

func TestRunEmpty(t *testing.T) {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// spawn nothing
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunSetupPanicStillClosesScope(t *testing.T) {
	var runScope scoped.Spawner

	p := capturePanic(func() {
		_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
			runScope = sp
			panic("setup boom")
		})
	})

	if p != "setup boom" {
		t.Fatalf("expected setup panic value, got %v", p)
	}
	if runScope == nil {
		t.Fatal("expected scope to be captured")
	}

	lateGoPanic := capturePanic(func() {
		runScope.Spawn("late", func(context.Context, scoped.Spawner) error { return nil })
	})
	if lateGoPanic == nil {
		t.Fatal("expected Spawn to panic after Run setup panic cleanup")
	}
}

func TestWithPolicyInvalidPanics(t *testing.T) {
	p := capturePanic(func() {
		_ = scoped.Run(context.Background(), func(scoped.Spawner) {}, scoped.WithPolicy(scoped.Policy(999)))
	})
	if p == nil {
		t.Fatal("expected panic for invalid policy")
	}
}

func TestRunFailFast(t *testing.T) {
	sentinel := errors.New("task-3 failed")
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < 10; i++ {
			sp.Spawn(fmt.Sprintf("task-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				if i == 3 {
					return sentinel
				}
				// Block until context is cancelled (siblings should be cancelled).
				<-ctx.Done()
				return nil
			})
		}
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestRunFailFastCancelsOthers(t *testing.T) {
	var cancelled atomic.Int32
	started := make(chan struct{}, 5)

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// Long-running tasks
		for i := 0; i < 5; i++ {
			sp.Spawn("worker", func(ctx context.Context, _ scoped.Spawner) error {
				started <- struct{}{} // signal ready
				<-ctx.Done()
				cancelled.Add(1)
				return ctx.Err()
			})
		}

		// Ensure all workers are started
		for i := 0; i < 5; i++ {
			<-started
		}

		// Error task
		sp.Spawn("fail", func(ctx context.Context, _ scoped.Spawner) error {
			return errors.New("boom")
		})
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if got := cancelled.Load(); got != 5 {
		t.Fatalf("expected 5 workers cancelled, got %d", got)
	}
}

func TestRunCollect(t *testing.T) {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < 5; i++ {
			sp.Spawn(fmt.Sprintf("task-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				return fmt.Errorf("error-%d", i)
			})
		}
	}, scoped.WithPolicy(scoped.Collect))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	out := scoped.AllTaskErrors(err)

	if len(out) != 5 {
		t.Fatalf("expected 5 errors, got %d", len(out))
	}
}

func TestRunCollectNoCancellation(t *testing.T) {
	var completed atomic.Int32
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		// One task errors...
		sp.Spawn("fail", func(ctx context.Context, _ scoped.Spawner) error {
			return errors.New("fail")
		})
		// ...but siblings should NOT be cancelled.
		for i := 0; i < 3; i++ {
			sp.Spawn("worker", func(ctx context.Context, _ scoped.Spawner) error {
				time.Sleep(20 * time.Millisecond)
				completed.Add(1)
				return nil
			})
		}
	}, scoped.WithPolicy(scoped.Collect))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := completed.Load(); got != 3 {
		t.Fatalf("expected 3 workers completed, got %d", got)
	}
}

func TestRunPanicReRaised(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		pe, ok := r.(*scoped.PanicError)
		if !ok {
			t.Fatalf("expected *PanicError, got %T", r)
		}
		if pe.Value != "boom" {
			t.Fatalf("expected panic value 'boom', got %v", pe.Value)
		}
		if pe.Stack == "" {
			t.Fatal("expected non-empty stack trace")
		}
	}()

	_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("panicker", func(ctx context.Context, _ scoped.Spawner) error {
			panic("boom")
		})
	})
}

func TestRunPanicAsError(t *testing.T) {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("panicker", func(ctx context.Context, _ scoped.Spawner) error {
			panic("boom")
		})
	}, scoped.WithPanicAsError())
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
	var pe *scoped.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PanicError, got %T: %v", err, err)
	}
	if pe.Value != "boom" {
		t.Fatalf("expected 'boom', got %v", pe.Value)
	}
}

func TestRunMultiplePanicsFirstReRaised(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		_, ok := r.(*scoped.PanicError)
		if !ok {
			t.Fatalf("expected *PanicError, got %T", r)
		}
	}()

	_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < 5; i++ {
			sp.Spawn(fmt.Sprintf("p-%d", i), func(ctx context.Context, _ scoped.Spawner) error {
				panic(fmt.Sprintf("panic-%d", i))
			})
		}
	})
}

func TestRunLimit(t *testing.T) {
	const limit = 3
	var active atomic.Int32
	var maxActive atomic.Int32

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < 20; i++ {
			sp.Spawn("worker", func(ctx context.Context, _ scoped.Spawner) error {
				cur := active.Add(1)
				// Record the high-water mark.
				for {
					old := maxActive.Load()
					if cur <= old || maxActive.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				active.Add(-1)
				return nil
			})
		}
	}, scoped.WithLimit(limit))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := maxActive.Load(); got > int32(limit) {
		t.Fatalf("max active goroutines %d exceeded limit %d", got, limit)
	}
}

func TestRunLimitContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	blockerStarted := make(chan struct{})
	blockerDone := make(chan struct{})
	var waiterRan atomic.Bool

	err := scoped.Run(ctx, func(sp scoped.Spawner) {

		// Fill the single semaphore slot with a blocker.
		sp.Spawn("blocker", func(ctx context.Context, _ scoped.Spawner) error {
			close(blockerStarted)
			<-blockerDone
			return nil
		})

		// Ensure blocker has acquired the slot.
		<-blockerStarted

		// This waiter will block on semaphore acquisition.
		sp.Spawn("waiter", func(ctx context.Context, _ scoped.Spawner) error {
			waiterRan.Store(true)
			return nil
		})

		// Give the waiter goroutine time to reach the semaphore select.
		time.Sleep(20 * time.Millisecond)

		// Cancel context while the slot is still held — only ctx.Done() fires.
		cancel()

		// Give the waiter time to observe cancellation and exit.
		time.Sleep(10 * time.Millisecond)

		// Now release the blocker so Wait() can return.
		close(blockerDone)

	}, scoped.WithLimit(1))
	_ = err

	if waiterRan.Load() {
		t.Fatal("waiter should not have run — semaphore wait was cancelled")
	}
}

func TestRunExternalCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var sawCancel atomic.Bool
	err := scoped.Run(ctx, func(sp scoped.Spawner) {
		sp.Spawn("long", func(ctx context.Context, _ scoped.Spawner) error {
			<-ctx.Done()
			sawCancel.Store(true)
			return ctx.Err()
		})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if !sawCancel.Load() {
		t.Fatal("task did not observe cancellation")
	}
}

func TestRunSubTasks(t *testing.T) {
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("parent", func(ctx context.Context, sp scoped.Spawner) error {
			count.Add(1)
			// Spawn children from within a task.
			for i := 0; i < 3; i++ {
				sp.Spawn("child", func(ctx context.Context, _ scoped.Spawner) error {
					count.Add(1)
					return nil
				})
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := count.Load(); got != 4 {
		t.Fatalf("expected 4 (1 parent + 3 children), got %d", got)
	}
}

func TestContextPropagation(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "hello")
	err := scoped.Run(ctx, func(sp scoped.Spawner) {
		sp.Spawn("task", func(ctx context.Context, _ scoped.Spawner) error {
			if got := ctx.Value(key{}); got != "hello" {
				return fmt.Errorf("expected 'hello', got %v", got)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHooks(t *testing.T) {
	var (
		started  []string
		finished []string
		mu       sync.Mutex
	)

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("alpha", func(ctx context.Context, _ scoped.Spawner) error {
			time.Sleep(5 * time.Millisecond)
			return nil
		})
		sp.Spawn("beta", func(ctx context.Context, _ scoped.Spawner) error {
			return nil
		})
	},
		scoped.WithOnStart(func(info scoped.TaskInfo) {
			mu.Lock()
			started = append(started, info.Name)
			mu.Unlock()
		}),
		scoped.WithOnDone(func(info scoped.TaskInfo, err error, d time.Duration) {
			mu.Lock()
			finished = append(finished, info.Name)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("expected 2 starts, got %d", len(started))
	}
	if len(finished) != 2 {
		t.Fatalf("expected 2 finishes, got %d", len(finished))
	}
}

func TestHookPanicBehavior(t *testing.T) {
	mode := os.Getenv("SCOPED_HOOK_PANIC_MODE")
	if mode != "" {
		switch mode {
		case "start":
			_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
				sp.Spawn("task", func(ctx context.Context, _ scoped.Spawner) error { return nil })
			}, scoped.WithOnStart(func(scoped.TaskInfo) {
				panic("hook panic start")
			}))
		case "done":
			_ = scoped.Run(context.Background(), func(sp scoped.Spawner) {
				sp.Spawn("task", func(ctx context.Context, _ scoped.Spawner) error { return nil })
			}, scoped.WithOnDone(func(scoped.TaskInfo, error, time.Duration) {
				panic("hook panic done")
			}))
		default:
			panic("unknown hook panic mode")
		}
		return
	}

	cases := []struct {
		name string
		mode string
		msg  string
	}{
		{name: "on_start", mode: "start", msg: "hook panic start"},
		{name: "on_done", mode: "done", msg: "hook panic done"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHookPanicBehavior$")
			cmd.Env = append(os.Environ(), "SCOPED_HOOK_PANIC_MODE="+tc.mode)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("expected subprocess to fail due to hook panic")
			}
			if !bytes.Contains(out, []byte(tc.msg)) {
				t.Fatalf("expected panic output to contain %q, got:\n%s", tc.msg, out)
			}
		})
	}
}

func TestGoResult(t *testing.T) {
	var r *scoped.Result[int]
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		r = scoped.SpawnResult(sp, "compute", func(ctx context.Context) (int, error) {
			return 42, nil
		})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val, err := r.Wait()
	if err != nil {
		t.Fatalf("result error: %v", err)
	}
	if val != 42 {
		t.Fatalf("expected 42, got %d", val)
	}
}

func TestGoResultError(t *testing.T) {
	sentinel := errors.New("compute failed")
	var r *scoped.Result[int]
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		r = scoped.SpawnResult(sp, "compute", func(ctx context.Context) (int, error) {
			return 0, sentinel
		})
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	_, rerr := r.Wait()
	if !errors.Is(rerr, sentinel) {
		t.Fatalf("expected sentinel from result, got %v", rerr)
	}
}

func TestGoResultContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var r *scoped.Result[int]
	err := scoped.Run(ctx, func(sp scoped.Spawner) {
		r = scoped.SpawnResult(sp, "slow", func(ctx context.Context) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		})
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
	})
	_ = err
	_, rerr := r.Wait()
	if rerr == nil {
		t.Fatal("expected error from cancelled result")
	}
}

func TestGoResultPanic(t *testing.T) {
	var r *scoped.Result[int]
	err := scoped.Run(
		context.Background(),
		func(sp scoped.Spawner) {
			r = scoped.SpawnResult(sp,
				"compute",
				func(ctx context.Context) (int, error) {
					panic("boom")
				},
			)
		},
		scoped.WithPanicAsError(),
	)

	if err == nil {
		t.Fatal("expected error from panic")
	}
	var pe *scoped.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PanicError, got %T: %v", err, err)
	}

	_, rerr := r.Wait()
	if !errors.As(rerr, &pe) {
		t.Fatalf("expected PanicError from result, got %T: %v", rerr, rerr)
	}
}

func TestGoResultPanicWithoutPanicAsError(t *testing.T) {
	var r *scoped.Result[int]
	p := capturePanic(func() {
		_ = scoped.Run(
			context.Background(),
			func(sp scoped.Spawner) {
				r = scoped.SpawnResult(sp, "compute", func(ctx context.Context) (int, error) {
					panic("boom")
				})
			},
		)
	})
	if p == nil {
		t.Fatal("expected panic")
	}

	_, rerr := r.Wait()
	var pe *scoped.PanicError
	if !errors.As(rerr, &pe) {
		t.Fatalf("expected PanicError from result, got %T: %v", rerr, rerr)
	}
	if pe.Value != "boom" {
		t.Fatalf("expected panic value boom, got %v", pe.Value)
	}
}

func TestForEach(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var sum atomic.Int64
	err := scoped.ForEachSlice(context.Background(), items, func(ctx context.Context, item int) error {
		sum.Add(int64(item))
		return nil
	}, scoped.WithLimit(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sum.Load(); got != 15 {
		t.Fatalf("expected sum 15, got %d", got)
	}
}

func TestForEachError(t *testing.T) {
	items := []int{1, 2, 3}
	err := scoped.ForEachSlice(context.Background(), items, func(ctx context.Context, item int) error {
		if item == 2 {
			return errors.New("bad item")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMap(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	results, err := scoped.MapSlice(context.Background(), items, func(ctx context.Context, item int) (int, error) {
		return item * 2, nil
	}, scoped.WithLimit(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{2, 4, 6, 8, 10}
	for i := range results {
		if results[i].Err != nil {
			t.Fatalf("results[%d] err = %v, want nil", i, results[i].Err)
		}
		if results[i].Value != expected[i] {
			t.Fatalf("results[%d] = %d, want %d", i, results[i].Value, expected[i])
		}
	}
}

func TestMapError(t *testing.T) {
	items := []int{1, 2, 3}
	_, err := scoped.MapSlice(context.Background(), items, func(ctx context.Context, item int) (int, error) {
		if item == 2 {
			return 0, errors.New("bad")
		}
		return item, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const n = 10000
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < n; i++ {
			sp.Spawn("", func(ctx context.Context, _ scoped.Spawner) error {
				count.Add(1)
				return nil
			})
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := count.Load(); got != n {
		t.Fatalf("expected %d, got %d", n, got)
	}
}

func TestRunStressWithLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const n = 10000
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		for i := 0; i < n; i++ {
			sp.Spawn("", func(ctx context.Context, _ scoped.Spawner) error {
				count.Add(1)
				return nil
			})
		}
	}, scoped.WithLimit(50))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := count.Load(); got != n {
		t.Fatalf("expected %d, got %d", n, got)
	}
}

func capturePanic(fn func()) (p any) {
	defer func() {
		p = recover()
	}()
	fn()
	return nil
}

func TestRun_BasicTasks(t *testing.T) {
	var ran atomic.Int32

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("task", func(ctx context.Context, sp scoped.Spawner) error {
			ran.Add(1)
			return nil
		})
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran.Load() != 1 {
		t.Fatalf("expected 1 task, got %d", ran.Load())
	}
}

func TestRun_SubTasks(t *testing.T) {
	var count atomic.Int32

	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		sp.Spawn("parent", func(ctx context.Context, sp scoped.Spawner) error {
			count.Add(1)

			for i := 0; i < 3; i++ {
				sp.Spawn("child", func(ctx context.Context, _ scoped.Spawner) error {
					count.Add(1)
					return nil
				})
			}
			return nil
		})
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := count.Load(); got != 4 {
		t.Fatalf("expected 4 (1 parent + 3 children), got %d", got)
	}
}

func TestScopeObservability(t *testing.T) {
	sc, sp := scoped.New(context.Background())

	if sc.TotalSpawned() != 0 {
		t.Fatalf("expected 0 total spawned, got %d", sc.TotalSpawned())
	}

	started := make(chan struct{})
	release := make(chan struct{})
	sp.Spawn("blocker", func(ctx context.Context, _ scoped.Spawner) error {
		close(started)
		<-release
		return nil
	})

	<-started
	if sc.TotalSpawned() != 1 {
		t.Fatalf("expected 1 total spawned, got %d", sc.TotalSpawned())
	}
	if sc.ActiveTasks() != 1 {
		t.Fatalf("expected 1 active task, got %d", sc.ActiveTasks())
	}

	close(release)
	if err := sc.Wait(); err != nil {
		t.Fatal(err)
	}
	if sc.ActiveTasks() != 0 {
		t.Fatalf("expected 0 active tasks after wait, got %d", sc.ActiveTasks())
	}
	if sc.TotalSpawned() != 1 {
		t.Fatalf("expected 1 total spawned after wait, got %d", sc.TotalSpawned())
	}
}

func TestSpawnTimeout(t *testing.T) {
	err := scoped.Run(context.Background(), func(sp scoped.Spawner) {
		scoped.SpawnTimeout(sp, "slow", 10*time.Millisecond, func(ctx context.Context, _ scoped.Spawner) error {
			<-ctx.Done()
			return ctx.Err()
		})
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}
