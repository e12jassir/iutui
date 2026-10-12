PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
LIBDIR ?= $(PREFIX)/lib/yt
FILES  := yt yt-fmt yt-thumb-preview

.PHONY: install uninstall test lint

install:
	@mkdir -p $(BINDIR) $(LIBDIR)
	@for f in $(FILES); do install -m 755 bin/$$f $(BINDIR)/$$f; done
	@install -m 644 lib/*.sh $(LIBDIR)/
	@echo "yt installed in $(BINDIR) (libs in $(LIBDIR))"

uninstall:
	@for f in $(FILES); do rm -f $(BINDIR)/$$f; done
	@rm -rf $(LIBDIR)
	@echo "yt removed from $(BINDIR)"

test:
	@bash tests/smoke.sh

lint:
	@shellcheck -x bin/yt bin/yt-thumb-preview install.sh tests/smoke.sh
	@python3 -m py_compile bin/yt-fmt
