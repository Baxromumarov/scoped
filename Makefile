.PHONY: test bench lint cover vet clean

test:
	go test -race ./...

bench:
	go test -run '^$$' -bench . -benchmem ./...

lint:
	golangci-lint run ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vet:
	go vet ./...

clean:
	rm -f coverage.out
