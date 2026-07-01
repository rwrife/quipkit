BINARY := quipkit
PKG := ./cmd/quipkit
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet fmt clean

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f $(BINARY)
