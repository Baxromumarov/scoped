package chanx

import (
	"context"
	"errors"
	"sync"
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

	mu       sync.RWMutex // protects isClosed and serializes with Close
	isClosed bool
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
func (c *Closable[T]) Send(v T) error {
	c.mu.RLock()
	if c.isClosed {
		c.mu.RUnlock()
		return ErrClosed
	}

	// Try non-blocking send first while holding the lock
	select {
	case c.ch <- v:
		c.mu.RUnlock()
		return nil
	default:
		// Buffer is full, need to block - but we can't hold lock while blocking
	}
	c.mu.RUnlock()

	// Block outside the lock, but use closed channel to detect close
	select {
	case c.ch <- v:
		return nil
	case <-c.closed:
		return ErrClosed
	}
}

// TrySend non-blocking send that returns ErrBuffFull instead of blocking or deadlocking
func (c *Closable[T]) TrySend(v T) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.isClosed {
		return ErrClosed
	}

	select {
	case c.ch <- v:
		return nil
	default:
		return ErrBuffFull
	}
}

// SendContext sends v to the underlying channel, unblocking early if ctx
// is canceled. Returns [ErrClosed] if the channel is closed, or the
// context error if canceled.
func (c *Closable[T]) SendContext(ctx context.Context, v T) error {
	c.mu.RLock()
	if c.isClosed {
		c.mu.RUnlock()
		return ErrClosed
	}

	// Try non-blocking send first while holding the lock
	select {
	case c.ch <- v:
		c.mu.RUnlock()
		return nil
	default:
		// Buffer is full, need to block
	}
	c.mu.RUnlock()

	// Block outside the lock
	select {
	case c.ch <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return ErrClosed
	}
}

// Close closes the underlying channel. It is safe to call multiple times;
// only the first call actually closes the channel.
func (c *Closable[T]) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.isClosed = true
		c.mu.Unlock()

		close(c.closed)
		close(c.ch)
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
