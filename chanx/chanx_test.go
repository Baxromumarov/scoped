package chanx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSend(t *testing.T) {
	ch := make(chan int, 1) // buffered so Send doesn't block

	err := Send(context.Background(), ch, 12)
	assert.NoError(t, err)

	// Verify value was sent
	val := <-ch
	assert.Equal(t, 12, val)
}

func TestSend_ContextCanceled(t *testing.T) {
	ch := make(chan int) // unbuffered

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Send(ctx, ch, 12)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
