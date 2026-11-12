# Security

## What you are exposing

Antigravity is a coding agent. Anyone who reaches its UI can read and write
files on the host and run commands on it. Treat access to Antigravity Remote as
equivalent to shell access.

## How access is protected

| Control | Detail |
| --- | --- |
| Password | Stored as a PBKDF2-HMAC-SHA256 hash (200,000 iterations, random 16-byte salt) in `~/.agy-remote/credentials.json`, mode `0600`. Never stored in plain text. |
| Sessions | 256-bit random tokens. Only their SHA-256 hashes are written to disk, so a stolen `sessions.json` grants nothing. Default lifetime 30 days (`--session-days`). |
| Cookies | `HttpOnly`, `SameSite=Lax`, and `Secure` whenever the request arrived over HTTPS. |
| Rate limiting | 5 failed attempts per IP per 5 minutes, then an exponential lockout up to 30 minutes. A global limiter (60 failures/minute) blunts distributed guessing. The lockout applies even to the correct password. |
| File uploads | Streaming multipart upload to workspace with strict boundary checking (`filepath.Rel`) to prevent path traversal. Expired uploads are automatically purged by a lightweight background cleaner (default TTL: 7 days). |
| Binary updates | Official `language_server` downloads are strictly validated against Google Cloud Storage (`storage.googleapis.com/antigravity-public/`). Installation uses atomic filesystem renames so failures never corrupt running binaries. |
| Revocation | `agy-server sessions revoke` or "Sign out all" in the control panel invalidates every device immediately. |
| Admin surface | The control panel (QR code, password, shutdown) listens on a **separate loopback-only port** and is never routed through the public listener. |
| Login QR | The QR code carries a single-use enrollment token valid for 10 minutes, so you never type the password on a phone. Reuse is rejected. |

## Sidecar UIs, localhost previews and OAuth callbacks

Antigravity opens some things on `localhost`: sidecar UIs such as the Swarm
board, the dev servers an agent starts, and the OAuth redirects of plugins and
MCP servers. A browser on another device cannot reach the host's `localhost`,
so `agy-server` relays these three, each behind the login.

| Path | What it does | Boundaries |
| --- | --- | --- |
| `/__agy/sidecar/<port>/` | Serves a sidecar's web UI in the app's side panel. | Only ports the language server reports as running sidecars. Opening one needs a valid login session and a same-origin request, and redirects the frame to a path capability tied to that session: 2 hours idle, 12 hours at most, and gone when the session ends, though a sidecar WebSocket already open stays open until it closes, as the app's own does. The frame is an opaque-origin sandbox without `allow-same-origin`, so the sidecar cannot read the app's cookies or call the app. Browser cookies are never sent to the sidecar; its own cookies are kept on the server per capability. Nor are the headers a reverse proxy or tunnel adds about the user, such as Cloudflare Access's or Tailscale's, or Basic credentials the browser adds for the app, and sidecar responses are kept out of shared caches. Its Content Security Policy is kept and only gains its own path prefix and the injected helper script; a sidecar's `sandbox` can only be tightened. The app takes a comment the sidecar adds to a document only with the gateway token of the frame's URL, which a page the frame navigated to does not know. In that frame `document.cookie` and IndexedDB are unavailable and `localStorage` and `sessionStorage` live in memory per page, so a sidecar UI whose scripts read cookies, for example for a CSRF token, does not work. |
| `/__agy/api/loopback-fetch` | Fetches `http://localhost:<port><path>` for a localhost preview, including the requests the previewed page makes itself. | POST from the same origin with a valid session. The previewed page's scripts choose the port, so only listeners of the language server or a process it started are fetched. Besides a dev server the agent ran, these include MCP servers and other tools the agent runs that listen on a port, and the previewed page can send them any request and read the reply, so preview only pages you trust. Any other port is answered as having no listener without being contacted, so other services on the host stay out of reach and a server started outside Antigravity cannot be previewed. Ports used by `agy-server`, the language server itself and sidecars are refused too, as are the ports browsers block. `Origin` and `Referer` are passed on only when they name the target port. Bodies are capped at 16 MiB and at most 12 fetches run at once. |
| `/__agy/api/oauth-callback` | Delivers a pasted `http://localhost:<port>/...?code=...` redirect as a GET. | POST from the same origin with a valid session. The port must be a listener of the language server or a process it started, and the request goes to the loopback address family it listens on. Redirects from the listener are not followed, so the one-time code is used once. |

