# scoped

[![Go Reference](https://pkg.go.dev/badge/github.com/baxromumarov/scoped.svg)](https://pkg.go.dev/github.com/baxromumarov/scoped)
[![CI](https://github.com/baxromumarov/scoped/actions/workflows/ci.yml/badge.svg)](https://github.com/baxromumarov/scoped/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/baxromumarov/scoped)](https://goreportcard.com/report/github.com/baxromumarov/scoped)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

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

### Panic Contract

`scoped` intentionally panics for programmer misuse (invalid arguments or invalid lifecycle usage), for example:

- Calling `Spawn` on a closed spawner.
- Calling stream `Next()` concurrently.
- Passing invalid option values (negative limits, nil required callbacks, etc.).

These panics are API contract checks, not runtime data-path errors.
Task-function panics are handled separately:

- Default behavior: panic is captured and re-raised at `Wait()`.
- `WithPanicAsError()`: panic is converted to `*PanicError` and returned as an error.

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

### `Race` — First successful result

Run multiple tasks concurrently, return the first successful result, cancel the rest:

```go
val, err := scoped.Race(ctx,
    func(ctx context.Context) (string, error) { return fetchFromA(ctx) },
    func(ctx context.Context) (string, error) { return fetchFromB(ctx) },
    func(ctx context.Context) (string, error) { return fetchFromC(ctx) },
)
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
        fmt.Printf("active=%d completed=%d errored=%d longest=%s\n",
            m.ActiveTasks, m.Completed, m.Errored, m.LongestActive)
    }),
)
```

### Stall detection

Detect tasks running longer than a threshold (purely observational — does not cancel):

```go
scoped.Run(ctx, fn,
    scoped.WithStallDetector(5*time.Second, func(rt scoped.RunningTask) {
        log.Printf("STALLED: task=%q running for %s", rt.Name, rt.Elapsed)
    }),
)
```

### Scope snapshots

Get a point-in-time view of all running tasks:

```go
sc, sp := scoped.New(ctx, scoped.WithTaskTracking())
// ... spawn tasks ...
snap := sc.Snapshot()
for _, rt := range snap.RunningTasks {
    fmt.Printf("  %s running for %s\n", rt.Name, rt.Elapsed)
}
fmt.Printf("longest active: %s\n", snap.LongestActive)
```

### Pool monitoring

```go
pool := scoped.NewPool(ctx, 4,
    scoped.WithPoolMetrics(time.Second, func(s scoped.PoolStats) {
        fmt.Printf("submitted=%d inflight=%d queue=%d\n",
            s.Submitted, s.InFlight, s.QueueDepth)
    }),
)

// Or poll on demand:
stats := pool.Stats()
```

### Stream monitoring

Streams track items, errors, and throughput automatically:

```go
s := scoped.FromSlice(items)
// ... consume stream ...
stats := s.Stats()
fmt.Printf("read=%d errors=%d throughput=%.0f items/sec\n",
    stats.ItemsRead, stats.Errors, stats.Throughput)
```

For per-item event hooks:

```go
observed := scoped.Observe(stream, func(e scoped.StreamEvent[int]) {
    if e.Err != nil {
        log.Printf("stream error at seq %d: %v", e.Seq, e.Err)
    }
})
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
s := scoped.Empty[int]()              // immediate EOF
s := scoped.Repeat("hello", 5)       // emit "hello" 5 times (-1 = infinite)
s := scoped.Generate(1, func(v int) int { return v * 2 }) // 1, 2, 4, 8, ...
```

### Chaining operations

```go
results, err := scoped.FromSlice(items).
    Filter(func(v int) bool { return v > 0 }).
    Skip(10).
    Take(100).
    TakeWhile(func(v int) bool { return v < 500 }).
    DropWhile(func(v int) bool { return v < 50 }).
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

scan := scoped.Scan(stream, 0,               // running fold
    func(acc, v int) int { return acc + v },
)

zipped := scoped.Zip(streamA, streamB)       // *Stream[Pair[A, B]]
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
| `First(ctx)` | Return the first item |
| `Last(ctx)` | Consume all, return the last item |
| `Any(ctx, fn)` | True if any item matches predicate |
| `All(ctx, fn)` | True if all items match predicate |
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
| `Take` | Forward first n items then close |
| `Skip` | Drop first n items then forward rest |
| `Scan` | Running accumulation of input values |
| `Partition` | Split by predicate into two channels (both must be read concurrently) |

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

## Benchmarks

Comparison against raw goroutines, `golang.org/x/sync/errgroup`, and `sourcegraph/conc` (AMD Ryzen 7, Go 1.26):

### Overhead per task spawn

| Implementation | 10 tasks | 100 tasks | 1000 tasks |
|----------------|----------|-----------|------------|
| Raw goroutine+WG | 1.7 us | 17.5 us | 167 us |
| errgroup | 1.8 us | 16.7 us | 181 us |
| conc | 1.8 us | 17.0 us | 172 us |
| **scoped** | 3.2 us | 24.7 us | 227 us |

### ForEach (10 items, light work)

| Implementation | ns/op | allocs/op |
|----------------|-------|-----------|
| **scoped ForEach** | **7,552** | **29** |
| conc Iterator | 8,402 | 14 |
| raw goroutines | 297,209 | 1,002 |
| errgroup | 337,836 | 2,004 |

### Hot-path operations (zero allocations)

| Operation | ns/op |
|-----------|-------|
| Semaphore Acquire/Release | ~30 ns |
| Pool Submit | ~120 ns |
| chanx.TrySend | ~9 ns |
| chanx.TryRecv | ~13 ns |
| chanx.Send (ctx-aware) | ~23 ns |

Run benchmarks locally:

```bash
make bench
```

## License

Licensed under MIT. See `LICENSE`.

## Release Process

Release checklist and tagging steps are in `RELEASE.md`.
