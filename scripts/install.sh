#!/usr/bin/env bash
set -euo pipefail

REPO="AFSlayer/antigravity-server"
RELEASES_URL="https://antigravity-hub-auto-updater-974169037036.us-central1.run.app/releases"
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
  --workspace-root DIR  Where new projects are created (default \$HOME/antigravity/workspace)
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

validate_domain() {
  DOMAIN="${DOMAIN#"${DOMAIN%%[![:space:]]*}"}"
  DOMAIN="${DOMAIN%"${DOMAIN##*[![:space:]]}"}"
  [ -n "$DOMAIN" ] || return 0
  case "$DOMAIN" in
    *://*) die "Invalid domain '$DOMAIN': pass a bare hostname such as agy.example.com without http:// or https://." ;;
  esac
  if [ "${#DOMAIN}" -gt 253 ] || ! [[ "$DOMAIN" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$ ]]; then
    die "Invalid domain '$DOMAIN': expected a valid hostname."
  fi
}

validate_options() {
  if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
    die "Invalid --port '$PORT': must be an integer between 1 and 65535."
  fi
  if ! [[ "$SERVICE_NAME" =~ ^[a-zA-Z0-9_-]+$ ]]; then
    die "Invalid --service-name '$SERVICE_NAME': use only letters, digits, hyphens, and underscores."
  fi
  validate_domain
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

resolve_hub_url_from_releases() {
  local feed_url="$1" json first ver eid
  json="$(curl -fsSL --compressed "$feed_url" 2>/dev/null || true)"
  [ -n "$json" ] || return 1
  first="$(printf '%s' "$json" | tr -d '\r\n' | grep -oE '\{[^{}]*\}' | head -1 || true)"
  [ -n "$first" ] || return 1
  ver="$(printf '%s' "$first" | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([0-9]+\.[0-9]+(\.[0-9]+)*)".*/\1/p')"
  eid="$(printf '%s' "$first" | sed -nE 's/.*"execution_id"[[:space:]]*:[[:space:]]*"([0-9]+)".*/\1/p')"
  if [ -n "$ver" ] && [ -n "$eid" ]; then
    printf 'https://storage.googleapis.com/antigravity-public/antigravity-hub/%s-%s/%s/Antigravity.tar.gz' "$ver" "$eid" "$HUB_SLUG"
    return 0
  fi
  return 1
}

resolve_hub_url() {
  local resolved html dyn_url
  if resolved="$(resolve_hub_url_from_releases "$RELEASES_URL")" && [ -n "$resolved" ]; then
    printf '%s' "$resolved"
    return
  fi

  html="$(curl -fsSL --compressed "$DOWNLOAD_PAGE" 2>/dev/null || true)"
  [ -n "$html" ] || return 0

  dyn_url="$(printf '%s' "$html" | grep -oE 'https://antigravity-hub-auto-updater-[a-z0-9-]+\.[a-z0-9.-]+\.run\.app/releases' | head -1 || true)"
  if [ -n "$dyn_url" ] && [ "$dyn_url" != "$RELEASES_URL" ]; then
    if resolved="$(resolve_hub_url_from_releases "$dyn_url")" && [ -n "$resolved" ]; then
      printf '%s' "$resolved"
      return
    fi
  fi

  printf '%s' "$html" |
    grep -oE "https://storage\.googleapis\.com/antigravity-public/antigravity-hub/[0-9][0-9.]*-[0-9]+/${HUB_SLUG}/Antigravity\.tar\.gz" |
    head -1 || true
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

reject_symlink_ls_paths() {
  local dir="$1"
  [ ! -L "$dir" ] || die "Refusing to use symlinked language_server directory: $dir"
  [ ! -L "$dir/language_server" ] || die "Refusing to operate on symlink: $dir/language_server"
  [ ! -L "$dir/language_server.bak" ] || die "Refusing to operate on symlink: $dir/language_server.bak"
}

chown_ls_target() {
  reject_symlink_ls_paths "$LS_DIR"
  local owner
  owner="$(id -un):$(id -gn)"
  if [ "$LS_DIR" = "$DEFAULT_LS_DIR" ] || [ "$LS_DIR" = "$LEGACY_LS_DIR" ] || [ "${LS_DIR_CREATED:-no}" = "yes" ]; then
    sudo_run chown -h "$owner" "$LS_DIR" 2>/dev/null || true
  fi
  if [ -e "$LS_DIR/language_server" ] && [ ! -L "$LS_DIR/language_server" ]; then
    sudo_run chown -h "$owner" "$LS_DIR/language_server" 2>/dev/null || true
  fi
  if [ -e "$LS_DIR/language_server.bak" ] && [ ! -L "$LS_DIR/language_server.bak" ]; then
    sudo_run chown -h "$owner" "$LS_DIR/language_server.bak" 2>/dev/null || true
  fi
}

default_workspace_root() {
  local cfg_dir="${AGY_HOME:-$HOME/.agy-remote}"
  local cfg_file="$cfg_dir/config.json"
  if [ -f "$cfg_file" ]; then
    local existing=""
    if command -v python3 >/dev/null 2>&1; then
      existing="$(python3 -c 'import json,sys
try:
    with open(sys.argv[1], "r", encoding="utf-8") as f:
        v = json.load(f).get("workspace_root", "")
        if isinstance(v, str):
            sys.stdout.write(v)
except Exception:
    pass' "$cfg_file" 2>/dev/null || true)"
    else
      existing="$(sed -nE 's/.*"workspace_root"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p' "$cfg_file" | head -1 | sed 's/\\"/"/g; s/\\\//\//g; s/\\\\/\\/g')"
    fi
    if [ -n "$existing" ]; then
      printf '%s' "$existing"
      return
    fi
  fi
  if [ ! -d "$HOME/antigravity/workspace" ] && [ -d "$HOME/workspace" ]; then
    printf '%s' "$HOME/workspace"
    return
  fi
  printf '%s' "$HOME/antigravity/workspace"
}

install_language_server() {
  reject_symlink_ls_paths "$LS_DIR"
  if [ -x "$LS_DIR/language_server" ]; then
    ok "language_server already present at $LS_DIR/language_server"
    chown_ls_target
    return
  fi

  if [ "$LS_DIR_EXPLICIT" = "no" ] && [ ! -L "$LEGACY_LS_DIR" ] && [ ! -L "$LEGACY_LS_DIR/language_server" ] && [ -x "$LEGACY_LS_DIR/language_server" ]; then
    reject_symlink_ls_paths "$LEGACY_LS_DIR"
    LS_DIR="$LEGACY_LS_DIR"
    ok "Reusing language_server from $LS_DIR"
    chown_ls_target
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
  local archive_member
  archive_member="$(
    tar -tzf "$tmp/Antigravity.tar.gz" | awk '
      !found && /(^|\/)resources\/bin\/language_server$/ { found = $0 }
      !fallback && /(^|\/)language_server(_linux_(x64|arm64))?$/ { fallback = $0 }
      END { if (found != "") print found; else if (fallback != "") print fallback }
    '
  )" || die "Could not inspect the Antigravity archive."
  [ -n "$archive_member" ] || die "Could not find language_server inside the archive."
  tar -xzf "$tmp/Antigravity.tar.gz" -O "$archive_member" >"$tmp/language_server" ||
    die "Could not extract language_server from the archive."
  [ -s "$tmp/language_server" ] ||
    die "Extracted language_server binary is empty."

  reject_symlink_ls_paths "$LS_DIR"
  if [ ! -d "$LS_DIR" ]; then
    LS_DIR_CREATED="yes"
  fi
  sudo_run install -d "$LS_DIR"
  reject_symlink_ls_paths "$LS_DIR"
  sudo_run install -m 0755 "$tmp/language_server" "$LS_DIR/language_server"
  chown_ls_target
  ok "Installed $LS_DIR/language_server"

  IDE_VERSION="$version"
}

write_config() {
  local args=(config --port "$PORT" --language-server "$LS_DIR/language_server")

  if [ -n "$WORKSPACE_ROOT" ]; then
    # shellcheck disable=SC2088
    case "$WORKSPACE_ROOT" in
      "~") WORKSPACE_ROOT="$HOME" ;;
      "~/"*) WORKSPACE_ROOT="$HOME/${WORKSPACE_ROOT#\~/}" ;;
    esac
    args+=(--workspace-root "$WORKSPACE_ROOT")
  fi
  if [ -n "$DOMAIN" ]; then
    args+=(--public-url "https://$DOMAIN")
  fi

  mkdir -p "${WORKSPACE_ROOT:-$HOME/antigravity/workspace}"

  AGY_IDE_VERSION="${IDE_VERSION:-}" "$PREFIX/agy-server" "${args[@]}" >/dev/null
  ok "Wrote ${AGY_HOME:-$HOME/.agy-remote}/config.json"
}

