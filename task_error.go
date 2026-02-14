package scoped

import (
	"errors"
	"fmt"
)

// TaskError wraps an error together with the [TaskInfo] of the task that
// produced it. Scope error aggregation wraps every task failure in a
// TaskError so callers can attribute errors to specific tasks.
type TaskError struct {
	Task TaskInfo
	Err  error
}

func (e *TaskError) Error() string {
	return fmt.Sprintf("task %q failed: %v", e.Task.Name, e.Err)
}

func (e *TaskError) Unwrap() error {
	return e.Err
}

// IsTaskError reports whether err (or any error in its chain) is a [*TaskError].
func IsTaskError(err error) bool {
	if err == nil {
		return false
	}
	var te *TaskError
	return errors.As(err, &te)
}

// TaskOf extracts the [TaskInfo] from the first [*TaskError] in err's chain.
// Returns false if no TaskError is found.
func TaskOf(err error) (TaskInfo, bool) {
	if err == nil {
		return TaskInfo{}, false
	}

	var te *TaskError
	if errors.As(err, &te) {
		return te.Task, true
	}
	return TaskInfo{}, false
}

// CauseOf unwraps the first [*TaskError] in err's chain and returns its
// underlying cause. If err is not a TaskError, it is returned as-is.
// Returns nil if err is nil.
func CauseOf(err error) error {
	if err == nil {
		return nil
	}

	var te *TaskError
	if errors.As(err, &te) {
		return te.Err
	}

	return err
}
// AllTaskErrors recursively collects every [*TaskError] from err's chain,
// including errors wrapped via [errors.Join]. Returns nil if none are found.
func AllTaskErrors(err error) []*TaskError {
	if err == nil {
		return nil
	}

	var out []*TaskError
	collectTaskErrors(err, &out)
	return out
}

func collectTaskErrors(err error, out *[]*TaskError) {
	switch e := err.(type) {
	case *TaskError:
		*out = append(*out, e)

	case interface{ Unwrap() []error }:
		for _, sub := range e.Unwrap() {
			collectTaskErrors(sub, out)
		}

	case interface{ Unwrap() error }:
		collectTaskErrors(e.Unwrap(), out)
	}
}
