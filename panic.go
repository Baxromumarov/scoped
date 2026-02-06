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

// Unwrap returns nil. PanicError does not wrap another error.
func (e *PanicError) Unwrap() error { return nil }

func newPanicError(v any) *PanicError {
	// 8 KiB is enough for most stack traces. runtime.Stack truncates
	// gracefully if the buffer is too small.
	buf := make([]byte, 8192)
	n := runtime.Stack(buf, false)
	return &PanicError{
		Value: v,
		Stack: string(buf[:n]),
	}
}
