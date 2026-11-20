#!/usr/bin/env bash
set -euo pipefail

# iutui universal installer for Linux and macOS.
# Works via: curl -fsSL https://raw.githubusercontent.com/e12jassir/iutui/main/install.sh | bash
# Or from a local checkout: ./install.sh

PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
LIBDIR="${LIBDIR:-$PREFIX/lib/yt}"
REPO="e12jassir/iutui"
LEGACY_BINS=(yt yt-fmt yt-thumb-preview yt-tui ytx)

SPECIFIED_VERSION=""
TAG=""
CLEAN_VER=""
AUTO_YES=0
NO_DEPS=0
UNINSTALL=0

print_usage() {
  cat <<'EOF'
iutui Universal Installer

Usage:
  install.sh [options]

Options:
  -v, --version <v>   Install a specific release version (e.g. 0.5.1 or v0.5.1)
  -y, --yes           Non-interactive mode (accept dependency prompts automatically)
      --no-deps       Install binary only; skip dependency checks and installation
      --uninstall     Remove iutui, uitui, legacy binaries, and LIBDIR (keep config/cache)
  -h, --help          Show this help message

Environment variables:
  PREFIX              Installation prefix (default: ~/.local)
  BINDIR              Binary directory (default: $PREFIX/bin)
  LIBDIR              Legacy library directory to clean up (default: $PREFIX/lib/yt)
  IUTUI_SKIP_CHECKSUM Set to 1 to bypass SHA-256 verification for release tarballs
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -v|--version)
      if [ $# -ge 2 ] && [ -n "$2" ] && [ "${2#-}" = "$2" ]; then
        SPECIFIED_VERSION="$2"
        shift 2
      else
        echo "❌ Error: $1 requires a version value (e.g., --version 0.5.1)" >&2
        exit 1
      fi
      ;;
    --version=*)
      SPECIFIED_VERSION="${1#*=}"
      if [ -z "$SPECIFIED_VERSION" ]; then
        echo "❌ Error: --version= requires a non-empty version value" >&2
        exit 1
      fi
      shift
      ;;
    -y|--yes)
      AUTO_YES=1
      shift
      ;;
    --no-deps)
      NO_DEPS=1
      shift
      ;;
    --uninstall)
      UNINSTALL=1
      shift
      ;;
    -h|--help)
      print_usage
      exit 0
      ;;
    *)
      echo "⚠️  Unknown option '$1' ignored." >&2
      shift
      ;;
  esac
done

if [ "$UNINSTALL" -eq 1 ]; then
  for f in "${LEGACY_BINS[@]}" iutui uitui; do
    rm -f "$BINDIR/$f"
  done
  rm -rf "$LIBDIR"
  echo "iutui removed from $BINDIR (config, cache and state were left untouched)"
  exit 0
fi

SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
if [ -n "$SCRIPT_SOURCE" ] && [ -e "$SCRIPT_SOURCE" ]; then
  SRC="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
else
  SRC=""
fi

TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT

# Detect Operating System and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    echo "❌ Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

case "$OS" in
  linux)
    OS="linux"
    ;;
  darwin)
    OS="darwin"
    ;;
  *)
    echo "❌ Unsupported operating system: $OS (supported: linux, darwin)" >&2
    exit 1
    ;;
esac

# ==============================================================================
# Download Integrity Verification
# ==============================================================================

sha256_of() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    return 1
  fi
}

