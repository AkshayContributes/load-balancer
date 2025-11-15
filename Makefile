.PHONY: help build run clean test fmt vet deps

# Variables
BINARY_NAME=load-balancer
BINARY_PATH=bin/$(BINARY_NAME)
CMD_PATH=cmd/lb
GO=go
GOFLAGS=-v

help:
	@echo "Load Balancer - Available targets:"
	@echo "  make build    - Build the load balancer binary"
	@echo "  make run      - Build and run the load balancer"
	@echo "  make test     - Run tests"
	@echo "  make fmt      - Format code with gofmt"
	@echo "  make vet      - Run go vet"
	@echo "  make deps     - Download dependencies"
	@echo "  make clean    - Remove build artifacts"

deps:
	$(GO) mod download
	$(GO) mod verify

build: deps
	$(GO) build $(GOFLAGS) -o $(BINARY_PATH) ./cmd

run: build
	./$(BINARY_PATH)

test:
	$(GO) test $(GOFLAGS) -cover ./...

fmt:
	$(GO) fmt ./...

vet: fmt
	$(GO) vet ./...

clean:
	$(GO) clean
	rm -rf bin/
	rm -f $(BINARY_NAME)

.DEFAULT_GOAL := help
