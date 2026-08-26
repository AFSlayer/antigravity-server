#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
OUT="$ROOT/docs/assets"

CHROME="${CHROME:-}"
for candidate in \
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  "$(command -v google-chrome || true)" \
  "$(command -v chromium || true)"; do
  if [ -z "$CHROME" ] && [ -x "$candidate" ]; then CHROME="$candidate"; fi
done
[ -n "$CHROME" ] || { echo "Chrome not found. Set CHROME=/path/to/chrome" >&2; exit 1; }

FRAME_W=519
FRAME_H=952

shot() {
  local url="$1" out="$2" width="$3" height="$4" scale="${5:-2}"
  "$CHROME" --headless --disable-gpu --no-sandbox --hide-scrollbars \
    --default-background-color=00000000 \
    --force-device-scale-factor="$scale" \
    --window-size="$width,$height" \
    --virtual-time-budget=6000 \
    --screenshot="$out" \
    "$url" >/dev/null 2>&1
  printf '  %s (%s)\n' "$(basename "$out")" \
    "$(sips -g pixelWidth -g pixelHeight "$out" 2>/dev/null | awk '/pixel/{printf "%sx", $2}' | sed 's/x$//')"
}

frame_url() {
  shot "file://$HERE/frame.html?url=$1" "$2" "$FRAME_W" "$FRAME_H" 2
}

frame_image() {
  shot "file://$HERE/frame.html?src=file://$1&fit=${3:-cover}" "$2" "$FRAME_W" "$FRAME_H" 3
}

abspath() {
  printf '%s' "$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
}

usage() {
  cat <<EOF
Usage: shoot.sh <command> [args]

  login <base-url>            docs/assets/login.png from a running agy-server
  control <control-url>        docs/assets/control-panel.png
  hero <phone-screenshot.png>  docs/assets/hero.png
  frame <in.png> <out.png>     Wrap any phone screenshot in the device frame

The device frame renders pages inside a 393x852 iframe, so the layout matches a
phone regardless of the headless window size.

Environment: CHROME=/path/to/chrome
EOF
}

mkdir -p "$OUT"

case "${1:-}" in
  login)
    frame_url "${2:?usage: shoot.sh login http://127.0.0.1:PORT}/__agy/login" "$OUT/login.png"
    ;;
  control)
    shot "${2:?usage: shoot.sh control http://127.0.0.1:PORT/}" "$OUT/control-panel.png" 660 2400
    python3 "$HERE/trim.py" "$OUT/control-panel.png" 56
    ;;
  hero)
    frame_image "$(abspath "${2:?usage: shoot.sh hero screenshot.png}")" "$OUT/hero.png"
    ;;
  frame)
    frame_image "$(abspath "${2:?usage: shoot.sh frame in.png out.png}")" "${3:?missing output}"
    ;;
  *)
    usage
    exit 1
    ;;
esac
