# scoped

`scoped` is a small Go package for structured concurrency.

It helps you run goroutines with a clear lifecycle: start tasks in a scope, wait for all of them, and handle cancellation/errors consistently.

## Install

```bash
go get github.com/baxromumarov/scoped
```

## Quick Example

```go
package main

import (
	"context"
	"fmt"

	"github.com/baxromumarov/scoped"
)

func main() {
	err := scoped.Run(context.Background(), func(s *scoped.Scope) {
		s.Go("hello", func(ctx context.Context) error {
			fmt.Println("hello")
			return nil
		})
		s.Go("world", func(ctx context.Context) error {
			fmt.Println("world")
			return nil
		})
	})
	if err != nil {
		panic(err)
	}
}
```

## Highlights

- Error policies: `FailFast` (default) and `Collect`
- Bounded concurrency with `WithLimit`
- Typed task results with `GoResult`
- Slice helpers: `ForEach` and `Map`
- Context-aware channel helpers in `github.com/baxromumarov/scoped/chanx`
