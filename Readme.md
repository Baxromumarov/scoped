# scoped

Structured concurrency primitives for Go — run goroutines with clear lifecycles, coordinated cancellation, and composable error handling.

## Install

```bash
go get github.com/baxromumarov/scoped
```

## Core Concepts

### `Run` — Scoped lifecycle

`Run` creates a scope, executes your function, and waits for all spawned tasks:

```go
err := scoped.Run(ctx, func(sp scoped.Spawner) {
    sp.Spawn("fetch", func(ctx context.Context, _ scoped.Spawner) error {
        return fetch(ctx)
    })
    sp.Spawn("process", func(ctx context.Context, _ scoped.Spawner) error {
        return process(ctx)
    })
})
```

### `New` / `Wait` — Manual lifecycle

For cases where spawning happens outside a callback:

```go
sc, sp := scoped.New(ctx, scoped.WithPolicy(scoped.Collect))
sp.Spawn("task", func(ctx context.Context, _ scoped.Spawner) error {
    return doWork(ctx)
})
err := sc.Wait()
```

### Error Policies

- **`FailFast`** (default) — First error cancels all siblings. `Wait()` returns that error.
- **`Collect`** — All errors are collected. `Wait()` returns all via `errors.Join`.

```go
scoped.Run(ctx, fn, scoped.WithPolicy(scoped.Collect))
```

### Bounded Concurrency

Limit the number of goroutines:

```go
scoped.Run(ctx, fn, scoped.WithLimit(10))
```

### Panic Recovery

By default, panics are re-raised in `Wait()`. Use `WithPanicAsError()` to convert them to errors:

```go
scoped.Run(ctx, fn, scoped.WithPanicAsError())
```

## Helpers

### `ForEachSlice` — Parallel iteration

```go
err := scoped.ForEachSlice(ctx, urls, func(ctx context.Context, u string) error {
    return fetch(ctx, u)
}, scoped.WithLimit(10))
```

### `MapSlice` — Parallel map with results

```go
results, err := scoped.MapSlice(ctx, items, func(ctx context.Context, item T) (R, error) {
    return transform(ctx, item)
}, scoped.WithLimit(5))
```

### `SpawnResult` — Typed async result

```go
r := scoped.SpawnResult(sp, "compute", func(ctx context.Context) (int, error) {
    return expensiveCalc(ctx)
})
val, err := r.Wait()
```

## Streams

Pull-based, composable data streams with parallel processing:

```go
err := scoped.Run(ctx, func(sp scoped.Spawner) {
    stream := scoped.FromSlice(items).
        Filter(func(v int) bool { return v > 0 }).
        Take(100)

    mapped := scoped.Map(stream, func(ctx context.Context, v int) (string, error) {
        return fmt.Sprint(v), nil
    })

    results, _ := mapped.ToSlice(ctx)
})
```

### `ParallelMap` — Concurrent stream transformation

```go
pm := scoped.ParallelMap(ctx, sp, src, scoped.StreamOptions{
    MaxWorkers: 4,
    Ordered:    true,
}, transformFn)
```

## Channel Utilities (`chanx`)

The `chanx` subpackage provides context-aware channel operations:

| Function | Description |
|----------|-------------|
| `Send` / `Recv` | Context-aware send and receive |
| `TrySend` / `TryRecv` | Non-blocking send and receive |
| `SendBatch` / `RecvBatch` | Batch operations |
| `Merge` | Fan-in: combine multiple channels |
| `FanOut` | Distribute to N workers (round-robin) |
| `Tee` | Broadcast to N consumers |
| `Map` / `Filter` | Transform / filter pipelines |
| `Throttle` | Rate-limit (token bucket) |
| `Buffer` | Batch by size or timeout |
| `First` | Race: first value from any channel |
| `OrDone` | Wrap channel with context cancellation |
| `Drain` | Discard remaining values |
| `Closable` | Idempotent-close channel wrapper |

## License

MIT License — Copyright (c) 2026 Baxrom Umarov
