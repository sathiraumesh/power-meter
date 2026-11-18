BINARY  := power-meter
PKG     := sathiraumesh/power-meter
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/internal/cli.version=$(VERSION)

.PHONY: all build run test fmt vet tidy clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

run: build
	./$(BIN_DIR)/$(BINARY) $(ARGS)

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
