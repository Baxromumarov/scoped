package scoped

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func expectPanicContains(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if !strings.Contains(toString(r), want) {
			t.Fatalf("panic %q does not contain %q", toString(r), want)
		}
	}()
	fn()
}

func toString(v any) string {
	switch x := v.(type) {
	case error:
		return x.Error()
	case string:
		return x
	default:
		return ""
	}
}

func TestStreamValidationPanics(t *testing.T) {
	expectPanicContains(t, "NewStream requires non-nil", func() {
		NewStream[int](nil)
	})

	expectPanicContains(t, "Filter requires non-nil", func() {
		FromSlice([]int{1}).Filter(nil)
	})

	expectPanicContains(t, "Map requires non-nil source", func() {
		Map[int, int](nil, func(ctx context.Context, v int) (int, error) { return v, nil })
	})

	expectPanicContains(t, "Map requires non-nil mapper", func() {
		Map[int, int](FromSlice([]int{1}), nil)
	})

	expectPanicContains(t, "Batch requires non-nil source", func() {
		Batch[int](nil, 1)
	})

	expectPanicContains(t, "Batch requires n > 0", func() {
		Batch(FromSlice([]int{1}), 0)
	})

	expectPanicContains(t, "Take requires n >= 0", func() {
		FromSlice([]int{1}).Take(-1)
	})

	expectPanicContains(t, "Skip requires n >= 0", func() {
		FromSlice([]int{1}).Skip(-1)
	})

	expectPanicContains(t, "Peek requires non-nil", func() {
		FromSlice([]int{1}).Peek(nil)
	})

	expectPanicContains(t, "ForEach requires non-nil", func() {
		_ = FromSlice([]int{1}).ForEach(context.Background(), nil)
	})

	expectPanicContains(t, "ToChanScope requires non-nil", func() {
		var sp Spawner
		FromSlice([]int{1}).ToChanScope(sp)
	})

	expectPanicContains(t, "ParallelMap requires non-nil spawner", func() {
		var sp Spawner
		_ = ParallelMap(context.Background(), sp, FromSlice([]int{1}), StreamOptions{}, func(ctx context.Context, v int) (int, error) {
			return v, nil
		})
	})

	expectPanicContains(t, "ParallelMap requires non-nil source", func() {
		_ = Run(context.Background(), func(sp Spawner) {
			_ = ParallelMap[int, int](context.Background(), sp, nil, StreamOptions{}, func(ctx context.Context, v int) (int, error) {
				return v, nil
			})
		})
	})

	expectPanicContains(t, "ParallelMap requires non-nil mapper", func() {
		_ = Run(context.Background(), func(sp Spawner) {
			_ = ParallelMap[int, int](context.Background(), sp, FromSlice([]int{1}), StreamOptions{}, nil)
		})
	})

	expectPanicContains(t, "non-negative buffer size", func() {
		_ = Run(context.Background(), func(sp Spawner) {
			_ = ParallelMap(context.Background(), sp, FromSlice([]int{1}), StreamOptions{BufferSize: -1}, func(ctx context.Context, v int) (int, error) {
				return v, nil
			})
		})
	})
}