# Verify a downloaded tarball against sibling checksums.txt.
# Returns: 0 = verified, 1 = fatal (do not install), 2 = skipped via IUTUI_SKIP_CHECKSUM=1.
verify_checksum() {
  local file="$1"
  local url="$2"
  local base checksums_url checksums_file expected actual

  base="$(basename "$url")"
  checksums_url="$(dirname "$url")/checksums.txt"
  checksums_file="$TEMP_DIR/checksums.txt"

  if ! curl -fsSL -o "$checksums_file" "$checksums_url" 2>/dev/null; then
    echo "⚠️  Warning: could not download checksums.txt from: $checksums_url" >&2
    if [ "${IUTUI_SKIP_CHECKSUM:-0}" = "1" ]; then
      echo "⚠️  IUTUI_SKIP_CHECKSUM=1 is set: skipping integrity verification." >&2
      return 2
    fi
    echo "❌ Error: unable to verify the integrity of the downloaded asset (checksums.txt unavailable)." >&2
    echo "   Refusing to install an unverified binary. Re-run with IUTUI_SKIP_CHECKSUM=1 to bypass this check." >&2
    return 1
  fi

  expected="$(awk -v name="$base" '{ n=$2; sub(/^\*/, "", n); if (n == name) { print $1; exit } }' "$checksums_file")"
  if [ -z "$expected" ]; then
    echo "⚠️  Warning: no checksum entry found for '$base' in checksums.txt." >&2
    if [ "${IUTUI_SKIP_CHECKSUM:-0}" = "1" ]; then
      echo "⚠️  IUTUI_SKIP_CHECKSUM=1 is set: proceeding without a checksum for this asset." >&2
      return 2
    fi
    echo "❌ Error: refusing to install '$base' without a matching checksum entry." >&2
    echo "   Re-run with IUTUI_SKIP_CHECKSUM=1 to bypass this check." >&2
    return 1
  fi

  if ! actual="$(sha256_of "$file")"; then
    echo "⚠️  Warning: no SHA-256 tool found on this system (need 'sha256sum' or 'shasum')." >&2
    if [ "${IUTUI_SKIP_CHECKSUM:-0}" = "1" ]; then
      echo "⚠️  IUTUI_SKIP_CHECKSUM=1 is set: proceeding without checksum verification." >&2
      return 2
    fi
    echo "❌ Error: cannot compute SHA-256 to verify the download." >&2
    echo "   Install 'coreutils' (sha256sum) or 'shasum'. Re-run with IUTUI_SKIP_CHECKSUM=1 to bypass." >&2
    return 1
  fi

  if [ "$actual" != "$expected" ]; then
    echo "❌ Error: checksum mismatch for '$base'!" >&2
    echo "   expected: $expected" >&2
    echo "   actual:   $actual" >&2
    echo "   The downloaded file is corrupted or has been tampered with. Aborting." >&2
    return 1
  fi

  echo "✅ Checksum verified: $base"
  return 0
}

# ==============================================================================
# Build / Download & Install Binary
# ==============================================================================

echo "==> Installing iutui into $BINDIR"
mkdir -p "$BINDIR"

INSTALLED=0