A sidecar's capability is in its URL path, and a gateway's token in its query.
Whoever has that URL can use the sidecar, including sending messages to the
agent through the Swarm board, until the capability expires or the session
ends. A reverse proxy in front of `agy-server` may write these URLs to its
access log, so keep such logs private or leave `/__agy/sidecar/` out of them.

Known limits:

- In a sidecar, root-relative URLs in ES module imports and in navigation
  through `location` are not rewritten into the sidecar's path, so a UI
  plugin that uses them may not load parts of itself.
- A sidecar's frame has an opaque origin. It cannot start a `Worker` from a
  script URL, cannot show its own pages in a nested frame, which
  `frame-ancestors 'self'` refuses there, and is not granted permissions such
  as the microphone or reading the clipboard.
- A server the agent starts detached so that it outlives its shell, for
  example with `setsid` or `disown`, can leave the language server's process
  tree and then cannot be previewed. Neither can one started outside
  Antigravity, such as a container published with `docker run -p`.
- Previews dial `127.0.0.1` or `::1`, so a server listening only on another
  loopback address such as `127.0.0.2` cannot be previewed.

## Running behind a reverse proxy

If a proxy terminates TLS in front of `agy-server`, tell it which peers to trust:

```bash
agy-server serve --public-url https://agy.example.com --trusted-proxies 127.0.0.1/32,::1/128
```

Never list a CIDR you do not control. A trusted peer can claim any client IP.

### Proxy upload payload limits
`agy-server` streams uploads with no internal payload limit. However, front-facing reverse proxies and CDNs often impose default limits (e.g. Nginx defaults to 1 MB with `client_max_body_size`, and Cloudflare Free caps at 100 MB). If users encounter `413 Request Entity Too Large` when uploading large logs or traces, configure your proxy accordingly:
- **Nginx**: `client_max_body_size 100M;` (or larger)
- **Caddy**: default is unlimited, or tune `request_body { max_size ... }`
- **Cloudflare**: for files > 100 MB, use Tailscale or a direct tunnel bypass.

## Recommended deployments

**Best: no public exposure at all.** Put the host on
[Tailscale](https://tailscale.com/) or a WireGuard network and reach it over the
private address. `agy-server` detects and displays a Tailscale address
automatically. The password then becomes a second layer rather than the only one.

**Good: a domain behind Cloudflare Tunnel or Caddy with HTTPS**, plus a strong
password. `scripts/install.sh --domain agy.example.com` sets up Caddy with
automatic certificates and an HTTP-to-HTTPS redirect.

**Avoid: plain HTTP on a public IP.** The password and session cookie travel in
cleartext. On a trusted LAN this is acceptable; on the internet it is not.

## Local (same-network) mode

Local mode serves plain HTTP on your LAN, because browsers on phones reject
self-signed certificates and there is no way to get a real certificate for a
private address. The password still applies, but anyone already on your network
can observe the traffic. Do not use local mode on untrusted Wi-Fi — use
Tailscale instead.

## Reporting a vulnerability

Please open a [private security advisory](https://github.com/AFSlayer/antigravity-server/security/advisories/new)
rather than a public issue. Include the version (`agy-server version`), the
deployment shape, and reproduction steps. Expect a first response within a week.

## Not in scope

- Vulnerabilities in Antigravity itself or in Google's `language_server` —
  report those to Google.
- Anyone with an interactive session on the host: they can read
  `~/.agy-remote/` and the Antigravity OAuth token directly.