func TestFromChanBranches(t *testing.T) {
	t.Run("nil channel is EOF", func(t *testing.T) {
		s := FromChan[int](nil)
		_, err := s.Next(context.Background())
		if err != io.EOF {
			t.Fatalf("expected EOF, got %v", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ch := make(chan int)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s := FromChan(ch)
		_, err := s.Next(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})
}

func TestBatchErrorAndCountPartial(t *testing.T) {
	sentinel := errors.New("boom")
	i := 0
	s := NewStream(func(ctx context.Context) (int, error) {
		switch i {
		case 0:
			i++
			return 1, nil
		case 1:
			i++
			return 2, nil
		default:
			return 0, sentinel
		}
	})

	b := Batch(s, 10)
	_, err := b.Next(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected %v, got %v", sentinel, err)
	}

	i = 0
	s = NewStream(func(ctx context.Context) (int, error) {
		switch i {
		case 0:
			i++
			return 1, nil
		case 1:
			i++
			return 2, nil
		default:
			return 0, sentinel
		}
	})
	count, err := s.Count(context.Background())
	if count != 2 {
		t.Fatalf("expected count=2, got %d", count)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected %v, got %v", sentinel, err)
	}
}

func TestStopPropagationAndStopOnce(t *testing.T) {
	var stops atomic.Int32
	base := &Stream[int]{
		next: func(ctx context.Context) (int, error) { return 1, nil },
		stop: func() { stops.Add(1) },
	}

	got, err := base.Take(0).ToSlice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
	if stops.Load() != 1 {
		t.Fatalf("expected stop once, got %d", stops.Load())
	}

	stops.Store(0)
	sentinel := errors.New("stream err")
	base = &Stream[int]{
		next: func(ctx context.Context) (int, error) { return 0, sentinel },
		stop: func() { stops.Add(1) },
	}
	_, err = base.ToSlice(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected %v, got %v", sentinel, err)
	}
	if stops.Load() != 1 {
		t.Fatalf("expected stop once, got %d", stops.Load())
	}
}

func TestToChanBranches(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		out, errCh := FromSlice([]int{1, 2, 3}).ToChan(context.Background())
		var got []int
		for v := range out {
			got = append(got, v)
		}
		if !reflect.DeepEqual(got, []int{1, 2, 3}) {
			t.Fatalf("unexpected values: %v", got)
		}
		if err := <-errCh; err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("source error", func(t *testing.T) {
		sentinel := errors.New("boom")
		s := NewStream(func(ctx context.Context) (int, error) {
			return 0, sentinel
		})
		out, errCh := s.ToChan(context.Background())
		if _, ok := <-out; ok {
			t.Fatal("expected closed output channel")
		}
		if err := <-errCh; !errors.Is(err, sentinel) {
			t.Fatalf("expected %v, got %v", sentinel, err)
		}
	})

	t.Run("context cancellation while sending", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := NewStream(func(ctx context.Context) (int, error) {
			return 1, nil
		})
		out, errCh := s.ToChan(ctx)
		cancel()
		if err := <-errCh; !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
		if _, ok := <-out; ok {
			t.Fatal("expected closed output channel")
		}
	})
}

func TestToChanScopeBranches(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var (
			got    []int
			chErr  error
			runErr error
		)
		runErr = Run(context.Background(), func(sp Spawner) {
			out, errCh := FromSlice([]int{1, 2, 3}).ToChanScope(sp)
			for v := range out {
				got = append(got, v)
			}
			chErr = <-errCh
		})
		if runErr != nil {
			t.Fatalf("unexpected run error: %v", runErr)
		}
		if chErr != nil {
			t.Fatalf("unexpected channel error: %v", chErr)
		}
		if !reflect.DeepEqual(got, []int{1, 2, 3}) {
			t.Fatalf("unexpected values: %v", got)
		}
	})

	t.Run("source error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		var chErr error
		runErr := Run(context.Background(), func(sp Spawner) {
			out, errCh := NewStream(func(ctx context.Context) (int, error) {
				return 0, sentinel
			}).ToChanScope(sp)
			for range out {
			}
			chErr = <-errCh
		})
		if !errors.Is(chErr, sentinel) {
			t.Fatalf("expected channel error %v, got %v", sentinel, chErr)
		}
		if !errors.Is(runErr, sentinel) {
			t.Fatalf("expected run error %v, got %v", sentinel, runErr)
		}
	})
}

