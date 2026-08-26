#!/usr/bin/env bash
set -euo pipefail

REPO="AFSlayer/antigravity-server"
DOWNLOAD_PAGE="https://antigravity.google/download"

DEFAULT_LS_DIR="/opt/agy-server"

# The project used to be called agy-remote. Installs from that era keep a 170 MB
# language_server and a systemd unit under the old name, and both have to be
# accounted for or an upgrade downloads it all again and then fights itself for
# the port.
LEGACY_NAME="agy-remote"
LEGACY_LS_DIR="/opt/agy-remote"

PREFIX="${AGY_INSTALL_PREFIX:-/usr/local/bin}"
LS_DIR="${AGY_LS_DIR:-$DEFAULT_LS_DIR}"
BINARY_URL="${AGY_BINARY_URL:-}"

# Asking for a directory that happens to equal the default is not the same as
# saying nothing, so only the latter may fall back to the pre-rename location.
LS_DIR_EXPLICIT="no"
if [ -n "${AGY_LS_DIR:-}" ]; then
  LS_DIR_EXPLICIT="yes"
fi

DOMAIN=""
PASSWORD=""
WORKSPACE_ROOT=""
PORT="8765"
SERVICE_NAME="agy-server"
WORK_DIR=""
ASSUME_YES="no"
WANT_SERVICE="yes"
WANT_CADDY="auto"

BOLD=$'\033[1m'
DIM=$'\033[90m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RED=$'\033[31m'
CYAN=$'\033[36m'
OFF=$'\033[0m'

say() { printf '  %s\n' "$*"; }
ok() { printf '  %s✓%s %s\n' "$GREEN" "$OFF" "$*"; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$OFF" "$*"; }
die() {
  printf '\n  %s✕%s %s\n\n' "$RED" "$OFF" "$*" >&2
  exit 1
}
heading() { printf '\n  %s%s%s\n' "$BOLD" "$*" "$OFF"; }

usage() {
  cat <<EOF
Antigravity Server installer

Usage: install.sh [options]

  --domain HOST         Serve over HTTPS on this domain (sets up Caddy)
  --password SECRET     Access password (generated if omitted)
  --workspace-root DIR  Where new projects are created (default \$HOME/workspace)
  --port N              Local port for agy-server (default 8765)
  --prefix DIR          Where to install the agy-server binary (default /usr/local/bin)
  --ls-dir DIR          Where to install language_server (default $DEFAULT_LS_DIR)
  --service-name NAME   systemd unit name (default agy-server)
  --no-service          Skip the systemd unit
  --no-caddy            Skip Caddy even when --domain is given
  --yes                 Never prompt

Environment:
  AGY_BINARY_URL        Override the agy-server download URL
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --domain) DOMAIN="${2:?--domain needs a value}"; shift 2 ;;
    --password) PASSWORD="${2:?--password needs a value}"; shift 2 ;;
    --workspace-root) WORKSPACE_ROOT="${2:?--workspace-root needs a value}"; shift 2 ;;
    --port) PORT="${2:?--port needs a value}"; shift 2 ;;
    --prefix) PREFIX="${2:?--prefix needs a value}"; shift 2 ;;
    --ls-dir) LS_DIR="${2:?--ls-dir needs a value}"; LS_DIR_EXPLICIT="yes"; shift 2 ;;
    --service-name) SERVICE_NAME="${2:?--service-name needs a value}"; shift 2 ;;
    --no-service) WANT_SERVICE="no"; shift ;;
    --no-caddy) WANT_CADDY="no"; shift ;;
    --yes|-y) ASSUME_YES="yes"; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Unknown option: $1 (try --help)" ;;
  esac
done

require() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed."
}

detect_platform() {
  [ "$(uname -s)" = "Linux" ] || die "The server installer supports Linux only. On macOS or Windows, just run agy-server."

  case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64"; HUB_SLUG="linux-x64" ;;
    aarch64|arm64) ARCH="arm64"; HUB_SLUG="linux-arm" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
}

sudo_run() {
  if [ "$(id -u)" = "0" ]; then
    "$@"
  else
    require sudo
    sudo "$@"
  fi
}

ask() {
  local prompt="$1" default="${2:-}" answer
  if [ "$ASSUME_YES" = "yes" ] || [ ! -t 0 ]; then
    printf '%s' "$default"
    return
  fi
  if [ -n "$default" ]; then
    read -r -p "  $prompt [$default]: " answer </dev/tty || answer=""
  else
    read -r -p "  $prompt: " answer </dev/tty || answer=""
  fi
  printf '%s' "${answer:-$default}"
}

resolve_binary_url() {
  if [ -n "$BINARY_URL" ]; then
    printf '%s' "$BINARY_URL"
    return
  fi
  printf 'https://github.com/%s/releases/latest/download/agy-server_linux_%s.tar.gz' "$REPO" "$ARCH"
}

