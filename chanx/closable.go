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
type Closable[T any] struct {
	ch     chan T
	once   sync.Once
	closed chan struct{} // closed when Close() is called

	isClosed atomic.Bool // fast check for closed state without relying on recover
	capacity int         // capacity helps to prevent to sending data to filled channel
}

// NewClosable creates a Closable channel with the given buffer capacity.
func NewClosable[T any](capacity int) *Closable[T] {
	return &Closable[T]{
		capacity: capacity,
		ch:       make(chan T, capacity),
		closed:   make(chan struct{}),
	}
}

// Send sends v to the underlying channel. It returns [ErrClosed] if the
// channel has been closed. Send blocks if the channel buffer is full.
func (c *Closable[T]) Send(v T) (err error) {

	if c.isClosed.Load() {
		return ErrClosed
	}
	select {
	case c.ch <- v:
		return nil
	case <-c.closed:
		return ErrClosed
	}

}

// SendContext sends v to the underlying channel, unblocking early if ctx
// is canceled. Returns [ErrClosed] if the channel is closed, or the
// context error if canceled.
func (c *Closable[T]) SendContext(ctx context.Context, v T) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrClosed
		}
	}()
	select {
	case c.ch <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close closes the underlying channel. It is safe to call multiple times;
// only the first call actually closes the channel.
func (c *Closable[T]) Close() {
	c.once.Do(func() {
		c.isClosed.Store(true)
		close(c.ch)
		close(c.closed)
	})
}

// Chan returns the underlying channel for reading. The returned channel
// is closed when [Closable.Close] is called.
func (c *Closable[T]) Chan() <-chan T {
	return c.ch
}

// Done returns a channel that is closed when [Closable.Close] is called.
// This is useful for select statements that need to detect closure.
func (c *Closable[T]) Done() <-chan struct{} {
	return c.closed
}

// TrySend non-blocking send that returns ErrBuffFull instead of blocking or deadlocking
func (c *Closable[T]) TrySend(v T) error {
	if c.isClosed.Load() {
		return ErrClosed
	}
	if c.capacity == len(c.ch) {
		return ErrBuffFull
	}
	c.ch <- v
	return nil
}