set_password() {
  local creds_file="${AGY_HOME:-$HOME/.agy-remote}/credentials.json"
  if [ -z "$PASSWORD" ] && [ -s "$creds_file" ]; then
    ok "Access password unchanged"
    return
  fi
  if [ -z "$PASSWORD" ]; then
    PASSWORD="$("$PREFIX/agy-server" passwd '' 2>/dev/null)" ||
      die "Could not generate an initial access password."
  else
    printf '%s\n' "$PASSWORD" | "$PREFIX/agy-server" passwd --stdin >/dev/null ||
      die "Could not set the access password."
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

systemd_quote() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//%/%%}"
  printf '"%s"' "$s"
}

install_service() {
  local unit="/etc/systemd/system/${SERVICE_NAME}.service"
  local home_env work_dir exec_bin extra_env=""
  home_env="Environment=$(systemd_quote "HOME=$HOME")"
  work_dir="$(systemd_quote "$HOME")"
  exec_bin="$(systemd_quote "$PREFIX/agy-server")"
  if [ -n "${AGY_HOME:-}" ]; then
    extra_env=$'\n'"Environment=$(systemd_quote "AGY_HOME=$AGY_HOME")"
  fi

  sudo_run tee "$unit" >/dev/null <<EOF
[Unit]
Description=Antigravity Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$(id -un)
${home_env}${extra_env}
WorkingDirectory=${work_dir}
ExecStart=${exec_bin} serve
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
  validate_options
  require curl
  require tar

  WORK_DIR="$(mktemp -d)"
  trap 'rm -rf "$WORK_DIR"' EXIT

  heading "Setup"
  if [ -z "$DOMAIN" ]; then
    DOMAIN="$(ask 'Domain for HTTPS (blank to skip)' '')"
    validate_domain
  fi
  if [ -z "$WORKSPACE_ROOT" ]; then
    WORKSPACE_ROOT="$(ask 'Workspace folder' "$(default_workspace_root)")"
  fi

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

  printf '\n'
  say "Sign in        Open ${CYAN}/__agy/signin${OFF} in your browser after entering the access password"
  say "               ${DIM}to complete Google account sign-in directly from the web UI.${OFF}"

  printf '\n  %sLogs%s   sudo journalctl -u %s -f\n' "$DIM" "$OFF" "$SERVICE_NAME"
  printf '  %sCheck%s  agy-server doctor\n\n' "$DIM" "$OFF"
}

main "$@"
