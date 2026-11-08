PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
LIBDIR ?= $(PREFIX)/lib/yt
FILES  := yt yt-fmt yt-thumb-preview yt-tui

.PHONY: install uninstall test test-bash test-go build clean lint verify

build:
	@go build -o bin/yt-tui ./cmd/yt-tui

clean:
	@rm -f bin/yt-tui

install: build
	@mkdir -p $(BINDIR) $(LIBDIR)
	@for f in $(FILES); do install -m 755 bin/$$f $(BINDIR)/$$f; done
	@install -m 644 lib/*.sh $(LIBDIR)/
	@echo "yt installed in $(BINDIR) (libs in $(LIBDIR))"

uninstall:
	@for f in $(FILES); do rm -f $(BINDIR)/$$f; done
	@rm -rf $(LIBDIR)
	@echo "yt removed from $(BINDIR)"

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
