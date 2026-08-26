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
