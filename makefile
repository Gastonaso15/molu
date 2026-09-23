.PHONY: all build test clean run-molu run-hub

all: test build

build:
	@mkdir -p bin
	go build -o bin/molu ./cmd/molu
	go build -o bin/molu-hub ./cmd/molu-hub

test:
	go test -v -race ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/ coverage.out coverage.html

run-molu:
	go run ./cmd/molu

run-hub:
	go run ./cmd/molu-hub