func TestParallelMapReadyForProductionBranches(t *testing.T) {
	t.Run("respects external cancellation context", func(t *testing.T) {
		extCtx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)

		go func() {
			var innerErr error
			runErr := Run(context.Background(), func(sp Spawner) {
				pm := ParallelMap(extCtx, sp, FromSlice([]int{1, 2, 3, 4, 5}), StreamOptions{MaxWorkers: 2}, func(ctx context.Context, v int) (int, error) {
					<-ctx.Done()
					return 0, ctx.Err()
				})
				_, innerErr = pm.ToSlice(context.Background())
			})
			if innerErr != nil {
				done <- innerErr
				return
			}
			done <- runErr
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context canceled, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("parallel map cancellation test timed out")
		}
	})

	t.Run("take cancels upstream and does not hang", func(t *testing.T) {
		done := make(chan error, 1)

		go func() {
			var innerErr error
			runErr := Run(context.Background(), func(sp Spawner) {
				items := make([]int, 200)
				for i := range items {
					items[i] = i
				}
				pm := ParallelMap(context.Background(), sp, FromSlice(items), StreamOptions{MaxWorkers: 4}, func(ctx context.Context, v int) (int, error) {
					time.Sleep(500 * time.Microsecond)
					return v, nil
				})
				got, err := pm.Take(1).ToSlice(context.Background())
				if err != nil {
					innerErr = err
					return
				}
				if len(got) != 1 {
					innerErr = errors.New("expected exactly one result")
				}
			})

			if innerErr != nil {
				done <- innerErr
				return
			}
			done <- runErr
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("parallel map with take timed out (possible deadlock)")
		}
	})

	t.Run("source error is returned via stream error", func(t *testing.T) {
		sentinel := errors.New("source boom")
		state := 0
		src := NewStream(func(ctx context.Context) (int, error) {
			switch state {
			case 0:
				state++
				return 1, nil
			case 1:
				state++
				return 2, nil
			default:
				return 0, sentinel
			}
		})

		var (
			got      []int
			innerErr error
		)
		runErr := Run(context.Background(), func(sp Spawner) {
			pm := ParallelMap(context.Background(), sp, src, StreamOptions{MaxWorkers: 2}, func(ctx context.Context, v int) (int, error) {
				return v * 10, nil
			})
			got, innerErr = pm.ToSlice(context.Background())
		})
		if runErr != nil {
			t.Fatalf("unexpected run error: %v", runErr)
		}
		if !errors.Is(innerErr, sentinel) {
			t.Fatalf("expected inner error %v, got %v", sentinel, innerErr)
		}
		if len(got) == 0 {
			t.Fatalf("expected partial results, got %v", got)
		}
	})
}

func TestMakeParallelNextBranches(t *testing.T) {
	t.Run("unordered closed channel returns EOF", func(t *testing.T) {
		out := &Stream[int]{}
		ch := make(chan indexedResult[int])
		close(ch)
		next := makeParallelNext(out, StreamOptions{}, ch)
		_, err := next(context.Background())
		if err != io.EOF {
			t.Fatalf("expected EOF, got %v", err)
		}
	})

	t.Run("context canceled while waiting", func(t *testing.T) {
		out := &Stream[int]{}
		ch := make(chan indexedResult[int])
		next := makeParallelNext(out, StreamOptions{}, ch)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := next(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("ordered gap returns ErrStreamGap", func(t *testing.T) {
		out := &Stream[int]{}
		ch := make(chan indexedResult[int], 1)
		ch <- indexedResult[int]{idx: 1, val: 10}
		close(ch)
		next := makeParallelNext(out, StreamOptions{Ordered: true}, ch)
		_, err := next(context.Background())
		if !errors.Is(err, ErrStreamGap) {
			t.Fatalf("expected ErrStreamGap, got %v", err)
		}
		if !errors.Is(out.Err(), ErrStreamGap) {
			t.Fatalf("expected stream ErrStreamGap, got %v", out.Err())
		}
	})

	t.Run("ordered gap returns embedded error when available", func(t *testing.T) {
		sentinel := errors.New("worker boom")
		out := &Stream[int]{}
		ch := make(chan indexedResult[int], 1)
		ch <- indexedResult[int]{idx: 2, err: sentinel}
		close(ch)
		next := makeParallelNext(out, StreamOptions{Ordered: true}, ch)
		_, err := next(context.Background())
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected %v, got %v", sentinel, err)
		}
		if !errors.Is(out.Err(), sentinel) {
			t.Fatalf("expected stream error %v, got %v", sentinel, out.Err())
		}
	})
}