# a) Local checkout build/prebuilt when no explicit version was requested
if [ -z "$SPECIFIED_VERSION" ] && [ -n "$SRC" ] && [ -d "$SRC/cmd/iutui" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "==> Building iutui Go binary from local checkout"
    (cd "$SRC" && CGO_ENABLED=0 go build -trimpath -o "$TEMP_DIR/iutui" ./cmd/iutui)
    install -m 755 "$TEMP_DIR/iutui" "$BINDIR/iutui"
    INSTALLED=1
  elif [ -x "$SRC/bin/iutui" ]; then
    echo "==> Installing prebuilt binary from $SRC/bin/iutui"
    install -m 755 "$SRC/bin/iutui" "$BINDIR/iutui"
    INSTALLED=1
  fi
fi

# b) Remote release download or git+go fallback
if [ "$INSTALLED" -eq 0 ]; then
  if [ -n "$SPECIFIED_VERSION" ]; then
    CLEAN_VER="${SPECIFIED_VERSION#v}"
    TAG="v${CLEAN_VER}"
    echo "==> Installing iutui ${TAG} for ${OS}/${ARCH}..."
  else
    LATEST_TAG=""
    if command -v curl >/dev/null 2>&1; then
      LATEST_TAG="$(curl -fsSL -H "User-Agent: iutui-installer" "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep -o '"tag_name": *"[^"]*"' | head -n1 | cut -d'"' -f4 || true)"
    fi
    if [ -n "$LATEST_TAG" ]; then
      TAG="$LATEST_TAG"
      CLEAN_VER="${LATEST_TAG#v}"
      echo "==> Installing iutui latest (${TAG}) for ${OS}/${ARCH}..."
    else
      echo "==> Installing iutui (latest) for ${OS}/${ARCH}..."
    fi
  fi

  CANDIDATE_URLS=()
  if [ -n "$TAG" ]; then
    CANDIDATE_URLS+=(
      "https://github.com/${REPO}/releases/download/${TAG}/iutui_${CLEAN_VER}_${OS}_${ARCH}.tar.gz"
      "https://github.com/${REPO}/releases/download/${TAG}/iutui_${OS}_${ARCH}.tar.gz"
    )
    if [ -z "$SPECIFIED_VERSION" ]; then
      CANDIDATE_URLS+=(
        "https://github.com/${REPO}/releases/latest/download/iutui_${CLEAN_VER}_${OS}_${ARCH}.tar.gz"
      )
    fi
  else
    CANDIDATE_URLS+=(
      "https://github.com/${REPO}/releases/latest/download/iutui_${OS}_${ARCH}.tar.gz"
    )
  fi

  if command -v curl >/dev/null 2>&1; then
    for url in "${CANDIDATE_URLS[@]}"; do
      if curl -fsSL -o "$TEMP_DIR/iutui.tar.gz" "$url" 2>/dev/null; then
        VERIFY_RC=0
        verify_checksum "$TEMP_DIR/iutui.tar.gz" "$url" || VERIFY_RC=$?
        if [ "$VERIFY_RC" -eq 1 ]; then
          echo "❌ Integrity verification failed for: $url" >&2
          exit 1
        fi
        if tar -xzf "$TEMP_DIR/iutui.tar.gz" -C "$TEMP_DIR" 2>/dev/null; then
          if [ -f "$TEMP_DIR/iutui" ]; then
            install -m 755 "$TEMP_DIR/iutui" "$BINDIR/iutui"
            INSTALLED=1
            break
          fi
        fi
      fi
    done
  fi

  if [ "$INSTALLED" -eq 0 ]; then
    echo "ℹ️  Release asset not found. Building from source via Git..."
    if ! command -v git >/dev/null 2>&1 || ! command -v go >/dev/null 2>&1; then
      echo "❌ Error: git and Go 1.24+ are required to build from source when precompiled assets are not found." >&2
      echo "   Install git and Go, or download release packages from: https://github.com/${REPO}/releases" >&2
      exit 1
    fi
    if [ -n "$TAG" ]; then
      git clone --depth 1 --branch "$TAG" "https://github.com/${REPO}.git" "$TEMP_DIR/repo"
    else
      git clone --depth 1 "https://github.com/${REPO}.git" "$TEMP_DIR/repo"
    fi
    (cd "$TEMP_DIR/repo" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${TAG:-dev}" -o "$BINDIR/iutui" ./cmd/iutui)
  fi
fi

# Clean legacy binaries/libdir and create uitui alias
for f in "${LEGACY_BINS[@]}"; do
  rm -f "$BINDIR/$f"
done
rm -rf "$LIBDIR"
ln -sf iutui "$BINDIR/uitui"

echo "✅ iutui installed successfully at: $BINDIR/iutui"

# Seed default config if missing
IUTUI_CONFIG_DEST="${XDG_CONFIG_HOME:-$HOME/.config}/iutui"
if [ ! -f "$IUTUI_CONFIG_DEST/config" ]; then
  mkdir -p "$IUTUI_CONFIG_DEST"
  if [ -n "$SRC" ] && [ -f "$SRC/config.example" ]; then
    cp "$SRC/config.example" "$IUTUI_CONFIG_DEST/config"
    echo "==> Default config written to $IUTUI_CONFIG_DEST/config"
  elif command -v curl >/dev/null 2>&1; then
    if curl -fsSL -o "$IUTUI_CONFIG_DEST/config" "https://raw.githubusercontent.com/${REPO}/main/config.example" 2>/dev/null && [ -s "$IUTUI_CONFIG_DEST/config" ]; then
      echo "==> Default config written to $IUTUI_CONFIG_DEST/config"
    else
      rm -f "$IUTUI_CONFIG_DEST/config"
    fi
  fi
fi

# ==============================================================================
# Dependency Verification and Automated Installation
# ==============================================================================

run_elevated() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    if [ -c /dev/tty ] && { true </dev/tty; } 2>/dev/null; then
      sudo "$@" </dev/tty
    else
      sudo "$@"
    fi
  else
    return 1
  fi
}

