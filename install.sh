#!/usr/bin/env bash
# iutui installer. Usage: ./install.sh [--uninstall]
# PREFIX (default ~/.local) and BINDIR (default $PREFIX/bin) are honoured.
set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
LIBDIR="${LIBDIR:-$PREFIX/lib/yt}"
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LEGACY_BINS=(yt yt-fmt yt-thumb-preview yt-tui ytx)

if [ "${1:-}" = "--uninstall" ]; then
    for f in "${LEGACY_BINS[@]}" iutui uitui; do rm -f "$BINDIR/$f"; done
    rm -rf "$LIBDIR"
    echo "iutui removed from $BINDIR (config, cache and history were left untouched)"
    exit 0
fi

echo "==> Installing iutui into $BINDIR"
mkdir -p "$BINDIR"
for f in "${LEGACY_BINS[@]}"; do rm -f "$BINDIR/$f"; done
rm -rf "$LIBDIR"

tmp_bin=""
cleanup() {
    if [ -n "$tmp_bin" ]; then
        rm -f "$tmp_bin"
    fi
}
trap cleanup EXIT

if command -v go >/dev/null 2>&1 && [ -d "$SRC/cmd/iutui" ]; then
    echo "==> Building iutui Go binary"
    tmp_bin="$(mktemp)"
    (cd "$SRC" && go build -o "$tmp_bin" ./cmd/iutui)
    install -m 755 "$tmp_bin" "$BINDIR/iutui"
elif [ -x "$SRC/bin/iutui" ]; then
    install -m 755 "$SRC/bin/iutui" "$BINDIR/iutui"
else
    echo "error: Go toolchain not found in PATH and no prebuilt bin/iutui binary found." >&2
    echo "       Install Go 1.24+ from https://go.dev/dl/ or download a prebuilt release." >&2
    exit 1
fi

ln -sf iutui "$BINDIR/uitui"

IUTUI_CONFIG_DEST="${XDG_CONFIG_HOME:-$HOME/.config}/iutui"
if [ ! -f "$IUTUI_CONFIG_DEST/config" ] && [ -f "$SRC/config.example" ]; then
    mkdir -p "$IUTUI_CONFIG_DEST"
    cp "$SRC/config.example" "$IUTUI_CONFIG_DEST/config"
    echo "==> Default config written to $IUTUI_CONFIG_DEST/config"
fi

hint() {
    if   command -v pacman  >/dev/null 2>&1; then echo "sudo pacman -S $*"
    elif command -v apt-get >/dev/null 2>&1; then echo "sudo apt install $*"
    elif command -v dnf     >/dev/null 2>&1; then echo "sudo dnf install $*"
    elif command -v brew    >/dev/null 2>&1; then echo "brew install $*"
    else echo "install: $*"; fi
}

echo "==> Checking dependencies"
missing=()
for dep in mpv yt-dlp; do
    if command -v "$dep" >/dev/null 2>&1; then
        echo "  [ok] $dep (required)"
    else
        echo "  [!!] $dep (required)"
        missing+=("$dep")
    fi
done
if [ "${#missing[@]}" -gt 0 ]; then
    echo "     $(hint "${missing[*]}")"
fi

if command -v chafa >/dev/null 2>&1; then
    echo "  [ok] chafa (optional ANSI thumbnail fallback)"
else
    echo "  [..] chafa (optional: ANSI thumbnails for non-Kitty/Ghostty terminals)"
fi

if command -v deno >/dev/null 2>&1 || command -v node >/dev/null 2>&1 || command -v bun >/dev/null 2>&1; then
    echo "  [ok] JS runtime (optional yt-dlp web extractor fallback)"
else
    echo "  [..] JS runtime (optional: deno/node/bun for yt-dlp web fallback)"
fi

if [ "$(uname -s)" = "Linux" ]; then
    if command -v playerctl >/dev/null 2>&1; then
        echo "  [ok] playerctl (optional MPRIS2 media key control)"
    else
        echo "  [..] playerctl (optional: MPRIS2 media controls on Linux)"
    fi
fi

case ":$PATH:" in
    *":$BINDIR:"*) ;;
    *) echo "  [i] add $BINDIR to your PATH" ;;
esac

echo
echo "Done. Run 'iutui doctor' to verify your setup, then 'iutui'."
