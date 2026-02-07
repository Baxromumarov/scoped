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
