# lazy1mcp build/test/install. `make install` puts the binary at $(BINDIR)/lazy1mcp (default ~/.local/bin).
BINDIR ?= $(HOME)/.local/bin
VERSION ?= 0.1.0
GO ?= go
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet fmt install clean screenshots

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/lazy1mcp .

test: vet
	$(GO) test -race -count=1 ./...

vet:
	@test -z "$$(gofmt -l .)" || (gofmt -l .; echo "gofmt needed"; exit 1)
	$(GO) vet ./...

fmt:
	gofmt -w .

install:
	mkdir -p $(BINDIR)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINDIR)/lazy1mcp .
	@echo "installed $(BINDIR)/lazy1mcp"

clean:
	rm -rf bin

# README screenshots (docs/screenshots) from the VHS tapes in docs/tapes, with demo data in a sandbox
screenshots:
	bash docs/screenshots.sh
