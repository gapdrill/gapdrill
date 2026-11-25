GO ?= go
BIN := bin/gapdrill
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/gapdrill/gapdrill/internal/version.Version=$(VERSION)

.PHONY: build test lint e2e demo fmt clean

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/gapdrill

test:
	$(GO) test -race -count=1 ./...

lint:
	golangci-lint run

e2e:
	$(GO) test -tags e2e -count=1 -timeout 30m -v ./test/e2e/...

demo: build
	cd examples/demo && ../../$(BIN) run -f gapdrill.yaml

fmt:
	gofmt -s -w .

clean:
	rm -rf bin gapdrill-report
