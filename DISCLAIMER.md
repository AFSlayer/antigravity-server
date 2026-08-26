# Disclaimer

Antigravity Remote is an independent, community project. It is **not affiliated
with, endorsed by, or supported by Google**. "Antigravity" and "Google" are
trademarks of Google LLC.

## What this project is

Google Antigravity's desktop app already contains a complete web UI: the
`language_server` binary that ships with it can serve that UI over HTTPS on
`127.0.0.1`. Antigravity Remote is a reverse proxy that makes that existing
server reachable from your phone or from a machine you own, behind a password.

## What this project does not do

Antigravity Remote deliberately does **not**:

- redistribute any Google binary — the installer downloads the official build
  from `storage.googleapis.com/antigravity-public/...`, the same URL the
  official download page uses;
- bypass authentication, licensing, quotas, regional availability, or any
  other access control;
- send your code, prompts, or credentials anywhere other than the Antigravity
  language server running on your own machine.

## The patches

Antigravity Remote rewrites a small number of strings in the web bundle as it
passes through the proxy. Every patch is listed in
[`internal/patches/registry.go`](internal/patches/registry.go) with a
description, and `agy-server doctor` prints which ones applied. They fall into
three groups:

1. **Origin correction** — the bundle hardcodes `https://127.0.0.1:<port>` as
   its API base URL, which cannot work from another device. It is replaced with
   the browser's own origin.
2. **Remote-session fixes** — skipping the desktop onboarding redirect.
3. **Touch usability** — Enter inserts a newline instead of sending, the iOS
   home bar no longer covers the composer, model reasoning levels are
   selectable by tap, and the voice button is hidden because transcription is
   unavailable in standalone mode.

No patch removes a restriction, unlocks a feature, or changes what Antigravity
is allowed to do.

## The icons

[`internal/assets/files/`](internal/assets/files) contains Antigravity's favicon
and app icon, extracted from the desktop app. They are included for one
functional reason: the language server does not serve those paths, so without
them a phone showing **Antigravity's own UI** gets a generic browser icon and a
blank home-screen tile. They identify Google's application, not this project, and
they are not used in any logo, wordmark, or branding position for Antigravity
Remote. All rights in them remain Google's. If Google would rather they were not
redistributed, open an issue and they will be removed in favour of reading them
from the local installation.

## Your responsibility

- You are bound by [Google's Antigravity terms](https://antigravity.google/terms)
  when you use it through this proxy, exactly as you are when you use it
  directly.
- Exposing a coding agent to the internet is your decision. It can read and
  write files and run commands on the host. Read [SECURITY.md](SECURITY.md)
  before putting it on a public address.
- Antigravity updates may change the web bundle and break patches. The tool
  tells you when that happens rather than failing silently, but there is no
  guarantee any given version will work.

## Warranty

None. See [LICENSE](LICENSE) — the software is provided "as is", without
warranty or condition of any kind.