resolve_hub_url() {
  curl -fsSL --compressed "$DOWNLOAD_PAGE" |
    grep -oE "https://storage\.googleapis\.com/antigravity-public/antigravity-hub/[^\"'<> ]+/${HUB_SLUG}/Antigravity\.tar\.gz" |
    head -1
}

hub_version() {
  printf '%s' "$1" | sed -nE 's#.*/antigravity-hub/([0-9][0-9.]*)-[0-9]+/.*#\1#p'
}

install_agy_server() {
  local url tmp
  url="$(resolve_binary_url)"
  tmp="$WORK_DIR/binary"
  mkdir -p "$tmp"

  say "Downloading agy-server…"
  curl -fsSL "$url" -o "$tmp/agy-server.tar.gz" ||
    die "Could not download $url"

  tar -xzf "$tmp/agy-server.tar.gz" -C "$tmp"
  [ -f "$tmp/agy-server" ] || die "The downloaded archive did not contain agy-server."

  sudo_run install -d "$PREFIX"
  sudo_run install -m 0755 "$tmp/agy-server" "$PREFIX/agy-server"
  ok "Installed $PREFIX/agy-server"

  # Upgrading over an older install: keep the old command name working rather
  # than leaving a stale binary beside the new one.
  if [ -f "$PREFIX/$LEGACY_NAME" ] && [ -f "$tmp/$LEGACY_NAME" ]; then
    sudo_run install -m 0755 "$tmp/$LEGACY_NAME" "$PREFIX/$LEGACY_NAME"
    ok "Updated $PREFIX/$LEGACY_NAME (older name, same build)"
  fi
}

install_language_server() {
  if [ -x "$LS_DIR/language_server" ]; then
    ok "language_server already present at $LS_DIR/language_server"
    return
  fi

  if [ "$LS_DIR_EXPLICIT" = "no" ] && [ -x "$LEGACY_LS_DIR/language_server" ]; then
    LS_DIR="$LEGACY_LS_DIR"
    ok "Reusing language_server from $LS_DIR"
    return
  fi

  local url version tmp
  url="$(resolve_hub_url)"
  [ -n "$url" ] || die "Could not find the Antigravity download URL. Download it manually from $DOWNLOAD_PAGE and pass --ls-dir."

  version="$(hub_version "$url")"
  say "Antigravity ${version:-unknown} for $HUB_SLUG"
  say "Downloading the official Antigravity build (about 170 MB)…"

  tmp="$WORK_DIR/antigravity"
  mkdir -p "$tmp"

  curl -fL --progress-bar "$url" -o "$tmp/Antigravity.tar.gz" ||
    die "Could not download the Antigravity build."

  say "Extracting language_server…"
  tar -xzf "$tmp/Antigravity.tar.gz" -C "$tmp" --wildcards --strip-components=3 \
    'Antigravity-*/resources/bin/language_server' ||
    die "Could not extract language_server from the archive."

  sudo_run install -d "$LS_DIR"
  sudo_run install -m 0755 "$tmp/language_server" "$LS_DIR/language_server"
  ok "Installed $LS_DIR/language_server"

  IDE_VERSION="$version"
}

write_config() {
  local args=(config --port "$PORT" --language-server "$LS_DIR/language_server")

  [ -n "$WORKSPACE_ROOT" ] && args+=(--workspace-root "$WORKSPACE_ROOT")
  if [ -n "$DOMAIN" ]; then
    args+=(--public-url "https://$DOMAIN" --trusted-proxies "127.0.0.1/32,::1/128")
  fi

  mkdir -p "${WORKSPACE_ROOT:-$HOME/workspace}"

  AGY_IDE_VERSION="${IDE_VERSION:-}" "$PREFIX/agy-server" "${args[@]}" >/dev/null
  ok "Wrote $HOME/.agy-remote/config.json"
}

set_password() {
  if [ -z "$PASSWORD" ]; then
    PASSWORD="$("$PREFIX/agy-server" passwd '' 2>/dev/null)"
  else
    "$PREFIX/agy-server" passwd "$PASSWORD" >/dev/null 2>&1
  fi
  ok "Access password set"
}

# Leaving the old unit enabled means two daemons racing for the same port, and
# the loser restarts forever.
disable_legacy_service() {
  [ "$SERVICE_NAME" != "$LEGACY_NAME" ] || return 0
  command -v systemctl >/dev/null 2>&1 || return 0
  [ -f "/etc/systemd/system/${LEGACY_NAME}.service" ] || return 0
  systemctl is-enabled --quiet "$LEGACY_NAME" 2>/dev/null ||
    systemctl is-active --quiet "$LEGACY_NAME" 2>/dev/null ||
    return 0

  sudo_run systemctl disable --now "$LEGACY_NAME" >/dev/null 2>&1 || true
  ok "Stopped the older $LEGACY_NAME service"
}

