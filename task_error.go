package scoped

import (
	"errors"
	"fmt"
)

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

func IsTaskError(err error) bool {
	if err == nil {
		return false
	}
	var te *TaskError
	return errors.As(err, &te)
}

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
