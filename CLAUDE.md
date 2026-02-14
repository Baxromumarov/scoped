# Scoped — Project Conventions

## Overview

Structured concurrency library for Go. The root package provides `Scope`,
`Run`, `Spawner`, `Result`, `Stream`, and parallel helpers. The `chanx`
sub-package provides context-aware channel utilities.

## Build & Test

```bash
go test -race -count=1 ./...        # run all tests with race detector
go test -bench=. ./...               # run benchmarks
go vet ./...                         # static analysis
```

## Architecture

| File | Purpose |
|------|---------|
| `scope.go` | Core scope lifecycle (`Run`, `New`, `Scope.Wait`, `finalize`) |
| `spawner.go` | `Spawner` interface and goroutine management |
| `options.go` | Configuration (`WithPolicy`, `WithLimit`, `WithPanicAsError`, hooks) |
| `stream.go` | Pull-based `Stream[T]` with transforms and `ParallelMap` |
| `result.go` | `SpawnResult` — typed async results |
| `helpers.go` | `ForEachSlice`, `MapSlice` convenience wrappers |
| `task_error.go` | `TaskError` wrapping, extraction helpers |
| `panic.go` | `PanicError` with stack capture |
| `doc.go` | Package-level godoc |
| `chanx/` | Context-aware channel ops (Send, Recv, Merge, FanOut, Tee, etc.) |

## Conventions

### Go Version
- **Go 1.22** minimum. Uses range-over-int, generics.

### Parameter Order
- `ctx context.Context` is always the first parameter.
- `Spawner` comes before the data it operates on.

### Validation
- **Panic on programmer errors**: nil callbacks, nil spawners, invalid config
  (n <= 0, negative buffer). These are programming mistakes, not runtime
  conditions.
- Prefix all panic messages with `"scoped: "`.

### Error Handling

Two error policies in scope:

| Policy | Behavior | Aggregation |
|--------|----------|-------------|
| `FailFast` (default) | First error cancels all siblings | Returns first error (wrapped in `TaskError`) |
| `Collect` | All tasks run to completion | Returns `errors.Join` of all `TaskError`s |

Stream error model:
- `Stream.Err()` aggregates via `errors.Join` — all errors preserved.
- `io.EOF` signals normal end-of-stream, never stored as an error.
- Terminal methods (`ToSlice`, `ForEach`, `Batch`) return partial results
  alongside the error, following `io.Reader` conventions.

### Concurrency Patterns

- **Double-select** for context cancellation in goroutines (chanx).
- **Nil channel guard**: functions that receive channels handle nil by closing
  output immediately.
- **`defer close(out)`** in every goroutine that creates output channels.
- **Semaphore pattern**: `chan struct{}` for concurrency limiting.
- **Raw goroutines for ParallelMap workers**: avoids deadlock when consumer
  reads inside `Run`. Only the dispatcher uses `sp.Spawn`.

### Testing

- Root package: standard `testing` (no testify).
- `chanx/`: uses `github.com/stretchr/testify` (assert/require).
- Always run with `-race`.
- Every `Example*` function must have an `// Output:` comment.
- Test concurrent code with `sync/atomic` counters, not sleep-based timing.

### Godoc

- Every exported type, function, and method must have a doc comment.
- Start doc comments with the symbol name: `// Run creates a Scope...`
- Use `[Symbol]` link syntax for cross-references.
- Put runnable examples in `example_test.go` (package `scoped_test`).

### Common Pitfalls

- **Tee tests (chanx)**: outputs are unbuffered; read ALL outputs concurrently
  or deadlock.
- **select with pre-cancelled context + buffered channel**: Go picks a random
  ready case. Use unbuffered channels in tests that assert context errors.
- **ParallelMap ordered mode**: the pending map can grow if an early item is
  slow while later items complete. Bounded by `MaxWorkers + BufferSize` in
  typical workloads.
- **Spawner lifetime**: calling `Spawn` after the task function returns panics.
  The child spawner is valid only during task execution.