prompt_consent() {
  local prompt_msg="$1"

  if [ "$AUTO_YES" -eq 1 ]; then
    return 0
  fi

  local answer=""
  if [ -c /dev/tty ] && { true </dev/tty; } 2>/dev/null; then
    read -r -p "$prompt_msg " answer </dev/tty || return 1
  elif [ -t 0 ]; then
    read -r -p "$prompt_msg " answer || return 1
  else
    return 1
  fi

  case "$answer" in
    ""|[Yy]|[Yy][Ee][Ss])
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

print_manual_instructions() {
  local pm="$1"
  shift
  local pkgs=("$@")

  echo "   You can install missing dependencies manually:"
  case "$pm" in
    apt)    echo "   sudo apt-get update && sudo apt-get install -y ${pkgs[*]}" ;;
    pacman) echo "   sudo pacman -S --needed ${pkgs[*]}" ;;
    dnf)    echo "   sudo dnf install -y ${pkgs[*]}" ;;
    zypper) echo "   sudo zypper install -y ${pkgs[*]}" ;;
    apk)    echo "   sudo apk add ${pkgs[*]}" ;;
    brew)   echo "   brew install ${pkgs[*]}" ;;
    *)      echo "   Install packages: ${pkgs[*]}" ;;
  esac
}

report_optional_deps() {
  echo "  [ok] thumbnails (native Go renderer: Kitty HD in Ghostty/Kitty, truecolor ANSI elsewhere)"

  if command -v deno >/dev/null 2>&1 || command -v node >/dev/null 2>&1 || command -v bun >/dev/null 2>&1; then
    echo "  [ok] JS runtime (optional yt-dlp web extractor fallback)"
  else
    echo "  [..] JS runtime (optional: deno/node/bun for yt-dlp web fallback)"
  fi

  if [ "$OS" = "linux" ]; then
    if command -v playerctl >/dev/null 2>&1; then
      echo "  [ok] playerctl (optional MPRIS2 media key control)"
    else
      echo "  [..] playerctl (optional: MPRIS2 media controls on Linux)"
    fi
  fi
}

