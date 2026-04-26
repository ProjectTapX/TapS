**English** | [中文](README.zh-CN.md) | [日本語](README.ja.md)

# TapS

Open-source game server management panel — one panel to host all your game servers.

TapS uses a Panel + Daemon dual-architecture. The Panel handles the Web UI and centralized management, while Daemons run on each game host machine to manage containers. Supports Minecraft Java / Bedrock / Terraria and any Docker-containerized process, featuring real PTY terminals, file management, backup & restore, auto-hibernation, SSO login, multi-language support, and more.

![screenshot](docs/images/screenshot.png)
![screenshot1](docs/images/screenshot1.png)
![screenshot2](docs/images/screenshot2.png)
![screenshot3](docs/images/screenshot3.png)

## ✨ Features

- **Dual Architecture** — Panel (control plane + Web UI) + Daemon (host agent), WSS + TLS fingerprint pinning
- **Instance Management** — Docker container instances: start/stop/restart/kill, auto-start & crash auto-restart
- **Browser Real-time Terminal** — xterm.js + WebSocket, real PTY, auto-reconnect, local line editing + Tab completion
- **One-click Deploy** — Built-in Vanilla / Paper / Purpur / Fabric / Forge / NeoForge templates
- **File Manager** — Chunked upload / streaming download / online editing / rename / copy / move / zip
- **Backup & Restore** — Instance-level zip snapshots with custom notes, backups count against disk quota
- **Managed Volumes** — Loopback fixed-size volumes for per-instance disk quotas
- **Resource Monitoring** — Real-time CPU / memory / disk dashboard + history charts, per-instance Docker stats
- **Auto Hibernation** — Idle detection → stop container → fake SLP listener with custom MOTD → wake on player connect
- **Node Groups** — Multi-node load scheduling, auto-select by disk availability + lowest memory usage
- **Scheduled Tasks** — Cron expressions: send command / start / stop / restart / backup
- **Users & Permissions** — admin / user roles, per-instance granular authorization (view/control/terminal/files)
- **API Keys** — `tps_` prefixed long-lived credentials with IP whitelist + scope restriction + expiration
- **SSO / OIDC** — Supports Logto / Google / Microsoft / Keycloak and any standard OIDC provider
- **Login CAPTCHA** — Cloudflare Turnstile / reCAPTCHA Enterprise
- **Security Hardening** — CSP / X-Frame-Options / SSRF protection / path traversal protection / rate limiting / graceful shutdown
- **Multi-language** — 中文 / English / 日本語 (926 keys, fully aligned)
- **Dark Theme** — Global dark / light mode toggle
- **Docker Image Management** — Pull / remove / custom display names with OCI label auto-detection

## 🏗️ Tech Stack

| Component | Technology |
|-----------|-----------|
| Panel Backend | Go 1.25 + Gin + GORM + SQLite |
| Daemon Backend | Go 1.25 + gorilla/websocket + Docker CLI |
| Frontend | React 18 + TypeScript + Vite 5 + Ant Design 5 |
| State Management | Zustand (persist + partialize) |
| i18n | i18next (zh / en / ja — 926 keys) |
| Terminal | xterm.js + WebSocket |
| SSO | OpenID Connect (go-oidc + PKCE) |
| Encryption | AES-256-GCM (secrets at-rest) + bcrypt (passwords) |
| TLS | Self-signed ECDSA cert + SHA-256 fingerprint pin (Panel↔Daemon) |

## 🚀 Quick Start

### Requirements

- **Go** ≥ 1.25
- **Node.js** ≥ 18 + npm
- **Docker Engine** (on Daemon hosts, for running game containers)
- **Linux** (Daemon production only; macOS/Windows for local development with degraded features)

### Deploy

```bash
git clone https://github.com/yourname/TapS.git
cd TapS

# 1. Build binaries
bash scripts/build.sh
# Output: dist/panel-linux-amd64, dist/daemon-linux-amd64, dist/web/

# 2. Deploy to server
scp dist/panel-linux-amd64 dist/daemon-linux-amd64 root@server:/opt/taps/
scp -r dist/web root@server:/opt/taps/web

# 3. Create systemd services and start
# See docs/deployment/single-host.md
```

### Local Development

```bash
# Terminal 1 — Daemon
cd packages/daemon && go run ./cmd/daemon

# Terminal 2 — Panel
cd packages/panel && go run ./cmd/panel

# Terminal 3 — Frontend (hot reload, :5173 proxies /api → :24444)
cd web && npm install && npm run dev
```

Default credentials: `admin` / `admin` (forced password change on first login).

### Configuration

#### Panel Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `TAPS_ADDR` | Listen address | `:24444` | No |
| `TAPS_DATA_DIR` | Data directory (DB, keys) | `./data` | No |
| `TAPS_WEB_DIR` | SPA static files directory | `./web` | No |
| `TAPS_ADMIN_USER` | Initial admin username | `admin` | No |
| `TAPS_ADMIN_PASS` | Initial admin password | `admin` | No |
| `TAPS_TLS_CERT` | TLS cert path (direct HTTPS) | — | No |
| `TAPS_TLS_KEY` | TLS key path | — | No |

#### Daemon Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `TAPS_DAEMON_ADDR` | Listen address | `:24445` | No |
| `TAPS_DAEMON_DATA` | Data directory | `./data` | No |
| `TAPS_REQUIRE_DOCKER` | Docker-only instances | `true` | No |

