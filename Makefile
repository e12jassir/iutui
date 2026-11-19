PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
LIBDIR ?= $(PREFIX)/lib/yt
LEGACY_BINS := yt yt-fmt yt-thumb-preview yt-tui ytx

.PHONY: install uninstall test build clean lint verify

build:
	@mkdir -p bin
	@go build -o bin/iutui ./cmd/iutui

clean:
	@rm -f bin/iutui bin/ytx
	@rmdir bin 2>/dev/null || true

install: build
	@mkdir -p $(BINDIR)
	@for f in $(LEGACY_BINS); do rm -f $(BINDIR)/$$f; done
	@rm -rf $(LIBDIR)
	@install -m 755 bin/iutui $(BINDIR)/iutui
	@ln -sf iutui $(BINDIR)/uitui
	@echo "iutui (and uitui alias) installed in $(BINDIR)"

uninstall:
	@for f in $(LEGACY_BINS) iutui uitui; do rm -f $(BINDIR)/$$f; done
	@rm -rf $(LIBDIR)
	@echo "iutui removed from $(BINDIR)"

test:
	@go test -race ./...

lint:
	@test -z "$$(gofmt -l .)" && go vet ./...

verify: lint test