ensure_dependencies() {
  local missing=()
  local dep
  for dep in mpv yt-dlp; do
    if ! command -v "$dep" >/dev/null 2>&1; then
      missing+=("$dep")
    fi
  done

  if [ "${#missing[@]}" -eq 0 ]; then
    echo "✅ System dependencies are up to date (mpv and yt-dlp ready)."
    report_optional_deps
    return 0
  fi

  echo ""
  echo "Checking system dependencies..."

  if [ "$OS" = "darwin" ]; then
    if command -v brew >/dev/null 2>&1; then
      if prompt_consent "Missing dependencies (${missing[*]}). Install via Homebrew? [Y/n]"; then
        echo "Installing ${missing[*]} with Homebrew..."
        if ! brew install "${missing[@]}"; then
          echo "⚠️  Warning: Could not install dependencies automatically." >&2
          print_manual_instructions "brew" "${missing[@]}"
        fi
      else
        echo "Skipped automatic dependency installation."
        print_manual_instructions "brew" "${missing[@]}"
      fi
    else
      echo "⚠️  Warning: Homebrew not found." >&2
      print_manual_instructions "brew" "${missing[@]}"
    fi
    report_optional_deps
    return 0
  fi

  # Linux Distribution Detection
  local distro="unknown"
  local distro_like=""
  if [ -f /etc/os-release ]; then
    # shellcheck source=/dev/null
    distro="$( ( . /etc/os-release && printf '%s' "${ID:-unknown}" ) )"
    # shellcheck source=/dev/null
    distro_like="$( ( . /etc/os-release && printf '%s' "${ID_LIKE:-}" ) )"
  elif [ -f /etc/debian_version ]; then
    distro="debian"
  elif [ -f /etc/arch-release ]; then
    distro="arch"
  elif [ -f /etc/fedora-release ]; then
    distro="fedora"
  fi

  distro="$(printf '%s' "$distro" | tr '[:upper:]' '[:lower:]')"

  local pkg_manager=""
  case "$distro" in
    ubuntu|debian|pop|linuxmint|zorin|elementary)
      pkg_manager="apt"
      ;;
    arch|manjaro|endeavouros|artix|cachyos)
      pkg_manager="pacman"
      ;;
    fedora|rhel|centos|almalinux|rocky)
      pkg_manager="dnf"
      ;;
    opensuse*|sles)
      pkg_manager="zypper"
      ;;
    alpine)
      pkg_manager="apk"
      ;;
    *)
      local distro_like_lc
      distro_like_lc="$(printf '%s' "$distro_like" | tr '[:upper:]' '[:lower:]')"
      case "$distro_like_lc" in
        *debian*)
          pkg_manager="apt"
          ;;
        *arch*)
          pkg_manager="pacman"
          ;;
        *fedora*|*rhel*)
          pkg_manager="dnf"
          ;;
        *suse*)
          pkg_manager="zypper"
          ;;
      esac
      ;;
  esac

  echo "Missing required dependencies on ${distro}: ${missing[*]}"

  if [ -z "$pkg_manager" ]; then
    echo "⚠️  Warning: Package manager not recognized for ${distro}." >&2
    print_manual_instructions "" "${missing[@]}"
    report_optional_deps
    return 0
  fi

  if ! prompt_consent "Install missing dependencies (${missing[*]}) using ${pkg_manager}? [Y/n]"; then
    echo "Skipping automatic dependency installation."
    print_manual_instructions "$pkg_manager" "${missing[@]}"
    report_optional_deps
    return 0
  fi

  echo "Installing dependencies..."
  local success=0
  case "$pkg_manager" in
    apt)
      if run_elevated apt-get update -y && run_elevated apt-get install -y "${missing[@]}"; then
        success=1
      fi
      ;;
    pacman)
      local pacman_args=(pacman -S --needed)
      if [ "$AUTO_YES" -eq 1 ]; then
        pacman_args+=(--noconfirm)
      fi
      if run_elevated "${pacman_args[@]}" "${missing[@]}"; then
        success=1
      fi
      ;;
    dnf)
      if run_elevated dnf install -y "${missing[@]}"; then
        success=1
      else
        echo "⚠️  Note: On Fedora/RHEL, 'mpv' requires the RPM Fusion Free repository:" >&2
        echo "   https://rpmfusion.org/Configuration" >&2
      fi
      ;;
    zypper)
      if run_elevated zypper install -y "${missing[@]}"; then
        success=1
      fi
      ;;
    apk)
      if run_elevated apk add "${missing[@]}"; then
        success=1
      fi
      ;;
  esac

  if [ "$success" -eq 1 ]; then
    echo "✅ Dependencies installed successfully."
  else
    echo "⚠️  Warning: Could not install dependencies automatically." >&2
    print_manual_instructions "$pkg_manager" "${missing[@]}"
  fi

  report_optional_deps
}

if [ "$NO_DEPS" -eq 0 ]; then
  ensure_dependencies
fi

# Verify PATH configuration
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *)
    echo "⚠️  Note: Ensure $BINDIR is in your PATH:"
    echo "   export PATH=\"$BINDIR:\$PATH\""
    ;;
esac

echo ""
"$BINDIR/iutui" doctor || true
