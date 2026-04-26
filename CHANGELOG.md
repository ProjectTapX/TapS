**English** | [中文](CHANGELOG.zh-CN.md) | [日本語](CHANGELOG.ja.md)

# Changelog

This file documents the major changes in each TapS release. Format based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [26.1.0] - 2026-04-26

First public release.

### Added

- **Panel + Daemon dual architecture**: Panel (Go + Gin + GORM + SQLite) handles Web UI and centralized management; Daemon (Go + gorilla/websocket + Docker CLI) runs containers on host machines
- **React frontend**: Vite 5 + React 18 + TypeScript + Ant Design 5 with dark/light theme switching
- **Instance management**: Docker container instance creation/start/stop/force-kill/auto-start/crash auto-restart
- **Browser real-time terminal**: xterm.js + WebSocket, real PTY, auto-reconnect on disconnect, local line editing + Tab completion
- **One-click deploy templates**: Vanilla / Paper / Purpur / Fabric / Forge / NeoForge — pick a version and deploy
- **File manager**: Chunked upload / streaming download / online editing / rename / copy / move / zip compress & extract
- **Backup & restore**: Instance-level zip snapshots with notes, backups count toward disk quota
- **Managed volumes**: Loopback fixed-size volumes giving each instance its own disk quota
- **Resource monitoring**: Node CPU/memory/disk real-time dashboard + historical graphs, per-instance Docker stats
- **Auto-hibernation**: Idle detection → stop container → fake SLP listener → wake on player connect
- **Node grouping**: Multi-node load scheduling, auto-select by available disk + lowest memory
- **Scheduled tasks**: Cron expressions, actions: command / start / stop / restart / backup
- **Users & permissions**: admin / user roles with per-instance granular authorization
- **API Key**: `tps_`-prefixed long-lived credentials with IP whitelist + scope + expiration
- **SSO / OIDC**: Supports Logto / Google / Microsoft / Keycloak and any standard OIDC provider, PKCE + HMAC state
- **Login CAPTCHA**: Cloudflare Turnstile / reCAPTCHA Enterprise
- **Docker image management**: Pull / delete / custom display names, automatic OCI label reading
- **Multi-language**: Chinese / English / Japanese (926 keys aligned across all three)
- **Webhook notifications**: Push JSON on node offline/online events (60s debounce)

### Security

- Content-Security-Policy (admin-configurable script-src / frame-src whitelists)
- X-Frame-Options / X-Content-Type-Options / Referrer-Policy / conditional HSTS
- SSRF protection: ClassifyHost tri-classification + DialContext DNS rebinding recheck
- Path traversal protection: EvalSymlinks + containedIn + O_NOFOLLOW + zip symlink rejection
- JWT: HS256 + sliding renewal + TokensInvalidBefore revocation + alg:none rejection
- bcrypt password hashing + AES-GCM secrets at-rest
- Rate limiting: login / changePw / apiKey / oauthStart independent buckets
- WS dispatch concurrency cap (default 8192) + WS frame size limit
- HTTP server timeouts (slow-loris protection)
- Graceful shutdown: SIGTERM → srv.Shutdown → hib.Shutdown → vm.UnmountAll
- All multi-key settings writes wrapped in db.Transaction
- 99 cumulative security hardening items, six audit rounds rated A
