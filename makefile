.PHONY: all build test coverage clean run-molu inspector xolu-up xolu-down

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

# xolu local con datos de demo de Seam. El script se elige según el sistema
# (macOS, Windows con Git Bash, o el base para Linux); se puede forzar con
# XOLU_SCRIPT=scripts/levantar-xolu-macos.sh. Detalles en cada script.
# Ejemplo: make xolu-up && make inspector XOLU_URL=http://127.0.0.1:9191
# (el puerto por defecto es 9191 en macOS y 9090 en los otros dos scripts;
# lo imprime xolu-up al terminar)
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
XOLU_SCRIPT ?= ./scripts/levantar-xolu-macos.sh
else ifneq (,$(filter MINGW% MSYS% CYGWIN%,$(UNAME_S)))
XOLU_SCRIPT ?= ./scripts/levantar-xolu-windows.sh
else
XOLU_SCRIPT ?= ./scripts/levantar-xolu.sh
endif

xolu-up:
	$(XOLU_SCRIPT)

xolu-down:
	$(XOLU_SCRIPT) stop
