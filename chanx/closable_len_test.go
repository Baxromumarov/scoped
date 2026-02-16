package chanx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClosable_LenAfterClose(t *testing.T) {
	c := NewClosable[int](10)

	// Send 3 items.
	require.NoError(t, c.Send(1))
	require.NoError(t, c.Send(2))
	require.NoError(t, c.Send(3))

	assert.Equal(t, 3, c.Len(), "Len should reflect buffered items before close")

	c.Close()

	// After close, Len returns 0 even though items are still drainable.
	assert.Equal(t, 0, c.Len(), "Len should return 0 after close")

	// Items are still drainable from Chan().
	var drained []int
	for range 3 {
		v := <-c.Chan()
		drained = append(drained, v)
	}
	assert.Equal(t, []int{1, 2, 3}, drained)
}

func TestClosable_LenBeforeClose(t *testing.T) {
	c := NewClosable[int](10)

	assert.Equal(t, 0, c.Len())

	_ = c.Send(42)
	assert.Equal(t, 1, c.Len())

	_ = c.Send(43)
	assert.Equal(t, 2, c.Len())

	<-c.Chan()
	assert.Equal(t, 1, c.Len())
}
