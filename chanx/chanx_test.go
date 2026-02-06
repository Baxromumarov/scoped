package chanx_test

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/baxromumarov/scoped/chanx"
)

// --- Send / Recv ---

func TestSend(t *testing.T) {
	ch := make(chan int, 1)
	err := chanx.Send(context.Background(), ch, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := <-ch; v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestSendContextCancel(t *testing.T) {
	ch := make(chan int) // unbuffered, will block
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := chanx.Send(ctx, ch, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRecv(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42
	v, ok, err := chanx.Recv(context.Background(), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestRecvClosed(t *testing.T) {
	ch := make(chan int)
	close(ch)
	_, ok, err := chanx.Recv(context.Background(), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for closed channel")
	}
}

func TestRecvContextCancel(t *testing.T) {
	ch := make(chan int) // will block
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := chanx.Recv(ctx, ch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// --- Merge ---

func TestMerge(t *testing.T) {
	ch1 := make(chan int, 3)
	ch2 := make(chan int, 3)
	ch1 <- 1
	ch1 <- 2
	close(ch1)
	ch2 <- 3
	ch2 <- 4
	close(ch2)

	merged := chanx.Merge(context.Background(), ch1, ch2)
	var got []int
	for v := range merged {
		got = append(got, v)
	}
	sort.Ints(got)
	expected := []int{1, 2, 3, 4}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("at index %d: expected %d, got %d", i, expected[i], got[i])
		}
	}
}

func TestMergeContextCancel(t *testing.T) {
	ch1 := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())

	merged := chanx.Merge(ctx, ch1)

	// Cancel immediately.
	cancel()

	// The merged channel should close.
	select {
	case _, ok := <-merged:
		if ok {
			t.Fatal("expected merged channel to close")
		}
	case <-time.After(time.Second):
		t.Fatal("merged channel did not close after context cancel")
	}
}

func TestMergeEmpty(t *testing.T) {
	merged := chanx.Merge[int](context.Background())
	select {
	case _, ok := <-merged:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("empty merge did not close")
	}
}

// --- FanOut ---

func TestFanOut(t *testing.T) {
	in := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		in <- i
	}
	close(in)

	outs := chanx.FanOut(context.Background(), in, 3)
	if len(outs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(outs))
	}

	var all []int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ch := range outs {
		ch := ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range ch {
				mu.Lock()
				all = append(all, v)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	sort.Ints(all)
	expected := []int{1, 2, 3, 4, 5, 6}
	if len(all) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, all)
	}
	for i := range all {
		if all[i] != expected[i] {
			t.Fatalf("at index %d: expected %d, got %d", i, expected[i], all[i])
		}
	}
}

func TestFanOutCancel(t *testing.T) {
	in := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())
	outs := chanx.FanOut(ctx, in, 2)

	cancel()

	for i, ch := range outs {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatalf("output %d should be closed", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("output %d did not close after cancel", i)
		}
	}
}

func TestFanOutInvalidNPanics(t *testing.T) {
	for _, n := range []int{0, -1} {
		n := n
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for n=%d", n)
				}
			}()
			_ = chanx.FanOut(context.Background(), make(chan int), n)
		})
	}
}

// --- Tee ---

func TestTee(t *testing.T) {
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)

	outs := chanx.Tee(context.Background(), in, 2)
	if len(outs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outs))
	}

	var wg sync.WaitGroup
	results := make([][]int, 2)
	for i, ch := range outs {
		i, ch := i, ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range ch {
				results[i] = append(results[i], v)
			}
		}()
	}
	wg.Wait()

	for i, r := range results {
		if len(r) != 3 {
			t.Fatalf("output %d: expected 3 values, got %d", i, len(r))
		}
		for j, v := range r {
			if v != j+1 {
				t.Fatalf("output %d[%d] = %d, want %d", i, j, v, j+1)
			}
		}
	}
}

func TestTeeCancel(t *testing.T) {
	in := make(chan int)
	ctx, cancel := context.WithCancel(context.Background())
	outs := chanx.Tee(ctx, in, 2)

	cancel()

	for i, ch := range outs {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatalf("output %d should be closed", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("output %d did not close after cancel", i)
		}
	}
}

func TestTeeInvalidNPanics(t *testing.T) {
	for _, n := range []int{0, -1} {
		n := n
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for n=%d", n)
				}
			}()
			_ = chanx.Tee(context.Background(), make(chan int), n)
		})
	}
}

// --- OrDone ---

func TestOrDone(t *testing.T) {
	in := make(chan int, 3)
	in <- 1
	in <- 2
	in <- 3
	close(in)

	out := chanx.OrDone(context.Background(), in)
	var got []int
	for v := range out {
		got = append(got, v)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 values, got %d", len(got))
	}
}

func TestOrDoneCancel(t *testing.T) {
	in := make(chan int) // will block
	ctx, cancel := context.WithCancel(context.Background())
	out := chanx.OrDone(ctx, in)

	cancel()

	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("OrDone did not close after cancel")
	}
}

// --- Drain ---

func TestDrain(t *testing.T) {
	ch := make(chan int, 5)
	for i := 0; i < 5; i++ {
		ch <- i
	}
	close(ch)

	chanx.Drain(ch)

	// Channel should be drained. Reading again gives zero, not-ok.
	v, ok := <-ch
	if ok {
		t.Fatalf("expected closed channel, got value %d", v)
	}
}

// --- Closable ---

func TestClosable(t *testing.T) {
	c := chanx.NewClosable[int](5)
	for i := 0; i < 5; i++ {
		if err := c.Send(i); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	c.Close()

	var got []int
	for v := range c.Chan() {
		got = append(got, v)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 values, got %d", len(got))
	}
}

func TestClosableDoubleClose(t *testing.T) {
	c := chanx.NewClosable[int](0)
	c.Close()
	c.Close() // should not panic
}

func TestClosableSendAfterClose(t *testing.T) {
	c := chanx.NewClosable[int](0)
	c.Close()
	err := c.Send(1)
	if !errors.Is(err, chanx.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestClosableSendContext(t *testing.T) {
	c := chanx.NewClosable[int](1)
	err := c.SendContext(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v := <-c.Chan()
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	c.Close()
}

func TestClosableSendContextCancel(t *testing.T) {
	c := chanx.NewClosable[int](0) // unbuffered, will block
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.SendContext(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	c.Close()
}

func TestClosableSendContextAfterClose(t *testing.T) {
	c := chanx.NewClosable[int](1)
	c.Close()
	err := c.SendContext(context.Background(), 1)
	if !errors.Is(err, chanx.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestClosableDone(t *testing.T) {
	c := chanx.NewClosable[int](0)

	select {
	case <-c.Done():
		t.Fatal("Done should not be closed yet")
	default:
	}

	c.Close()

	select {
	case <-c.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("Done was not closed after Close()")
	}
}