install_service() {
  local unit="/etc/systemd/system/${SERVICE_NAME}.service"

  sudo_run tee "$unit" >/dev/null <<EOF
[Unit]
Description=Antigravity Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$(id -un)
Environment=HOME=$HOME
WorkingDirectory=$HOME
ExecStart=$PREFIX/agy-server serve
Restart=always
RestartSec=5
KillMode=mixed
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
EOF

  sudo_run systemctl daemon-reload
  sudo_run systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
  sudo_run systemctl restart "$SERVICE_NAME"
  ok "Service $SERVICE_NAME installed and started"
}

install_caddy() {
  if ! command -v caddy >/dev/null 2>&1; then
    say "Installing Caddy for automatic HTTPS…"
    sudo_run apt-get update -qq
    sudo_run apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl
    curl -fsSL 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' |
      sudo_run gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -fsSL 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' |
      sudo_run tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
    sudo_run apt-get update -qq
    sudo_run apt-get install -y -qq caddy
  fi

  sudo_run install -d /etc/caddy/conf.d

  # Two site files claiming the same domain make Caddy refuse to load. Only the
  # one that claims this domain is in the way; a legacy file serving something
  # else is somebody's live site.
  local legacy_site="/etc/caddy/conf.d/${LEGACY_NAME}.caddy"
  if [ "$SERVICE_NAME" != "$LEGACY_NAME" ] && [ -f "$legacy_site" ] &&
    grep -qF "$DOMAIN {" "$legacy_site" 2>/dev/null; then
    sudo_run rm -f "$legacy_site"
    ok "Removed the older Caddy site file for $DOMAIN"
  fi

  sudo_run tee "/etc/caddy/conf.d/${SERVICE_NAME}.caddy" >/dev/null <<EOF
$DOMAIN {
	encode zstd gzip
	reverse_proxy 127.0.0.1:$PORT {
		flush_interval -1
	}
}
EOF

  if ! grep -q 'conf.d/\*.caddy' /etc/caddy/Caddyfile 2>/dev/null; then
    printf '\nimport /etc/caddy/conf.d/*.caddy\n' | sudo_run tee -a /etc/caddy/Caddyfile >/dev/null
  fi

  sudo_run systemctl reload caddy || sudo_run systemctl restart caddy
  ok "Caddy is serving https://$DOMAIN"
}

main() {
  printf '\n  %sAntigravity Server installer%s\n' "$BOLD" "$OFF"

  detect_platform
  require curl
  require tar

  WORK_DIR="$(mktemp -d)"
  trap 'rm -rf "$WORK_DIR"' EXIT

  heading "Setup"
  [ -n "$DOMAIN" ] || DOMAIN="$(ask 'Domain for HTTPS (blank to skip)' '')"
  [ -n "$WORKSPACE_ROOT" ] || WORKSPACE_ROOT="$(ask 'Workspace folder' "$HOME/workspace")"

  heading "Downloading"
  install_agy_server
  install_language_server

  heading "Configuring"
  write_config
  set_password
  disable_legacy_service

  if [ "$WANT_SERVICE" = "yes" ]; then
    heading "Service"
    if command -v systemctl >/dev/null 2>&1; then
      install_service
    else
      warn "systemd not found, skipping the service. Start it yourself with: agy-server serve"
    fi
  fi

  if [ -n "$DOMAIN" ] && [ "$WANT_CADDY" != "no" ]; then
    heading "HTTPS"
    install_caddy
  fi

  heading "Done"
  if [ -n "$PASSWORD" ]; then
    say "Password       ${BOLD}${PASSWORD}${OFF}"
  fi
  if [ -n "$DOMAIN" ]; then
    say "Open           ${CYAN}https://${DOMAIN}${OFF}"
  else
    say "Open           ${CYAN}http://$(hostname -I 2>/dev/null | awk '{print $1}'):${PORT}${OFF}"
    say "               ${DIM}that is this host's local address; use your public address or a"
    say "               tunnel if you are connecting from outside the network${OFF}"
  fi

  if [ ! -s "$HOME/.gemini/jetski-standalone-oauth-token" ]; then
    printf '\n'
    warn "This machine is not signed in to Antigravity yet."
    say "${DIM}Antigravity signs in through a localhost callback, which a remote"
    say "server cannot receive. Copy the token from a computer where you already"
    say "use the Antigravity desktop app:${OFF}"
    printf '\n'
    say "  ${CYAN}scp ~/.gemini/jetski-standalone-oauth-token $(id -un)@$(hostname):~/.gemini/${OFF}"
    printf '\n'
    say "${DIM}Then: sudo systemctl restart ${SERVICE_NAME}${OFF}"
  fi

  printf '\n  %sLogs%s   sudo journalctl -u %s -f\n' "$DIM" "$OFF" "$SERVICE_NAME"
  printf '  %sCheck%s  agy-server doctor\n\n' "$DIM" "$OFF"
}

main "$@"
