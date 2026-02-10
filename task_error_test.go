package scoped

import (
	"errors"
	"fmt"
	"testing"
)

func TestTaskError_Error(t *testing.T) {
	err := errors.New("something went wrong")
	te := &TaskError{
		Task: TaskInfo{Name: "worker-1"},
		Err:  err,
	}

	expected := `task "worker-1" failed: something went wrong`
	if got := te.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
}

func TestTaskError_Unwrap(t *testing.T) {
	err := errors.New("original error")
	te := &TaskError{
		Task: TaskInfo{Name: "worker-1"},
		Err:  err,
	}

	if got := te.Unwrap(); got != err {
		t.Errorf("Unwrap() = %v, want %v", got, err)
	}
}

func TestIsTaskError(t *testing.T) {
	te := &TaskError{
		Task: TaskInfo{Name: "task"},
		Err:  errors.New("err"),
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			want: false,
		},
		{
			name: "TaskError",
			err:  te,
			want: true,
		},
		{
			name: "wrapped TaskError",
			err:  fmt.Errorf("wrapped: %w", te),
			want: true,
		},
		{
			name: "joined errors containing TaskError",
			err:  errors.Join(errors.New("other"), te),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTaskError(tt.err); got != tt.want {
				t.Errorf("IsTaskError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTaskOf(t *testing.T) {
	info := TaskInfo{Name: "target-task"}
	te := &TaskError{
		Task: info,
		Err:  errors.New("err"),
	}

	tests := []struct {
		name     string
		err      error
		wantInfo TaskInfo
		wantOk   bool
	}{
		{
			name:     "nil error",
			err:      nil,
			wantInfo: TaskInfo{},
			wantOk:   false,
		},
		{
			name:     "standard error",
			err:      errors.New("standard"),
			wantInfo: TaskInfo{},
			wantOk:   false,
		},
		{
			name:     "TaskError",
			err:      te,
			wantInfo: info,
			wantOk:   true,
		},
		{
			name:     "wrapped TaskError",
			err:      fmt.Errorf("wrapped: %w", te),
			wantInfo: info,
			wantOk:   true,
		},
		{
			name:     "joined errors containing TaskError",
			err:      errors.Join(errors.New("other"), te),
			wantInfo: info,
			wantOk:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInfo, gotOk := TaskOf(tt.err)
			if gotOk != tt.wantOk {
				t.Errorf("TaskOf() ok = %v, want %v", gotOk, tt.wantOk)
			}
			if gotInfo != tt.wantInfo {
				t.Errorf("TaskOf() info = %v, want %v", gotInfo, tt.wantInfo)
			}
		})
	}
}

func TestCauseOf(t *testing.T) {
	rootErr := errors.New("root cause")
	te := &TaskError{
		Task: TaskInfo{Name: "task"},
		Err:  rootErr,
	}

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "nil error",
			err:  nil,
			want: nil,
		},
		{
			name: "standard error",
			err:  rootErr,
			want: rootErr,
		},
		{
			name: "TaskError",
			err:  te,
			want: rootErr,
		},
		{
			name: "wrapped TaskError",
			err:  fmt.Errorf("wrapped: %w", te),
			want: rootErr,
		},
		{
			name: "joined errors containing TaskError",
			err:  errors.Join(errors.New("other"), te),
			want: rootErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CauseOf(tt.err)
			if got != tt.want {
				t.Errorf("CauseOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllTaskErrors(t *testing.T) {
	te1 := &TaskError{Task: TaskInfo{Name: "t1"}, Err: errors.New("e1")}
	te2 := &TaskError{Task: TaskInfo{Name: "t2"}, Err: errors.New("e2")}
	te3 := &TaskError{Task: TaskInfo{Name: "t3"}, Err: errors.New("e3")}

	// TaskError wrapping another TaskError
	teNested := &TaskError{Task: TaskInfo{Name: "outer"}, Err: te1}

	tests := []struct {
		name string
		err  error
		want []*TaskError
	}{
		{
			name: "nil error",
			err:  nil,
			want: nil,
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			want: nil,
		},
		{
			name: "single TaskError",
			err:  te1,
			want: []*TaskError{te1},
		},
		{
			name: "wrapped TaskError",
			err:  fmt.Errorf("wrapped: %w", te1),
			want: []*TaskError{te1},
		},
		{
			name: "joined TaskErrors",
			err:  errors.Join(te1, te2),
			want: []*TaskError{te1, te2},
		},
		{
			name: "mixed joined errors",
			err:  errors.Join(errors.New("other"), te1, errors.New("other2"), te2),
			want: []*TaskError{te1, te2},
		},
		{
			name: "nested joins",
			err:  errors.Join(errors.Join(te1, te2), te3),
			want: []*TaskError{te1, te2, te3},
		},
		{
			name: "TaskError wrapping TaskError (stops at top)",
			err:  teNested,
			want: []*TaskError{teNested},
		},
		{
			name: "Join with nested TaskError",
			err:  errors.Join(teNested, te2),
			want: []*TaskError{teNested, te2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AllTaskErrors(tt.err)
			if len(got) != len(tt.want) {
				t.Fatalf("AllTaskErrors() len = %d, want %d", len(got), len(tt.want))
			}
			for i, g := range got {
				if g != tt.want[i] {
					t.Errorf("AllTaskErrors()[%d] = %v, want %v", i, g, tt.want[i])
				}
			}
		})
	}
}
