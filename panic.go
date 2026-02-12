package scoped

import (
	"fmt"
	"runtime"
)

// PanicError wraps a recovered panic value together with the goroutine
// stack trace captured at the point of the panic.
//
// When [WithPanicAsError] is set, panics in tasks are converted to
// *PanicError and returned as regular errors. Otherwise, the *PanicError
// is re-raised via panic in [Scope.Wait].
type PanicError struct {
	// Value is the original value passed to panic().
	Value any

	// Stack is the goroutine stack trace at the point of panic.
	Stack string
}

// Error returns a human-readable representation of the panic,
// including the value and the full stack trace.
func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %v\n\n%s", e.Value, e.Stack)
}

// Unwrap returns the panic value as an error, if it implements the error
// interface, or nil otherwise.
func (e *PanicError) Unwrap() error {
	if err, ok := e.Value.(error); ok {
		return err
	}
	return nil
}

func newPanicError(v any) *PanicError {
	// Grow buffer until the full stack fits.
	var stack string
	for size := 8192; ; size *= 2 {
		buf := make([]byte, size)
		n := runtime.Stack(buf, false)
		if n < size {
			stack = string(buf[:n])
			break
		}
	}
	return &PanicError{
		Value: v,
		Stack: stack,
	}
}
