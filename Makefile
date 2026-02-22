.PHONY: build run test clean ui

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build: ui
	go build -ldflags "-X main.version=$(VERSION)" -o bin/praktor ./cmd/praktor

run:
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/praktor gateway

test:
	go test ./...

clean:
	rm -rf bin/ ui/dist/

# Build the React UI
ui:
	cd ui && npm install && npm run build

# Development: run without building UI
dev:
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/praktor gateway

version:
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/praktor version
