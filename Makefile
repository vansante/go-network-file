all: lint test

clean:
	# Cleaning test cache
	go clean -testcache

lint:
	go get github.com/golangci/golangci-lint/cmd/golangci-lint@v1.19.1
	golangci-lint run

test:
	# Running tests
	go test -v -timeout 10s -race ./...

.PHONY: all clean lint test