> Full configuration reference (including daemon config.json and admin UI settings): [docs/usage/settings.md](docs/usage/settings.md)

## 📖 Usage

1. After login, go to **System Settings** and configure **Panel Public URL** (required for SSO / terminal / CORS)
2. Pull Docker images on the **Images** page (e.g. `eclipse-temurin:21-jre`)
3. Create instances in **Instance Management**: use a template for one-click deploy, or customize Docker config
4. Instance detail page: terminal, file manager, backups, scheduled tasks

> Detailed guide: [docs/usage/quickstart.md](docs/usage/quickstart.md)

## 📁 Project Structure

```
TapS/
├── packages/
│   ├── shared/                  # Shared: Panel↔Daemon WS RPC protocol, rate limiter, TLS utils
│   ├── panel/                   # Panel backend
│   │   ├── cmd/panel/           #   Entry point + CLI (reset-auth-method)
│   │   └── internal/
│   │       ├── api/             #     All HTTP handlers + router + middleware
│   │       ├── auth/            #     JWT + API Key + bcrypt + middleware
│   │       ├── sso/             #     OIDC flow + PKCE store + state
│   │       ├── model/           #     DB models (User/Daemon/Task/...)
│   │       ├── store/           #     SQLite open + migrations + seed
│   │       ├── daemonclient/    #     WS connection to daemon + reconnect + fingerprint pin
│   │       ├── alerts/          #     Webhook dispatch + SSRF protection
│   │       ├── netutil/         #     ClassifyHost + SafeHTTPClient
│   │       ├── secrets/         #     AES-GCM encryption
│   │       └── ...
│   └── daemon/                  # Daemon backend
│       ├── cmd/daemon/          #   Entry point
│       └── internal/
│           ├── rpc/             #     WS RPC server + HTTP file endpoints
│           ├── instance/        #     Instance lifecycle (docker run / pty / restart)
│           ├── docker/          #     Docker CLI wrapper (list/pull/remove/stats)
│           ├── fs/              #     Virtual filesystem (mount + Resolve + symlink protection)
│           ├── backup/          #     Zip backup/restore + path validation
│           ├── volumes/         #     Loopback volume management (mkfs + mount)
│           ├── hibernation/     #     Auto-hibernate (SLP poller + fake server + wake)
│           └── ...
├── web/                         # React frontend
│   ├── src/
│   │   ├── i18n/                #   Translation files (zh.ts / en.ts / ja.ts)
│   │   ├── pages/               #   Page components
│   │   ├── components/          #   Shared components (Terminal/FileManager/...)
│   │   ├── api/                 #   API client layer
│   │   ├── stores/              #   Zustand stores (auth / brand / prefs)
│   │   └── layouts/             #   AppLayout (sidebar + header)
│   └── vite.config.ts
├── scripts/                     # Build + i18n check scripts
├── docs/                        # Documentation (English)
│   ├── zh/                      #   中文文档
│   └── ja/                      #   日本語ドキュメント
├── .github/workflows/ci.yml     # GitHub Actions CI
├── CHANGELOG.md                 # Version changelog
├── CONTRIBUTING.md              # Contribution guide
└── LICENSE                      # GPL-3.0
```

## 🔧 Development

```bash
# Frontend hot reload
cd web && npm run dev

# i18n alignment check (CI)
node scripts/i18n-gap-check.js

# Cross-compile production binaries
bash scripts/build.sh
# Output: dist/panel-linux-{amd64,arm64}, dist/daemon-linux-{amd64,arm64}, dist/web/
```

### Code Structure Overview

- **Panel** routes registered in `packages/panel/internal/api/router.go`
- **Daemon** RPC actions dispatched in `packages/daemon/internal/rpc/server.go`
- **Shared protocol** in `packages/shared/protocol/message.go`
- **Frontend routes** in `web/src/router.tsx`, page components in `web/src/pages/`

## 📄 API Documentation

100+ endpoints with full curl examples + response samples + field definitions:

- [API Overview](docs/api/overview.md) — Auth, error format, rate limiting, CORS, security headers
- [Endpoint Reference](docs/api/endpoints.md) — All HTTP / WebSocket endpoints

## 📚 Documentation

**[📖 Full Documentation](docs/README.md)** — Complete guide covering usage, deployment, operations, development, security, and API reference.

| Document | Content |
|----------|---------|
| [Quick Start](docs/usage/quickstart.md) | First login, configuration, creating instances |
| [System Settings](docs/usage/settings.md) | All 17 settings cards explained |
| [Single Host Deploy](docs/deployment/single-host.md) | Panel + Daemon on one machine |
| [Nginx Reverse Proxy](docs/deployment/nginx-https.md) | HTTPS + complete nginx config |
| [Security Architecture](docs/security/architecture.md) | Complete defense layer inventory |
| [Hardening Checklist](docs/security/best-practices.md) | Pre-launch must-do items |
| [Troubleshooting](docs/operations/troubleshooting.md) | Common issues |

> Documentation is also available in [中文](docs/zh/README.md) and [日本語](docs/ja/README.md).

## 🤝 Contributing

Contributions are welcome! See [Contributing Guide](CONTRIBUTING.md) for details.

> Report security vulnerabilities via email to **hi@mail.mctap.org** — do not open public issues.

## 📝 Changelog

See [CHANGELOG.md](CHANGELOG.md).

## 📜 License

This project is licensed under [GPL-3.0](LICENSE).
