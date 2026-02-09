package scoped

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestFromSlice_NextSequence(t *testing.T) {
	s := FromSlice([]int{1, 2})

	ctx := context.Background()

	v, err := s.Next(ctx)
	if err != nil || v != 1 {
		t.Fatalf("got %v, %v; want 1, nil", v, err)
	}

	v, err = s.Next(ctx)
	if err != nil || v != 2 {
		t.Fatalf("got %v, %v; want 2, nil", v, err)
	}

	_, err = s.Next(ctx)
	if err != io.EOF {
		t.Fatalf("got %v; want io.EOF", err)
	}
}

func TestFromSlice(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	s := FromSlice(items)
	res, err := s.ToSlice(context.Background())
	if err != nil {
		t.Fatalf("ToSlice failed: %v", err)
	}
	if !reflect.DeepEqual(res, items) {
		t.Errorf("got %v, want %v", res, items)
	}
}

func TestStreamMap(t *testing.T) {
	items := []int{1, 2, 3}
	s := FromSlice(items)
	ms := Map(s, func(ctx context.Context, v int) (int, error) {
		return v * 2, nil
	})
	res, err := ms.ToSlice(context.Background())
	if err != nil {
		t.Fatalf("ToSlice failed: %v", err)
	}
	want := []int{2, 4, 6}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %v, want %v", res, want)
	}
}

func TestFilter(t *testing.T) {
	items := []int{1, 2, 3, 4}
	s := FromSlice(items).Filter(func(v int) bool {
		return v%2 == 0
	})
	res, err := s.ToSlice(context.Background())
	if err != nil {
		t.Fatalf("ToSlice failed: %v", err)
	}
	want := []int{2, 4}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %v, want %v", res, want)
	}
}

func TestTake(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	s := FromSlice(items).Take(3)
	res, err := s.ToSlice(context.Background())
	if err != nil {
		t.Fatalf("ToSlice failed: %v", err)
	}
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %v, want %v", res, want)
	}
}

func TestFluentChain(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s := FromSlice(items).
		Filter(func(v int) bool { return v%2 == 0 }).
		Take(3)

	res, err := s.ToSlice(context.Background())
	if err != nil {
		t.Fatalf("ToSlice failed: %v", err)
	}
	want := []int{2, 4, 6}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %v, want %v", res, want)
	}
}

func TestBatch(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	s := FromSlice(items)
	bs := Batch(s, 2)
	res, err := bs.ToSlice(context.Background())
	if err != nil {
		t.Fatalf("ToSlice failed: %v", err)
	}
	want := [][]int{{1, 2}, {3, 4}, {5}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %v, want %v", res, want)
	}
}

func TestParallelMap(t *testing.T) {
	t.Run("Unordered", func(t *testing.T) {
		err := Run(context.Background(), func(s *Scope) {
			items := []int{1, 2, 3, 4, 5}
			src := FromSlice(items)
			pm := ParallelMap(context.Background(), s, src, StreamOptions{MaxWorkers: 3}, func(ctx context.Context, v int) (int, error) {
				return v * 10, nil
			})
			res, err := pm.ToSlice(context.Background())
			if err != nil {
				t.Fatalf("ToSlice failed: %v", err)
			}
			if len(res) != 5 {
				t.Errorf("got len %d, want 5", len(res))
			}
			// Check if all items are present (unordered)
			found := make(map[int]bool)
			for _, v := range res {
				found[v] = true
			}
			for _, v := range items {
				if !found[v*10] {
					t.Errorf("item %d not found", v*10)
				}
			}
		})
		if err != nil {
			t.Fatalf("Scope failed: %v", err)
		}
	})

	t.Run("Ordered", func(t *testing.T) {
		err := Run(context.Background(), func(s *Scope) {
			items := []int{1, 2, 3, 4, 5}
			src := FromSlice(items)
			pm := ParallelMap(context.Background(), s, src, StreamOptions{MaxWorkers: 3, Ordered: true}, func(ctx context.Context, v int) (int, error) {
				// Sleep to potentially cause out-of-order completion
				time.Sleep(time.Duration(5-v) * 10 * time.Millisecond)
				return v * 10, nil
			})
			res, err := pm.ToSlice(context.Background())
			if err != nil {
				t.Fatalf("ToSlice failed: %v", err)
			}
			want := []int{10, 20, 30, 40, 50}
			if !reflect.DeepEqual(res, want) {
				t.Errorf("got %v, want %v", res, want)
			}
		})
		if err != nil {
			t.Fatalf("Scope failed: %v", err)
		}
	})
}

func TestStreamError(t *testing.T) {
	wantErr := errors.New("boom")
	s := FromFunc(func(ctx context.Context) (int, error) {
		return 0, wantErr
	})
	_, err := s.ToSlice(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("got error %v, want %v", err, wantErr)
	}
}

func TestParallelMapError(t *testing.T) {
	wantErr := errors.New("worker-boom")
	err := Run(context.Background(), func(s *Scope) {
		src := FromSlice([]int{1, 2, 3})
		pm := ParallelMap(context.Background(), s, src, StreamOptions{MaxWorkers: 2}, func(ctx context.Context, v int) (int, error) {
			if v == 2 {
				return 0, wantErr
			}
			return v, nil
		})
		_, err := pm.ToSlice(context.Background())
		if !errors.Is(err, wantErr) {
			t.Errorf("got error %v, want %v", err, wantErr)
		}
	})
	// In FailFast policy, the scope might also return the error
	if err != nil && !errors.Is(err, wantErr) {
		t.Errorf("scope error %v, want %v", err, wantErr)
	}
}

func TestStreamMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("Skip", func(t *testing.T) {
		items := []int{1, 2, 3, 4, 5}
		res, err := FromSlice(items).Skip(2).ToSlice(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []int{3, 4, 5}
		if !reflect.DeepEqual(res, want) {
			t.Errorf("got %v, want %v", res, want)
		}
	})

	t.Run("Peek", func(t *testing.T) {
		var peeked []int
		items := []int{1, 2, 3}
		_, err := FromSlice(items).Peek(func(v int) {
			peeked = append(peeked, v)
		}).ToSlice(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(peeked, items) {
			t.Errorf("got %v, want %v", peeked, items)
		}
	})

	t.Run("Count", func(t *testing.T) {
		items := []int{1, 2, 3, 4, 5}
		count, err := FromSlice(items).Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 5 {
			t.Errorf("got %d, want 5", count)
		}
	})

	t.Run("ForEach", func(t *testing.T) {
		var sum int
		items := []int{1, 2, 3}
		err := FromSlice(items).ForEach(ctx, func(v int) error {
			sum += v
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if sum != 6 {
			t.Errorf("got %d, want 6", sum)
		}
	})

	t.Run("Collect", func(t *testing.T) {
		items := []int{1, 2}
		res, err := FromSlice(items).Collect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res, items) {
			t.Errorf("got %v, want %v", res, items)
		}
	})
}
