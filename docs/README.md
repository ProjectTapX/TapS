**English** | [中文](zh/README.md) | [日本語](ja/README.md)

# TapS Documentation

TapS is a game server management panel for Minecraft (and other Docker-containerized processes), consisting of **Panel** (control plane + Web UI) and **Daemon** (host agent on each target machine). The Panel communicates securely with Daemons via WSS + fingerprint pinning, providing instance orchestration, file management, backups, monitoring, scheduled tasks, auto-hibernation, API Keys, users & permissions, and more.

---

## 📦 Deployment

| Document | Scenario |
|----------|----------|
| [Single Host Deploy](deployment/single-host.md) | Panel + Daemon on one machine |
| [Panel Only](deployment/panel-only.md) | Centralized Panel, remote Daemons |
| [Daemon Only](deployment/daemon-only.md) | Add a new node to existing Panel |
| [Nginx + HTTPS](deployment/nginx-https.md) | Reverse proxy with Let's Encrypt |

---

## 🚀 Usage

| Document | Content |
|----------|---------|
| [Quick Start](usage/quickstart.md) | First login, add node, create instance |
| [Instances](usage/instances.md) | Create / start-stop / terminal / deploy |
| [Files & Backups](usage/files.md) | File browser, upload/download, backup/restore |
| [Users & Permissions](usage/users-permissions.md) | Roles, per-instance authorization |
| [API Keys](usage/api-keys.md) | Issue, expire, revoke, scopes |
| [SSO / OIDC](usage/sso-oidc.md) | Connect to Logto / Google / Microsoft / Keycloak |
| [System Settings](usage/settings.md) | All 17 settings cards explained |

---

## 🔌 API

| Document | Content |
|----------|---------|
| [API Overview](api/overview.md) | Auth, error format, rate limiting, CORS, security headers |
| [Endpoint Reference](api/endpoints.md) | All HTTP / WebSocket endpoints (100+) |

---

## 🛠 Operations

| Document | Content |
|----------|---------|
| [Upgrade](operations/upgrade.md) | Rolling upgrade Panel / Daemon |
| [Backup & Restore](operations/backup-restore.md) | DB / data dir / disaster recovery |
| [Troubleshooting](operations/troubleshooting.md) | Common issues |

---

## 🔒 Security

| Document | Content |
|----------|---------|
| [Security Architecture](security/architecture.md) | Complete defense layer inventory |
| [Hardening Checklist](security/best-practices.md) | Pre-launch must-do items |

---

## 🧑‍💻 Development

| Document | Content |
|----------|---------|
| [Building from Source](development/build.md) | Local build, cross-compile |
| [Project Structure](development/architecture.md) | Module layout, key code walkthrough |

---

## Feedback & Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md). Report security vulnerabilities to **hi@mail.mctap.org**.
