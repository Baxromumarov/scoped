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
	"time"

	"github.com/baxromumarov/scoped"
)

func w1(ctx context.Context) error {
	select {
	case <-time.After(1 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func w2(ctx context.Context) error {
	select {
	case <-time.After(1 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func w3(ctx context.Context) error {
	return fmt.Errorf("w3 failed")
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	arr := []func(context.Context) error{w3, w1, w2}

	now := time.Now()

	err := scoped.Run(
		ctx,
		func(sp scoped.Spawner) {
			for idx, f := range arr {
				f := f
				sp.Go(
					fmt.Sprintf("%d index", idx),
					func(ctx context.Context, _ scoped.Spawner) error {
						return f(ctx)
					},
				)
			}
		},
		scoped.WithPolicy(scoped.FailFast),
		scoped.WithPanicAsError(),
	)

	if err != nil {
		fmt.Println("Final error:", err)
	}

	fmt.Println("Elapsed time:", time.Since(now))
}
```

## License

MIT License

Copyright (c) 2026 Baxrom Umarov

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
