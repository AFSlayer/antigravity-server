#!/usr/bin/env bash
set -euo pipefail

REPO="AFSlayer/antigravity-server"
BINARY_URL="${AGY_BINARY_URL:-}"
START="yes"

BOLD=$'\033[1m'
DIM=$'\033[90m'
GREEN=$'\033[32m'
RED=$'\033[31m'
CYAN=$'\033[36m'
OFF=$'\033[0m'

say() { printf '  %s\n' "$*"; }
ok() { printf '  %s✓%s %s\n' "$GREEN" "$OFF" "$*"; }
die() {
  printf '\n  %s✕%s %s\n\n' "$RED" "$OFF" "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
Antigravity Server — desktop installer

Usage: install-desktop.sh [options]

  --dir DIR      Install location (default: /usr/local/bin, or ~/.local/bin)
  --no-start     Install only, do not launch
  --help         Show this message

Because this script downloads with curl, macOS does not quarantine the binary,
so you never see the "unidentified developer" warning.
EOF
}

TARGET_DIR=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir) TARGET_DIR="${2:?--dir needs a value}"; shift 2 ;;
    --no-start) START="no"; shift ;;
    --help | -h) usage; exit 0 ;;
    *) die "Unknown option: $1 (try --help)" ;;
  esac
done

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) die "Unsupported operating system: $(uname -s). On Windows use install-desktop.ps1." ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) die "Unsupported architecture: $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required but not installed."
command -v tar >/dev/null 2>&1 || die "tar is required but not installed."

pick_target_dir() {
  if [ -n "$TARGET_DIR" ]; then
    printf '%s' "$TARGET_DIR"
    return
  fi
  if [ -w /usr/local/bin ] 2>/dev/null; then
    printf '/usr/local/bin'
  elif [ "$(id -u)" = "0" ]; then
    printf '/usr/local/bin'
  else
    printf '%s/.local/bin' "$HOME"
  fi
}

printf '\n  %sAntigravity Server%s\n\n' "$BOLD" "$OFF"

TARGET_DIR="$(pick_target_dir)"
URL="${BINARY_URL:-https://github.com/$REPO/releases/latest/download/agy-server_${os}_${arch}.tar.gz}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

say "Downloading for ${os}/${arch}…"
curl -fsSL "$URL" -o "$WORK_DIR/agy-server.tar.gz" ||
  die "Could not download $URL"

tar -xzf "$WORK_DIR/agy-server.tar.gz" -C "$WORK_DIR"
[ -f "$WORK_DIR/agy-server" ] || die "The archive did not contain agy-server."

mkdir -p "$TARGET_DIR" 2>/dev/null || true
if [ -w "$TARGET_DIR" ]; then
  install -m 0755 "$WORK_DIR/agy-server" "$TARGET_DIR/agy-server"
else
  say "Installing to $TARGET_DIR needs administrator access."
  sudo install -m 0755 "$WORK_DIR/agy-server" "$TARGET_DIR/agy-server"
fi

xattr -d com.apple.quarantine "$TARGET_DIR/agy-server" 2>/dev/null || true
ok "Installed $TARGET_DIR/agy-server"

case ":$PATH:" in
  *":$TARGET_DIR:"*) ;;
  *)
    say "${DIM}$TARGET_DIR is not on your PATH. Add it with:${OFF}"
    say "  ${CYAN}echo 'export PATH=\"$TARGET_DIR:\$PATH\"' >> ~/.zshrc${OFF}"
    ;;
esac

if [ "$START" = "yes" ]; then
  printf '\n'
  say "Starting… a control panel with a QR code will open in your browser."
  printf '\n'
  exec "$TARGET_DIR/agy-server"
fi

printf '\n  Run it with: %sagy-server%s\n\n' "$CYAN" "$OFF"
