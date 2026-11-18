#!/usr/bin/env bash
# iutui installer.  Usage: ./install.sh [--uninstall]
# PREFIX (default ~/.local) and BINDIR (default $PREFIX/bin) are honoured.
set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
LIBDIR="${LIBDIR:-$PREFIX/lib/yt}"
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIBEXEC_FILES=(yt yt-fmt yt-thumb-preview)
LEGACY_BINS=(yt yt-fmt yt-thumb-preview yt-tui ytx)

if [ "${1:-}" = "--uninstall" ]; then
    for f in "${LEGACY_BINS[@]}" iutui uitui; do rm -f "$BINDIR/$f"; done
    rm -rf "$LIBDIR"
    echo "iutui removed from $BINDIR (config, cache and history were left untouched)"
    exit 0
fi

if [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
    echo "iutui needs bash >= 4 (macOS: brew install bash)" >&2
    exit 1
fi

echo "==> Installing iutui into $BINDIR"
mkdir -p "$BINDIR" "$LIBDIR"
for f in "${LEGACY_BINS[@]}"; do rm -f "$BINDIR/$f"; done
for f in "${LIBEXEC_FILES[@]}"; do install -m 755 "$SRC/bin/$f" "$LIBDIR/$f"; done
install -m 644 "$SRC"/lib/*.sh "$LIBDIR/"

if command -v go >/dev/null 2>&1 && [ -d "$SRC/cmd/iutui" ]; then
    echo "==> Building iutui binary"
    (cd "$SRC" && go build -o "$SRC/bin/iutui" ./cmd/iutui)
    install -m 755 "$SRC/bin/iutui" "$BINDIR/iutui"
elif [ -x "$SRC/bin/iutui" ]; then
    install -m 755 "$SRC/bin/iutui" "$BINDIR/iutui"
fi
if [ -x "$BINDIR/iutui" ]; then
    ln -sf iutui "$BINDIR/uitui"
fi

IUTUI_CONFIG_DEST="${XDG_CONFIG_HOME:-$HOME/.config}/iutui"
if [ ! -f "$IUTUI_CONFIG_DEST/config" ]; then
    mkdir -p "$IUTUI_CONFIG_DEST"
    cp "$SRC/config.example" "$IUTUI_CONFIG_DEST/config"
    echo "==> Default config written to $IUTUI_CONFIG_DEST/config"
fi

CONFIG_DEST="${XDG_CONFIG_HOME:-$HOME/.config}/yt"
if [ ! -f "$CONFIG_DEST/config" ]; then
    mkdir -p "$CONFIG_DEST"
    cp "$SRC/config.example" "$CONFIG_DEST/config"
    echo "==> Default config written to $CONFIG_DEST/config"
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
for dep in yt-dlp mpv curl python3; do
    if command -v "$dep" >/dev/null 2>&1; then echo "  [ok] $dep"
    else echo "  [!!] $dep"; missing+=("$dep"); fi
done
if [ "${#missing[@]}" -gt 0 ]; then
    echo "     $(hint "${missing[*]}")"
fi

if ! command -v deno >/dev/null 2>&1 && ! command -v node >/dev/null 2>&1 && ! command -v bun >/dev/null 2>&1; then
    echo "  [!!] JS runtime: optional for Android fast path; needed by yt-dlp web fallback (deno recommended: https://deno.com)"
fi

case ":$PATH:" in *":$BINDIR:"*) ;; *) echo "  [i] add $BINDIR to your PATH" ;; esac

echo
if [ -x "$BINDIR/iutui" ]; then
    echo "Done. Run 'iutui doctor' to verify your setup, then 'iutui'."
else
    echo "Done. Run 'yt --doctor' to verify your setup, then 'yt'."
fi
