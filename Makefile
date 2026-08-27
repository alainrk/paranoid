MODULE  := github.com/alainrk/paranoid
BINARY  := paranoid
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/cli.version=$(VERSION) \
	-X $(MODULE)/internal/cli.commit=$(COMMIT) \
	-X $(MODULE)/internal/cli.date=$(DATE)

.PHONY: build test lint check fixtures docs smoke-live clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/paranoid

test:
	go test -race ./...

lint:
	golangci-lint run

check: lint test build fixtures

# Run only the golden fixture suite. `make test` runs it too, as part
# of the full test run.
fixtures:
	go test -race -run TestFixtures -count=1 ./internal/cli/

# Regenerate docs/rules.md from the rule registry.
docs:
	go run ./hack/gen-rules-docs

# Live smoke test against the real registries. Not part of check or CI.
smoke-live:
	go test -tags smokelive -count=1 -run TestLiveLookups ./internal/registry/

clean:
	rm -rf bin dist
