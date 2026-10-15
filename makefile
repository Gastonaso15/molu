.PHONY: all build test coverage clean run-molu inspector

XOLU_URL ?= http://localhost:9090
AUTH_MODE ?= bearer
TOKEN_FILE ?= .token.dev

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
	rm -rf bin/ coverage.out coverage.html .token.dev dev-token.txt

run-molu:
	go run ./cmd/molu

inspector: build
	@if [ ! -f $(TOKEN_FILE) ]; then echo "molu-dev-token" > $(TOKEN_FILE); fi
	npx @modelcontextprotocol/inspector \
		-e MOLU_FRONT_XOLU_URL="$(XOLU_URL)" \
		-e MOLU_FRONT_XOLU_AUTH_MODE="$(AUTH_MODE)" \
		-e MOLU_FRONT_XOLU_TOKEN_FILE="$(CURDIR)/$(TOKEN_FILE)" \
		./bin/molu