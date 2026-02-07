package chanx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrClosed is returned by [Closable.Send] when the channel has been closed.
var ErrClosed = errors.New("chanx: send on closed channel")

// ErrBuffFull is returned by [Closable.TrySend] when the channel is full
// but trying to send more data
var ErrBuffFull = errors.New("chanx: buffer is full")

// Closable wraps a channel with idempotent close and panic-safe send.
//
// Go channels panic on double close and on send-after-close. Closable
// converts these panics into errors, making it safe to use in
// concurrent teardown scenarios.
//
// Note: Unlike raw Go channels, the underlying data channel is NOT closed
// when Close() is called. Use Done() to detect closure in select statements.
// The Chan() method returns a receive-only channel that will continue to
// deliver any buffered messages after Close() is called.
type Closable[T any] struct {
	ch     chan T
	once   sync.Once
	closed chan struct{} // closed when Close() is called

	isClosed atomic.Bool // fast check for closed state
}

// NewClosable creates a Closable channel with the given buffer capacity.
func NewClosable[T any](capacity int) *Closable[T] {
	return &Closable[T]{
		ch:     make(chan T, capacity),
		closed: make(chan struct{}),
	}
}

// Send sends v to the underlying channel. It returns [ErrClosed] if the
// channel has been closed. Send blocks if the channel buffer is full.
func (c *Closable[T]) Send(v T) (err error) {
	// Fast path: check if already closed
	if c.isClosed.Load() {
		return ErrClosed
	}

	defer func() {
		if recover() != nil {
			err = ErrClosed
		}
	}()

	select {
	case c.ch <- v:
		return nil
	case <-c.closed:
		return ErrClosed
	}
}

// TrySend non-blocking send that returns ErrBuffFull instead of blocking or deadlocking
func (c *Closable[T]) TrySend(v T) error {
	// Fast path: check if already closed
	if c.isClosed.Load() {
		return ErrClosed
	}

	select {
	case c.ch <- v:
		return nil
	default:
		// buffer full OR no receiver - recheck closed status
		if c.isClosed.Load() {
			return ErrClosed
		}
		return ErrBuffFull
	}
}

// SendContext sends v to the underlying channel, unblocking early if ctx
// is canceled. Returns [ErrClosed] if the channel is closed, or the
// context error if canceled.
func (c *Closable[T]) SendContext(ctx context.Context, v T) error {
	// Fast path: check if already closed
	if c.isClosed.Load() {
		return ErrClosed
	}

	select {
	case c.ch <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return ErrClosed
	}
}

// Close marks the channel as closed. It is safe to call multiple times;
// only the first call has effect.
//
// After Close() is called:
//   - Send, TrySend, and SendContext will return ErrClosed
//   - Done() will return a closed channel
//   - Chan() remains open but no new values will be sent
//   - Existing buffered values can still be read from Chan()
func (c *Closable[T]) Close() {
	c.once.Do(func() {
		c.isClosed.Store(true)
		close(c.closed)
	})
}

// Chan returns the underlying channel for reading.
// Note: This channel is NOT closed when Close() is called.
// Use Done() in a select to detect closure, or range over Chan()
// with a separate Done() check.
func (c *Closable[T]) Chan() <-chan T {
	return c.ch
}

// Done returns a channel that is closed when [Closable.Close] is called.
// This is useful for select statements that need to detect closure.
func (c *Closable[T]) Done() <-chan struct{} {
	return c.closed
}

// IsClosed returns true if Close() has been called.
func (c *Closable[T]) IsClosed() bool {
	return c.isClosed.Load()
}

// Len returns the number of items currently buffered in the channel.
// Note: This is for informational purposes only and the value may be
// stale in concurrent contexts.
func (c *Closable[T]) Len() int {
	return len(c.ch)
}
