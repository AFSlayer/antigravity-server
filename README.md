<div align="center">

# Antigravity Server

A second front door to your own Antigravity — one that fixes the mobile web UI,  
skips Google's relay, and runs unattended on a cheap Linux box.

[![release](https://img.shields.io/github/v/release/AFSlayer/antigravity-server?style=flat-square&color=4f7cff)](https://github.com/AFSlayer/antigravity-server/releases/latest)
[![ci](https://img.shields.io/github/actions/workflow/status/AFSlayer/antigravity-server/ci.yml?branch=main&style=flat-square)](https://github.com/AFSlayer/antigravity-server/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

| Official remote | Same server, through `agy-server` |
| :---: | :---: |
| <img src="docs/assets/compare-official.png" width="380" alt="Antigravity's conversation list on a phone through the official remote bridge" /> | <img src="docs/assets/compare-agy.png" width="380" alt="The same conversation list through agy-server, with a new-conversation button on every project and a kebab menu on every row" /> |
| No `+` on a project. No `⋮` on a conversation. | New conversation per project, and delete / rename / pin / archive per row. |

<sub>One headless Linux box, two front doors, minutes apart.</sub>

[한국어](README.ko.md) · [中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português](README.pt-BR.md) · [Español](README.es.md)

</div>

---

## Why Antigravity Server? (vs Official Remote)

Google now ships an official remote bridge at `antigravity.google.com`: sign in with the same account and you reach every machine of yours that is running Antigravity with remote access enabled. **Reaching your own agent from a phone is no longer something this project has to provide** — and a headless Linux server shows up in that list too.

What the official bridge hands your phone is the desktop web bundle, unchanged. That is where `agy-server` earns its place: it sits in front of the same Antigravity core as a second, direct front door, and rewrites that bundle on the way out so a touch screen can actually use it.

The two are not exclusive. `agy-server` only enables the same `remoteControlEnabled` setting the official bridge uses, so one machine can serve both at once — use whichever address suits you.

| | Official remote (`antigravity.google.com`) | Antigravity Server (`agy-server`) |
| :--- | :--- | :--- |
| **Mobile web UI** | The desktop bundle as-is | **42 named runtime patches** for touch |
| **Conversation control** | No delete, pin, or archive on mobile | **Delete, Rename, Pin, Archive** from the kebab menu and titlebar |
| **Project navigation** | No project `(+)` button; switch via the bottom input | **Restored `(+)` button** in project list headers |
| **Message actions** | Undo and Copy live behind hover states | **Undo (`↶`) and Copy (`📋`)** always visible on touch |
| **iOS keyboard fit** | Bottom safe-area gap remains; viewport jumps on focus | Safe-area collapse and viewport tracking while the keyboard is open |
| **File uploads** | 1MB RPC payload limit | **Chunked streaming uploader** for large logs, HARs, and datasets |
| **Connection path** | Relayed through Google's servers | **Direct** — your own domain, LAN, or VPN |
| **Who can get in** | Whoever holds that Google account | Your own password (PBKDF2), sessions, and rate limiting |
| **Running it headless** | Set it up yourself | One installer: systemd unit, Caddy HTTPS, `language_server` auto-updater |

---

## Quick Start

### Option 1: Linux Server / Cloud VPS (Recommended)

Run Antigravity on a headless Linux instance (Oracle Cloud Free Tier, AWS, DigitalOcean, or a home server):

```bash
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash
```

The installer:
1. Prompts for your domain (e.g. `agy.example.com`) and workspace root.
2. Downloads `language_server` directly from Google's official build bucket (`storage.googleapis.com`). No Google binaries are redistributed.
3. Configures Caddy for automatic HTTPS, creates a systemd service, and sets access credentials.

#### Google Authentication
When accessing your server for the first time:
- **Direct Web Login**: Open the Web UI, navigate to **Settings**, and complete Google authentication directly in your browser.
- **Or Copy Existing Token (Optional)**: If you already logged in on a local desktop, you can copy your token to skip re-authenticating:
  ```bash
  scp ~/.gemini/jetski-standalone-oauth-token user@your-server:~/.gemini/
  ```

---

### Option 2: Desktop Companion (macOS, Windows, Linux Desktop)

To expose a local desktop Antigravity instance over your local network:

```bash
# macOS & Linux
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.sh | bash
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.ps1 | iex
```

`agy-server` opens a local control panel with a QR code. Scan the code from a phone on the same network to connect without typing a password.

<div align="center">
<img src="docs/assets/control-panel.png" width="320" alt="Control Panel" />
</div>

---

## Mobile PWA & Client Setup

Antigravity Server supports the Progressive Web App (PWA) standard. Adding it to your mobile home screen launches the interface in a **fullscreen, standalone view with no browser address bar or navigation buttons**:

- **iOS (Safari)**: Tap the **Share button (`⎋`)** → Select **Add to Home Screen**.
- **Android (Chrome)**: Tap the **Menu (`⋮`)** → Select **Install app** or **Add to Home screen**.

> [!TIP]
> Running in standalone PWA mode ensures virtual keyboard transitions and 0px safe-area collapse operate smoothly without browser toolbar jumps.

---

## Key Features

### ⚡ Mobile-First UX Patches
- **Touch-Friendly Controls**: Undo (`↶`) and Copy (`📋`) buttons remain permanently visible on mobile message bubbles.
- **Full Conversation Management**: Delete conversations via the titlebar menu and toggle Pin/Archive directly from the history dropdown.
- **Precise Keyboard Tracking**: Automatically collapses safe area insets to 0px when the on-screen keyboard appears.

<div align="center">
<img src="docs/assets/demo.gif" width="320" alt="The patched mobile web UI in a phone browser" />
</div>

---

### 📁 Chunked Streaming File Uploads
Standard Antigravity restricts file attachments via a 1MB RPC limit. `agy-server` injects a chunked streaming uploader to transfer large logs, datasets, or HAR files directly into your workspace:

<div align="center">
<img src="docs/assets/upload.gif" width="560" alt="Chunked Streaming File Uploader Demo" />
</div>

---

### 🖥️ Desktop & Tablet Web Interface
In addition to mobile devices, Antigravity Server runs smoothly in any modern desktop browser:

<div align="center">
<img src="docs/assets/desktop.png" width="700" alt="Antigravity Web UI on Desktop Browser" />
</div>

---

### 🔄 Zero-Downtime Automatic Updates
On headless Linux servers, `agy-server` includes a background auto-updater service:
- Checks Google's official release buckets daily for new `language_server` versions.
- Downloads and replaces the core binary atomically with zero downtime.
- Manual check & upgrade: run `agy-server update`.

---

### 📝 In-App Global & Workspace Rules Editor
Manage your agent instructions (`~/.gemini/GEMINI.md`) directly within the web UI:
- Open **Settings → Customizations → Rules**.
- Click the native **Edit** button next to `user_global` or any workspace rule to expand an inline code editor.
- Updates are saved atomically to the host filesystem with instant effect.

---

## Production & Reverse Proxy Setup

Antigravity uses Server-Sent Events (SSE), WebSocket connections, and chunked streaming. If running behind a custom reverse proxy, disable proxy buffering and configure WebSocket upgrades:

### Caddy
```caddyfile
agy.example.com {
    encode zstd gzip

    reverse_proxy 127.0.0.1:8765 {
        flush_interval -1
    }
}
```

### Nginx
```nginx
server {
    listen 443 ssl http2;
    server_name agy.example.com;

    # Allow large chunked streaming uploads
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8765;
        proxy_http_version 1.1;

        # WebSocket support
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # Disable buffering for real-time agent token streaming
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;

        # Forward real client IP
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

> [!IMPORTANT]
> When running behind a reverse proxy, pass `--trusted-proxies 127.0.0.1/32` (or set `AGY_TRUSTED_PROXIES=127.0.0.1/32`) so brute-force rate limiters inspect the genuine client IP rather than the proxy.

---

## How It Works

Antigravity includes a standalone binary named `language_server`. When run with `--standalone`, it serves the Antigravity Web UI on `127.0.0.1`.

`agy-server` acts as a reverse proxy to:
- Handle authentication (PBKDF2 hashing, cookie sessions, rate-limiting).
- Apply on-the-fly JS/CSS patches for touch devices.
- Provide a chunked streaming endpoint for large file uploads.

```
  Phone / Tablet / Laptop Browser
                │
                ▼ HTTPS (Port 443 / 8765)
  ┌──────────────────────────────────────────────┐
  │ agy-server (Reverse Proxy & Auth)            │
  │  - PBKDF2 Session & Rate Limiting            │
  │  - Chunked Streaming Uploader (/uploads)     │
  │  - On-the-fly Web Bundle Patcher             │
  └──────────────────────┬───────────────────────┘
                         │ localhost
                         ▼
  ┌──────────────────────────────────────────────┐
  │ language_server --standalone                 │
  │  - Official Antigravity Core & Agent Engine   │
  │  - Terminal, File Tree, Artifacts, Composer   │
  └──────────────────────┬───────────────────────┘
                         │ gRPC
                         ▼
                Google CloudCode API
```

---

## Mobile UX Patches

The web bundle Antigravity serves — through the official remote bridge or through `agy-server` — is the desktop one. `agy-server` rewrites it in flight using the 42 patches defined in [`internal/patches/registry.go`](internal/patches/registry.go). A sample of what they change:

| Category | Desktop Bundle Behavior | agy-server Patch |
| :--- | :--- | :--- |
| **Navigation** | Project `(+)` button omitted on mobile screens | Restores the `(+)` New Conversation button next to each project row |
| **Conversation Actions** | No delete, pin, or archive on touch | Adds Delete, Pin, and Archive to the `⋮` kebab menu and titlebar |
| **Message Actions** | Undo and Copy buttons hidden behind hover states | Displays Undo (`↶`) and Copy (`📋`) buttons on touch devices |
| **Virtual Keyboard** | iOS Safari viewport bounces and leaves blank gaps | Collapses bottom safe-area insets to 0px while keyboard is active |
| **File Uploads** | 1MB RPC payload limit fails on logs or datasets | Streams files asynchronously to disk via chunked streaming endpoint |
| **Touch Interaction** | 300ms tap delay and double-tap zoom | Sets `touch-action: manipulation` for immediate touch response |
| **Input Behavior** | Mobile Enter key sends message instead of newline | Enter creates a newline; Send button or Cmd/Ctrl+Enter submits |
| **Model Selection** | Tapping a model closes the menu immediately | Opens the reasoning effort submenu on tap |

Run `agy-server doctor` to inspect the status of all patches against your installed bundle.

---

## CLI Commands

```
agy-server                      Start in desktop companion mode (local network)
agy-server serve                Run as a headless server daemon
agy-server update               Check and update language_server to latest upstream
agy-server doctor               Verify patch integrity and system status
agy-server passwd [password]    Set or reset web access password
agy-server sessions [revoke]    List active sessions or revoke all devices
agy-server config [flags]       Manage configuration in config.json
```

All CLI flags can be set via environment variables prefixed with `AGY_` (e.g. `AGY_PORT=8765`, `AGY_PUBLIC_URL=https://agy.example.com`).

---

## Security

- **Password Protection**: Passwords are hashed with PBKDF2-SHA256 (200,000 iterations).
- **Session Tokens**: 256-bit random tokens; only SHA-256 hashes are stored on disk.
- **Brute-Force Protection**: 5 failed login attempts trigger an IP lockout (5 to 30 minutes).
- **Upload Isolation**: File uploads are restricted to the configured project directory; path traversal attempts (`../`) are rejected.
- **Trusted Proxies**: Set `--trusted-proxies` when running behind Nginx, Caddy, or Cloudflare to prevent header spoofing.

---

## FAQ

**Google already has an official remote. Do I still need this?**  
Only if the official mobile web UI gets in your way, or you would rather not relay through Google's servers and would rather gate access with your own password. Both can run on the same machine; nothing here disables the official bridge.

**Does this require the Antigravity desktop GUI on Linux?**  
No. `agy-server` runs the core `language_server` binary headlessly.

**Will updates from Google break the patches?**  
Patches use adaptive regular expressions that match structural AST patterns rather than exact variable names. Run `agy-server update` to pull official upstream releases safely.

**Does my code pass through third-party servers?**  
No. Traffic flows directly between your client browser and your server instance. The only external connection is `language_server` communicating with Google's API.

---

## License

[Apache-2.0](LICENSE). Not affiliated with or endorsed by Google. See [DISCLAIMER.md](DISCLAIMER.md).
