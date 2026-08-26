<div align="center">

# Antigravity Server

通往你自己的 Antigravity 的第二道门。  
修好移动端网页界面，不经 Google 中继，并在廉价 Linux 机器上无人值守运行。

[![release](https://img.shields.io/github/v/release/AFSlayer/antigravity-server?style=flat-square&color=4f7cff)](https://github.com/AFSlayer/antigravity-server/releases/latest)
[![ci](https://img.shields.io/github/actions/workflow/status/AFSlayer/antigravity-server/ci.yml?branch=main&style=flat-square)](https://github.com/AFSlayer/antigravity-server/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

| 官方远程 | 同一台服务器，经 `agy-server` |
| :---: | :---: |
| <img src="docs/assets/compare-official.png" width="380" alt="通过官方远程桥接在手机上查看的对话列表" /> | <img src="docs/assets/compare-agy.png" width="380" alt="同一个对话列表，经 agy-server：每个项目都有新建对话按钮，每一行都有 kebab 菜单" /> |
| 项目上没有 `+`，对话上没有 `⋮`。 | 每个项目可新建对话，每一行可删除 / 重命名 / 置顶 / 归档。 |

<sub>一台无头 Linux 机器，两道门，前后相隔几分钟拍摄。</sub>

[English](README.md) · [한국어](README.ko.md) · [日本語](README.ja.md) · [Português](README.pt-BR.md) · [Español](README.es.md)

</div>

---

## 为什么选择 Antigravity Server？（对比官方远程桥接）

Google 现已在 `antigravity.google.com` 推出官方远程桥接：用同一账号登录，即可访问你所有正在运行 Antigravity 且开启远程访问的机器。**“用手机访问自己的智能体”本身已不再需要本项目提供**，而且无头 Linux 服务器同样会出现在那个列表里。

官方桥接下发到手机上的，是原封不动的桌面网页包。这正是 `agy-server` 的价值所在：它作为**第二道直连门户**站在同一个 Antigravity 内核前面，在网页包发出的路上把它改写成触屏能用的样子。

两者并不互斥。`agy-server` 打开的也只是官方桥接所用的那个 `remoteControlEnabled` 设置，因此同一台机器可以同时服务两边——用哪个地址都行。

| | 官方远程（`antigravity.google.com`） | Antigravity Server (`agy-server`) |
| :--- | :--- | :--- |
| **移动端网页界面** | 原样的桌面网页包 | 面向触屏的 **42 个运行时补丁** |
| **对话管理** | 移动端无法删除、置顶或归档 | 在 kebab 菜单与标题栏中**删除、重命名、置顶、归档** |
| **项目导航** | 缺少项目 `(+)` 按钮；需在底部输入框切换 | 在项目列表顶部**恢复 `(+)` 按钮** |
| **消息操作** | 撤销与复制藏在鼠标悬停之后 | 触屏上**常显撤销（`↶`）与复制（`📋`）** |
| **iOS 键盘** | 底部 Safe Area 留白，聚焦时视口抖动 | 键盘打开期间收起安全区并跟踪视口 |
| **文件上传** | 1MB RPC 负载限制 | 面向大体积日志、HAR、数据集的**分块流式上传器** |
| **连接路径** | 经 Google 服务器中继 | **直连**——你自己的域名、局域网或 VPN |
| **谁能进来** | 持有该 Google 账号的人 | 你自己的密码（PBKDF2）、会话与限流 |
| **无头运行** | 自行搭建 | 一条安装命令：systemd 单元、Caddy HTTPS、`language_server` 自动更新 |

---

## 快速开始

### 方案 1：Linux 服务器 / 云 VPS（推荐）

在无头 Linux 实例（Oracle Cloud 免费层、AWS、DigitalOcean 或家庭服务器）上运行：

```bash
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash
```

安装脚本执行过程：
1. 提示输入您的域名（如 `agy.example.com`）和工作区路径。
2. 直接从 Google 官方构建存储桶（`storage.googleapis.com`）下载 `language_server` 二进制文件（不重新分发 Google 专有文件）。
3. 配置 Caddy 自动申请 HTTPS 证书、注册 systemd 服务并设置访问密码。

#### Google 账号认证
首次访问服务器时：
- **Web 界面直接登录**：在浏览器中打开 Web UI，进入**设置（Settings）**菜单直接完成 Google 登录。
- **复制现有 Token（可选）**：如果已在本地电脑登录过，可直接复制 Token 跳过认证：
  ```bash
  scp ~/.gemini/jetski-standalone-oauth-token user@your-server:~/.gemini/
  ```

---

### 方案 2：桌面伴侣模式（macOS、Windows、Linux 桌面）

将本地电脑上运行的 Antigravity 共享给同一局域网下的手机：

```bash
# macOS & Linux
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.sh | bash
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.ps1 | iex
```

`agy-server` 将打开包含二维码的本地控制面板。使用同一 Wi-Fi 下的手机扫描二维码即可免密直连。

<div align="center">
<img src="docs/assets/control-panel.png" width="320" alt="Control Panel" />
</div>

---

## 移动端 PWA 设置（添加到主屏幕）

Antigravity Server 支持渐进式 Web 应用（PWA）标准。将其添加到移动设备主屏幕，即可在**无地址栏和底部工具栏的全屏独立模式**下运行：

- **iOS (Safari)**：点击底部的**分享按钮（`⎋`）** → 选择**添加到主屏幕（Add to Home Screen）**。
- **Android (Chrome)**：点击右上角**菜单（`⋮`）** → 选择**安装应用**或**添加到主屏幕**。

> [!TIP]
> 从主屏幕图标启动可确保虚拟键盘弹出时界面不抖动，并完美激活 **0px 键盘紧贴补丁**。

---

## 核心特性

### ⚡ 移动端专属 UX 补丁
- **触控便捷操作**：在消息气泡上常驻显示撤销（`↶`）和复制（`📋`）按钮。
- **完整的对话管理**：通过顶栏菜单删除对话，在列表菜单中一键置顶或归档。
- **精确虚拟键盘跟踪**：输入法激活时自动将 Safe Area 间距压缩至 0px。

<div align="center">
<img src="docs/assets/demo.gif" width="320" alt="手机浏览器中经过补丁的移动端网页界面" />
</div>

---

### 📁 分块流式大文件上传
解除官方 Antigravity 的 1MB RPC 限制，将大体积日志或数据集直接流式上传至工作区：

<div align="center">
<img src="docs/assets/upload.gif" width="560" alt="分块流式文件上传器演示" />
</div>

---

### 🖥️ 桌面与平板电脑 Web 界面
除了移动端外，在笔记本或台式电脑的现代浏览器中同样拥有出色体验：

<div align="center">
<img src="docs/assets/desktop.png" width="700" alt="在桌面浏览器运行的 Antigravity Web UI" />
</div>

---

### 🔄 零停机无缝自动更新（Auto-Updater）
在无头 Linux 服务器上，`agy-server` 内置后台自动更新服务：
- 每日检查 Google 官方发布存储桶中的最新 `language_server` 版本。
- 以零停机的原子方式安全替换核心二进制文件。
- 手动检查与更新：运行 `agy-server update`。

---

## 生产环境反向代理配置（Caddy / Nginx）

为了支持智能体的实时流式输出（SSE）、WebSocket 通信及大文件上传，反向代理需**禁用缓冲**并配置 **WebSocket 升级**：

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

    # 允许大体积流式上传
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8765;
        proxy_http_version 1.1;

        # WebSocket 支持
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # 禁用缓冲以实现实时流式输出（必须）
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;

        # 传递真实客户端 IP
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

> [!IMPORTANT]
> 在反向代理后运行时，请配置 `--trusted-proxies 127.0.0.1/32`（或设置环境变量 `AGY_TRUSTED_PROXIES=127.0.0.1/32`），以确保防暴力破解系统能准确获取真实访客 IP。

---

## 工作原理

Antigravity 内部包含名为 `language_server` 的独立二进制程序。使用 `--standalone` 运行时，它在本地 `127.0.0.1` 提供 Web 界面。

`agy-server` 作为其前端反向代理，负责身份认证、动态运行时补丁注入及流式文件上传。

---

## CLI 命令

```
agy-server                      以桌面伴侣模式启动（局域网）
agy-server serve                作为无头服务器守护进程运行
agy-server update               检查并升级 Google 官方 language_server
agy-server doctor               诊断补丁完整性与系统状态
agy-server passwd [password]    设置或更改 Web 访问密码
agy-server sessions [revoke]    查看活跃会话或注销所有设备
agy-server config [flags]       管理 config.json 配置项
```

---

## 许可证

[Apache-2.0](LICENSE)。与 Google 无隶属或背书关系。详见 [DISCLAIMER.md](DISCLAIMER.md)。
