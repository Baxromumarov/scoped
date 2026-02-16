package scoped

import (
	"context"
	"sync/atomic"
)

// Semaphore is a weighted semaphore for bounding concurrency.
// It is context-aware: Acquire unblocks if the context is cancelled.
type Semaphore struct {
	ch       chan struct{}
	cap      int
	acquired atomic.Int64
}

// NewSemaphore creates a semaphore with the given capacity.
// Panics if n <= 0.
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		panic("scoped: NewSemaphore requires n > 0")
	}
	return &Semaphore{
		ch:  make(chan struct{}, n),
		cap: n,
	}
}

// Acquire blocks until a slot is available or ctx is cancelled.
// Returns ctx.Err() on cancellation, nil on success.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		s.acquired.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire attempts to acquire a slot without blocking.
// Returns true if acquired, false otherwise.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		s.acquired.Add(1)
		return true
	default:
		return false
	}
}

// Release releases a slot. Panics if more slots are released than acquired.
func (s *Semaphore) Release() {
	if s.acquired.Add(-1) < 0 {
		s.acquired.Add(1) // undo
		panic("scoped: Semaphore.Release called without matching Acquire")
	}
	<-s.ch
}

// Available returns the number of available slots.
// The value may be stale in concurrent contexts.
func (s *Semaphore) Available() int {
	return s.cap - len(s.ch)
}
