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
    sp.Go("fetch", func(ctx context.Context) error {
        return fetch(ctx)
    })
    sp.Spawn("process", func(ctx context.Context, sub scoped.Spawner) error {
        sub.Go("step-1", step1)
        return nil
    })
})
```

Use `Go` for simple tasks and `Spawn` when the task needs to spawn sub-tasks.

### `New` / `Wait` — Manual lifecycle

For cases where spawning happens outside a callback:

```go
sc, sp := scoped.New(ctx, scoped.WithPolicy(scoped.Collect))
sp.Go("task", func(ctx context.Context) error {
    return doWork(ctx)
})
err := sc.Wait()
```

`WaitTimeout` adds a deadline — it returns `context.DeadlineExceeded` if tasks
don't finish in time without cancelling the scope:

```go
err := sc.WaitTimeout(5 * time.Second)
```

### Error Policies

- **`FailFast`** (default) — First error cancels all siblings. `Wait()` returns that error.
- **`Collect`** — All errors are collected. `Wait()` returns all via `errors.Join`.

```go
scoped.Run(ctx, fn, scoped.WithPolicy(scoped.Collect))
```

Use `WithMaxErrors(n)` to cap stored errors in Collect mode, preventing
unbounded memory growth. Check `sc.DroppedErrors()` for the overflow count.

### Error Introspection

All task errors are wrapped in `*TaskError` for attribution:

```go
err := scoped.Run(ctx, fn, scoped.WithPolicy(scoped.Collect))

for _, te := range scoped.AllTaskErrors(err) {
    fmt.Printf("task %q failed: %v\n", te.Task.Name, te.Err)
}

// Or check individual errors:
if info, ok := scoped.TaskOf(err); ok {
    fmt.Println("failed task:", info.Name)
}
cause := scoped.CauseOf(err) // unwrap TaskError to get the root cause
```

### Bounded Concurrency

Limit the number of concurrent goroutines:

```go
scoped.Run(ctx, fn, scoped.WithLimit(10))
```

### Panic Recovery

By default, panics are re-raised in `Wait()`. Use `WithPanicAsError()` to convert them to `*PanicError` errors with full stack traces:

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

Results are returned in input order. Use `Collect` policy for partial results:

```go
results, err := scoped.MapSlice(ctx, items, func(ctx context.Context, item T) (R, error) {
    return transform(ctx, item)
}, scoped.WithLimit(5))

for _, r := range results {
    if r.Err != nil {
        // handle per-item error
    }
    // use r.Value
}
```

### `SpawnResult` — Typed async result

```go
r := scoped.SpawnResult(sp, "compute", func(ctx context.Context) (int, error) {
    return expensiveCalc(ctx)
})
val, err := r.Wait()
```

### `SpawnTimeout` — Per-task deadline

```go
scoped.SpawnTimeout(sp, "slow-op", 5*time.Second,
    func(ctx context.Context, _ scoped.Spawner) error {
        return slowOperation(ctx) // cancelled after 5s
    },
)
```

### `SpawnRetry` — Retry with backoff

Retries up to `n` times with exponential backoff. Stops immediately on context cancellation:

```go
scoped.SpawnRetry(sp, "flaky-api", 3, 100*time.Millisecond,
    func(ctx context.Context, _ scoped.Spawner) error {
        return callFlakyAPI(ctx)
    },
)
// Backoff: 100ms, 200ms, 400ms
```

### `SpawnScope` — Sub-scopes

Run a group of tasks with an independent error policy inside a parent scope:

```go
scoped.Run(ctx, func(sp scoped.Spawner) {
    scoped.SpawnScope(sp, "batch", func(sub scoped.Spawner) {
        for _, item := range items {
            sub.Go(item.Name, item.Process)
        }
    }, scoped.WithPolicy(scoped.Collect))
})
```

## Semaphore

Standalone weighted semaphore for use outside scopes:

```go
sem := scoped.NewSemaphore(5)

if err := sem.Acquire(ctx); err != nil {
    return err // context cancelled
}
defer sem.Release()

// ... do bounded work
```

## Worker Pool

Fixed-size worker pool with queue:

```go
pool := scoped.NewPool(ctx, 4, scoped.WithQueueSize(100))

pool.Submit(func() error {
    return processJob(job)
})

if ok := pool.TrySubmit(fn); !ok {
    // queue full or pool closed
}

