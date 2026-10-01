.PHONY: all build test coverage clean run-molu

all: test build

build:
	@mkdir -p bin
	go build -o bin/molu ./cmd/molu

test:
	go test -v -race ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/ coverage.out coverage.html

run-molu:
	go run ./cmd/molu