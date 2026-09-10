VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO      ?= go
GOFLAGS ?= -mod=mod
LDFLAGS := -s -w -X main.version=$(VERSION)

export CGO_ENABLED=0
export GOFLAGS

.PHONY: build test vet lint-free run-dry fixtures clean

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o recall ./cmd/recall

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# lint-free: fails when gofmt would change anything or vet complains.
lint-free: vet
	@out=$$(gofmt -l . 2>/dev/null); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# run-dry: run recall against synthesized fixtures, never touching ~/.claude.
run-dry: build
	RECALL_DRY_RUN=1 ./recall --claude-dir testdata/fixtures/claude --recall-dir /tmp/recall-dry ls

# fixtures: regenerate synthesized fixtures (see testdata/fixtures/README.md).
fixtures:
	@echo "fixtures are synthesized by hand; see testdata/fixtures/README.md"

clean:
	rm -rf recall dist