err := pool.Close() // waits for in-flight tasks, returns joined errors
```

## Observability

### Lifecycle hooks

```go
scoped.Run(ctx, fn,
    scoped.WithOnEvent(func(e scoped.TaskEvent) {
        log.Printf("[%s] task=%s err=%v dur=%s",
            e.Kind, e.Task.Name, e.Err, e.Duration)
    }),
)
```

Event kinds: `EventStarted`, `EventDone`, `EventErrored`, `EventPanicked`, `EventCancelled`.

Legacy per-phase hooks are also available via `WithOnStart` and `WithOnDone`.

### Periodic metrics

```go
scoped.Run(ctx, fn,
    scoped.WithOnMetrics(time.Second, func(m scoped.Metrics) {
        fmt.Printf("active=%d completed=%d errored=%d\n",
            m.ActiveTasks, m.Completed, m.Errored)
    }),
)
```

## Streams

Pull-based, composable data pipelines with lazy evaluation.

### Creating streams

```go
s := scoped.FromSlice([]int{1, 2, 3, 4, 5})
s := scoped.FromChan(ch)
s := scoped.NewStream(func(ctx context.Context) (int, error) {
    // custom iterator — return io.EOF when done
})
```

### Chaining operations

```go
results, err := scoped.FromSlice(items).
    Filter(func(v int) bool { return v > 0 }).
    Skip(10).
    Take(100).
    Peek(func(v int) { log.Println(v) }).
    ToSlice(ctx)
```

### Type-changing transforms

Go does not support generic methods on generic types, so cross-type
operations are top-level functions:

```go
mapped := scoped.Map(stream, func(ctx context.Context, v int) (string, error) {
    return strconv.Itoa(v), nil
})

batched := scoped.Batch(stream, 10)          // *Stream[[]int]
reduced, _ := scoped.Reduce(ctx, stream, 0,  // fold
    func(acc, v int) int { return acc + v },
)
flat := scoped.FlatMap(stream, func(ctx context.Context, v int) *scoped.Stream[string] {
    return scoped.FromSlice(strings.Split(fmt.Sprint(v), ""))
})
unique := scoped.Distinct(stream)            // requires comparable
```

### `ParallelMap` — Concurrent stream transformation

```go
out := scoped.ParallelMap(ctx, sp, src, scoped.StreamOptions{
    MaxWorkers: 4,
    Ordered:    true,
    MaxPending: 16, // backpressure buffer for ordered mode
}, func(ctx context.Context, v int) (string, error) {
    return transform(ctx, v)
})

results, err := out.ToSlice(ctx)
```

### Terminal operations

| Method | Description |
|--------|-------------|
| `ToSlice(ctx)` | Collect all items into a slice |
| `ForEach(ctx, fn)` | Apply a function to each item |
| `Count(ctx)` | Count items in the stream |
| `ToChanScope(sp)` | Bridge to a channel within a scope |
| `Stop()` | Release resources (safe to call multiple times) |

## Channel Utilities (`chanx`)

The `chanx` subpackage provides context-aware channel operations:

```bash
go get github.com/baxromumarov/scoped/chanx
```

### Send and Receive

| Function | Description |
|----------|-------------|
| `Send` / `Recv` | Context-aware send and receive |
| `TrySend` / `TryRecv` | Non-blocking send and receive |
| `SendTimeout` / `RecvTimeout` | Send and receive with a deadline |
| `SendBatch` / `RecvBatch` | Batch send and receive |

### Fan-in, Fan-out, and Broadcasting

| Function | Description |
|----------|-------------|
| `Merge` | Combine multiple channels into one |
| `FanOut` | Distribute to N workers (round-robin) |
| `Tee` | Broadcast to N unbuffered consumers (all must read) |
| `Broadcast` | Broadcast with buffered outputs (tolerates slow consumers) |

### Transformation and Filtering

| Function | Description |
|----------|-------------|
| `Map` | Transform values through a pipeline |
| `Filter` | Pass only values matching a predicate |

### Rate Limiting and Batching

| Function | Description |
|----------|-------------|
| `Throttle` | Token-bucket rate limiting |
| `Buffer` | Batch by size or timeout |
| `BufferWithReason` | Batch with flush reason (`FlushSize`, `FlushTimeout`, `FlushClose`) |

### Timing

| Function | Description |
|----------|-------------|
| `Debounce` | Emit last value after a quiet period |
| `Window` | Time-based grouping (`Tumbling` or `Sliding` mode) |

### Combining and Selection

| Function | Description |
|----------|-------------|
| `Zip` | Pair values from two channels into `Pair[A, B]` |
| `First` | Race: first value from any channel |

### Lifecycle

| Function | Description |
|----------|-------------|
| `OrDone` | Wrap a channel with context cancellation |
| `Drain` | Discard remaining values to unblock producers |
| `Closable` | Idempotent-close channel wrapper (panics become `ErrClosed`) |

## License

MIT License — Copyright (c) 2026 Baxrom Umarov
