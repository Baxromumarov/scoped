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

// --- Run basics ---

func TestRunAllSuccess(t *testing.T) {
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 10; i++ {
			s.Go("task", func(ctx context.Context) error {
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
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		// spawn nothing
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunSetupPanicStillClosesScope(t *testing.T) {
	var runScope *scoped.Scope

	p := capturePanic(func() {
		_ = scoped.Run(context.Background(), func(s *scoped.Scope) {
			runScope = s
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
		runScope.Go("late", func(context.Context) error { return nil })
	})
	if lateGoPanic == nil {
		t.Fatal("expected Go to panic after Run setup panic cleanup")
	}
}

func TestWithPolicyInvalidPanics(t *testing.T) {
	p := capturePanic(func() {
		_, _ = scoped.New(context.Background(), scoped.WithPolicy(scoped.Policy(999)))
	})
	if p == nil {
		t.Fatal("expected panic for invalid policy")
	}
}

// --- FailFast ---

func TestRunFailFast(t *testing.T) {
	sentinel := errors.New("task-3 failed")
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 10; i++ {
			i := i
			s.Go(fmt.Sprintf("task-%d", i), func(ctx context.Context) error {
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
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		// Error task.
		s.Go("fail", func(ctx context.Context) error {
			return errors.New("boom")
		})
		// Long-running tasks that should be cancelled.
		for i := 0; i < 5; i++ {
			s.Go("worker", func(ctx context.Context) error {
				<-ctx.Done()
				cancelled.Add(1)
				return nil
			})
		}
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Give goroutines a moment to observe cancellation.
	time.Sleep(10 * time.Millisecond)
	if got := cancelled.Load(); got != 5 {
		t.Fatalf("expected 5 workers cancelled, got %d", got)
	}
}

// --- Collect ---

func TestRunCollect(t *testing.T) {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 5; i++ {
			i := i
			s.Go(fmt.Sprintf("task-%d", i), func(ctx context.Context) error {
				return fmt.Errorf("error-%d", i)
			})
		}
	}, scoped.WithPolicy(scoped.Collect))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// errors.Join produces an error that Unwrap() returns []error.
	var joinErr interface{ Unwrap() []error }
	if !errors.As(err, &joinErr) {
		t.Fatalf("expected joined error, got %T: %v", err, err)
	}
	errs := joinErr.Unwrap()
	if len(errs) != 5 {
		t.Fatalf("expected 5 errors, got %d", len(errs))
	}
}

func TestRunCollectNoCancellation(t *testing.T) {
	var completed atomic.Int32
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		// One task errors...
		s.Go("fail", func(ctx context.Context) error {
			return errors.New("fail")
		})
		// ...but siblings should NOT be cancelled.
		for i := 0; i < 3; i++ {
			s.Go("worker", func(ctx context.Context) error {
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

// --- Panics ---

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

	_ = scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("panicker", func(ctx context.Context) error {
			panic("boom")
		})
	})
}

func TestRunPanicAsError(t *testing.T) {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("panicker", func(ctx context.Context) error {
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

	_ = scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 5; i++ {
			i := i
			s.Go(fmt.Sprintf("p-%d", i), func(ctx context.Context) error {
				panic(fmt.Sprintf("panic-%d", i))
			})
		}
	})
}

// --- Bounded concurrency ---

func TestRunLimit(t *testing.T) {
	const limit = 3
	var active atomic.Int32
	var maxActive atomic.Int32

	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < 20; i++ {
			s.Go("worker", func(ctx context.Context) error {
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

	s, _ := scoped.New(ctx, scoped.WithLimit(1))

	// Fill the single semaphore slot with a blocker.
	s.Go("blocker", func(ctx context.Context) error {
		close(blockerStarted)
		<-blockerDone
		return nil
	})

	// Ensure blocker has acquired the slot.
	<-blockerStarted

	// This waiter will block on semaphore acquisition.
	s.Go("waiter", func(ctx context.Context) error {
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

	_ = s.Wait()

	if waiterRan.Load() {
		t.Fatal("waiter should not have run — semaphore wait was cancelled")
	}
}

// --- External cancellation ---

func TestRunExternalCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var sawCancel atomic.Bool
	err := scoped.Run(ctx, func(s *scoped.Scope) {
		s.Go("long", func(ctx context.Context) error {
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

// --- Sub-tasks ---

func TestRunSubTasks(t *testing.T) {
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("parent", func(ctx context.Context) error {
			count.Add(1)
			// Spawn children from within a task.
			for i := 0; i < 3; i++ {
				s.Go("child", func(ctx context.Context) error {
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

// --- Scope.Cancel ---

func TestScopeCancel(t *testing.T) {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("task", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		// Explicitly cancel the scope.
		go func() {
			time.Sleep(10 * time.Millisecond)
			s.Cancel(errors.New("manual cancel"))
		}()
	})
	if err == nil {
		t.Fatal("expected error from manual cancel")
	}
}

// --- Wait idempotent ---

func TestWaitIdempotent(t *testing.T) {
	s, _ := scoped.New(context.Background())
	s.Go("task", func(ctx context.Context) error {
		return errors.New("fail")
	})
	err1 := s.Wait()
	err2 := s.Wait()
	if err1 == nil || err2 == nil {
		t.Fatal("expected error on both waits")
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("expected same error, got %q and %q", err1, err2)
	}
}

// --- Go after closed ---

func TestGoAfterClosedPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on Go after closed scope")
		}
	}()

	s, _ := scoped.New(context.Background())
	_ = s.Wait()
	s.Go("late", func(ctx context.Context) error { return nil })
}

// --- Context propagation ---

func TestContextPropagation(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "hello")
	err := scoped.Run(ctx, func(s *scoped.Scope) {
		s.Go("task", func(ctx context.Context) error {
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

// --- Hooks ---

func TestHooks(t *testing.T) {
	var (
		started  []string
		finished []string
		mu       sync.Mutex
	)

	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("alpha", func(ctx context.Context) error {
			time.Sleep(5 * time.Millisecond)
			return nil
		})
		s.Go("beta", func(ctx context.Context) error {
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
			_ = scoped.Run(context.Background(), func(s *scoped.Scope) {
				s.Go("task", func(ctx context.Context) error { return nil })
			}, scoped.WithOnStart(func(scoped.TaskInfo) {
				panic("hook panic start")
			}))
		case "done":
			_ = scoped.Run(context.Background(), func(s *scoped.Scope) {
				s.Go("task", func(ctx context.Context) error { return nil })
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
		tc := tc
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

// --- GoResult ---

func TestGoResult(t *testing.T) {
	var r *scoped.Result[int]
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		r = scoped.GoResult(s, "compute", func(ctx context.Context) (int, error) {
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
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		r = scoped.GoResult(s, "compute", func(ctx context.Context) (int, error) {
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
	err := scoped.Run(ctx, func(s *scoped.Scope) {
		r = scoped.GoResult(s, "slow", func(ctx context.Context) (int, error) {
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

// --- ForEach ---

func TestForEach(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var sum atomic.Int64
	err := scoped.ForEach(context.Background(), items, func(ctx context.Context, item int) error {
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
	err := scoped.ForEach(context.Background(), items, func(ctx context.Context, item int) error {
		if item == 2 {
			return errors.New("bad item")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Map ---

func TestMap(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	results, err := scoped.Map(context.Background(), items, func(ctx context.Context, item int) (int, error) {
		return item * 2, nil
	}, scoped.WithLimit(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{2, 4, 6, 8, 10}
	for i, v := range results {
		if v != expected[i] {
			t.Fatalf("results[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

func TestMapError(t *testing.T) {
	items := []int{1, 2, 3}
	_, err := scoped.Map(context.Background(), items, func(ctx context.Context, item int) (int, error) {
		if item == 2 {
			return 0, errors.New("bad")
		}
		return item, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Stress test ---

func TestRunStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const n = 10000
	var count atomic.Int32
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < n; i++ {
			s.Go("", func(ctx context.Context) error {
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
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		for i := 0; i < n; i++ {
			s.Go("", func(ctx context.Context) error {
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
