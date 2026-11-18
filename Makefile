PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
LIBDIR ?= $(PREFIX)/lib/yt
LIBEXEC_FILES := yt yt-fmt yt-thumb-preview
LEGACY_BINS := yt yt-fmt yt-thumb-preview yt-tui ytx

.PHONY: install uninstall test test-bash test-go build clean lint verify

build:
	@go build -o bin/iutui ./cmd/iutui

clean:
	@rm -f bin/iutui bin/ytx

install: build
	@mkdir -p $(BINDIR) $(LIBDIR)
	@for f in $(LEGACY_BINS); do rm -f $(BINDIR)/$$f; done
	@install -m 755 bin/iutui $(BINDIR)/iutui
	@ln -sf iutui $(BINDIR)/uitui
	@for f in $(LIBEXEC_FILES); do install -m 755 bin/$$f $(LIBDIR)/$$f; done
	@install -m 644 lib/*.sh $(LIBDIR)/
	@echo "iutui (and uitui alias) installed in $(BINDIR) (libs in $(LIBDIR))"

uninstall:
	@for f in $(LEGACY_BINS) iutui uitui; do rm -f $(BINDIR)/$$f; done
	@rm -rf $(LIBDIR)
	@echo "iutui removed from $(BINDIR)"

test-bash:
	@bash tests/smoke.sh

test-go:
	@go test -v ./...

test: test-bash test-go

verify: lint test

lint:
	@for s in bin/yt bin/yt-thumb-preview install.sh tests/smoke.sh lib/*.sh; do bash -n "$$s" || exit 1; done
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck -x bin/yt bin/yt-thumb-preview install.sh tests/smoke.sh; fi
	@python3 -m py_compile bin/yt-fmt
