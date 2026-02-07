package chanx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClosable_Send(t *testing.T) {
	c := NewClosable[int](1)
	err := c.Send(-12)
	assert.NoError(t, err)

	err = c.TrySend(900)
	assert.EqualError(t, err, ErrBuffFull.Error())
}
func TestClosable_TrySend(t *testing.T) {
	c := NewClosable[int](2)
	err := c.TrySend(1)
	assert.NoError(t, err, "first try send error")

	err = c.TrySend(2)
	assert.NoError(t, err, "second try send error")

	err = c.TrySend(3)
	assert.EqualError(t, err, ErrBuffFull.Error())
}

func TestClosable_TrySendWithClose(t *testing.T) {
	c := NewClosable[int](2)
	err := c.TrySend(1)
	assert.NoError(t, err, "first try send error")
	c.Close() // close the channel and next TrySends must return ErrClosed error

	err = c.TrySend(2)
	assert.EqualError(t, err, ErrClosed.Error())

	err = c.TrySend(3)
	assert.EqualError(t, err, ErrClosed.Error())
}
