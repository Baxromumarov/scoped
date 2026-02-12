package scoped

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestForEachSlice(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		err := ForEachSlice(context.Background(), []int{}, func(ctx context.Context, item int) error {
			return nil
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("single item success", func(t *testing.T) {
		called := false
		err := ForEachSlice(context.Background(), []int{1}, func(ctx context.Context, item int) error {
			called = true
			if item != 1 {
				t.Errorf("expected 1, got %d", item)
			}
			return nil
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if !called {
			t.Error("function not called")
		}
	})

	t.Run("multiple items success", func(t *testing.T) {
		var mu sync.Mutex
		var called []int
		err := ForEachSlice(context.Background(), []int{1, 2, 3}, func(ctx context.Context, item int) error {
			mu.Lock()
			called = append(called, item)
			mu.Unlock()
			return nil
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		mu.Lock()
		count := len(called)
		mu.Unlock()
		if count != 3 {
			t.Errorf("expected 3 calls, got %d", count)
		}
		// Order not guaranteed, but since concurrent, check all present
		mu.Lock()
		sum := 0
		for _, v := range called {
			sum += v
		}
		mu.Unlock()
		if sum != 6 {
			t.Errorf("expected sum 6, got %d", sum)
		}
	})

	t.Run("error with FailFast", func(t *testing.T) {
		var called atomic.Int32
		err := ForEachSlice(context.Background(), []int{1, 2, 3}, func(ctx context.Context, item int) error {
			called.Add(1)
			if item == 2 {
				return errors.New("error on 2")
			}
			return nil
		})
		if err == nil {
			t.Error("expected error")
		}
		if err.Error() != "error on 2" {
			t.Errorf("expected 'error on 2', got %v", err)
		}
		// With FailFast, may not call all
		if got := called.Load(); got < 1 || got > 3 {
			t.Errorf("unexpected call count %d", got)
		}
	})

	t.Run("error with Collect", func(t *testing.T) {
		var called atomic.Int32
		err := ForEachSlice(context.Background(), []int{1, 2, 3}, func(ctx context.Context, item int) error {
			called.Add(1)
			if item == 2 {
				return errors.New("error on 2")
			}
			return nil
		}, WithPolicy(Collect))
		if err == nil {
			t.Error("expected error")
		}
		// Should be joined
		if !strings.Contains(err.Error(), "error on 2") {
			t.Errorf("expected error containing 'error on 2', got %v", err)
		}
		if got := called.Load(); got != 3 {
			t.Errorf("expected 3 calls, got %d", got)
		}
	})

	t.Run("context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ForEachSlice(ctx, []int{1, 2}, func(ctx context.Context, item int) error {
			return nil
		})
		if err == nil {
			t.Error("expected error due to cancel")
		}
	})

	t.Run("panic without WithPanicAsError", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic")
			}
		}()
		err := ForEachSlice(context.Background(), []int{1}, func(ctx context.Context, item int) error {
			panic("test panic")
		})
		assert.NoError(t, err)
	})

	t.Run("panic with WithPanicAsError", func(t *testing.T) {
		err := ForEachSlice(context.Background(), []int{1}, func(ctx context.Context, item int) error {
			panic("test panic")
		}, WithPanicAsError())
		if err == nil {
			t.Error("expected error")
		}
		var pe *PanicError
		if !errors.As(err, &pe) {
			t.Errorf("expected PanicError, got %T", err)
		}
		if pe.Value != "test panic" {
			t.Errorf("expected panic value 'test panic', got %v", pe.Value)
		}
	})

	t.Run("with limit", func(t *testing.T) {
		// Hard to test directly, but ensure no error
		err := ForEachSlice(context.Background(), []int{1, 2, 3}, func(ctx context.Context, item int) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		}, WithLimit(1))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("with hooks", func(t *testing.T) {
		startCalled := 0
		doneCalled := 0
		err := ForEachSlice(context.Background(), []int{1}, func(ctx context.Context, item int) error {
			return nil
		}, WithOnStart(func(TaskInfo) { startCalled++ }), WithOnDone(func(TaskInfo, error, time.Duration) { doneCalled++ }))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if startCalled != 1 {
			t.Errorf("expected 1 start call, got %d", startCalled)
		}
		if doneCalled != 1 {
			t.Errorf("expected 1 done call, got %d", doneCalled)
		}
	})
}

func TestMapSlice(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		results, err := MapSlice(context.Background(), []int{}, func(ctx context.Context, item int) (int, error) {
			return item * 2, nil
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected empty results, got %v", results)
		}
	})

	t.Run("single item success", func(t *testing.T) {
		results, err := MapSlice(context.Background(), []int{3}, func(ctx context.Context, item int) (int, error) {
			return item * 2, nil
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(results) != 1 || results[0].Value != 6 || results[0].Err != nil {
			t.Errorf("expected [6], got %v", results)
		}
	})

	t.Run("multiple items success", func(t *testing.T) {
		results, err := MapSlice(context.Background(), []int{1, 2, 3}, func(ctx context.Context, item int) (int, error) {
			return item * 2, nil
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		expected := []int{2, 4, 6}
		if len(results) != len(expected) {
			t.Errorf("expected %v, got %v", expected, results)
		}
		for i := range results {
			if results[i].Err != nil {
				t.Errorf("at %d, expected nil error, got %v", i, results[i].Err)
			}
			if results[i].Value != expected[i] {
				t.Errorf("at %d, expected %d, got %d", i, expected[i], results[i].Value)
			}
		}
	})

	t.Run("error with FailFast", func(t *testing.T) {
		results, err := MapSlice(context.Background(), []int{1, 2, 3}, func(ctx context.Context, item int) (int, error) {
			if item == 2 {
				return 0, errors.New("error on 2")
			}
			return item * 2, nil
		})
		if err == nil {
			t.Error("expected error")
		}
		if results != nil {
			t.Errorf("expected nil results, got %v", results)
		}
	})

	t.Run("error with Collect partial results", func(t *testing.T) {
		results, err := MapSlice(
			context.Background(),
			[]int{1, 2, 3},
			func(ctx context.Context, item int) (int, error) {
				if item == 2 {
					return 0, errors.New("error on 2")
				}
				return item * 2, nil
			},
			WithPolicy(Collect),
		)
		if err != nil {
			t.Errorf("expected no outer error, got %v", err)
		}

		if results == nil {
			t.Error("expected partial results, got nil")
		} else {
			expected := []int{2, 0, 6}
			if len(results) != len(expected) {
				t.Errorf("expected %v, got %v", expected, results)
			}
			for i := range results {
				if results[i].Value != expected[i] {
					t.Errorf("at %d, expected %d, got %d", i, expected[i], results[i].Value)
				}
			}

			if results[0].Err != nil {
				t.Errorf("expected nil error at index 0, got %v", results[0].Err)
			}
			if results[1].Err == nil || results[1].Err.Error() != "error on 2" {
				t.Errorf("expected item error at index 1, got %v", results[1].Err)
			}
			if results[2].Err != nil {
				t.Errorf("expected nil error at index 2, got %v", results[2].Err)
			}
		}
	})

	t.Run("collect with infrastructure error still returns partial results", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		results, err := MapSlice(ctx, []int{1, 2}, func(ctx context.Context, item int) (int, error) {
			return item, nil
		}, WithPolicy(Collect))
		if err == nil {
			t.Error("expected outer infrastructure error")
		}
		if results == nil {
			t.Error("expected partial results slice, got nil")
		} else if len(results) != 2 {
			t.Errorf("expected len 2, got %d", len(results))
		}
	})

	t.Run("context cancel with FailFast", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		results, err := MapSlice(ctx, []int{1, 2}, func(ctx context.Context, item int) (int, error) {
			return item, nil
		})
		if err == nil {
			t.Error("expected error")
		}
		if results != nil {
			t.Errorf("expected nil results, got %v", results)
		}
	})

	t.Run("panic with WithPanicAsError", func(t *testing.T) {
		results, err := MapSlice(context.Background(), []int{1}, func(ctx context.Context, item int) (int, error) {
			panic("test panic")
		}, WithPanicAsError())
		if err == nil {
			t.Error("expected error")
		}
		if results != nil {
			t.Errorf("expected nil results, got %v", results)
		}
		var pe *PanicError
		if !errors.As(err, &pe) {
			t.Errorf("expected PanicError, got %T", err)
		}
	})
}

func TestAtomicError(t *testing.T) {
	t.Run("store and load nil", func(t *testing.T) {
		var ae atomicError
		ae.Store(nil)
		if ae.Load() != nil {
			t.Error("expected nil")
		}
	})

	t.Run("store and load error", func(t *testing.T) {
		var ae atomicError
		err := errors.New("test")
		ae.Store(err)
		if ae.Load() != err {
			t.Error("expected stored error")
		}
	})

	t.Run("store error then nil", func(t *testing.T) {
		var ae atomicError
		err := errors.New("test")
		ae.Store(err)
		ae.Store(nil)
		if ae.Load() != nil {
			t.Error("expected nil after storing nil")
		}
	})

	t.Run("load before store", func(t *testing.T) {
		var ae atomicError
		if ae.Load() != nil {
			t.Error("expected nil initially")
		}
	})
}
