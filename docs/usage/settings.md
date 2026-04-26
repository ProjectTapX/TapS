**English** | [中文](../zh/usage/settings.md) | [日本語](../ja/usage/settings.md)

# System Settings Reference

The **System Settings** page (admin only). All settings are persisted in the SQLite `settings` table (key/value text).

## Card Order & Settings Overview

Top to bottom:

| # | Card | Key Settings | Default | Takes Effect |
|---|------|-------------|---------|-------------|
| 1 | **Site Branding** | siteName | `TapS` | Immediately |
| | | favicon (PNG/ICO) | None | Immediately |
| 2 | **Panel Public URL** | publicUrl | Empty | Immediately |
| 3 | **Panel Listen Port** | port | 24444 | **Requires restart** |
| 4 | **Trusted Proxy List** | proxies | `127.0.0.1, ::1` | **Requires restart** |
| 5 | **CORS Allowed Origins** | origins | Empty | Immediately |
| 6 | **Login CAPTCHA** | provider / siteKey / secret / scoreThreshold | `none` / empty / encrypted / 0.5 | Immediately |
| 7 | **Login Method** | method | `password-only` | Immediately |
| 8 | **SSO Providers (OIDC)** | provider list | — | Immediately |
| 9 | **Server Download Source** | source | `fastmirror` | Immediately |
| 10 | **Minecraft Java Auto-Hibernation** | defaultEnabled / minutes / warmup / motd / kick / icon | true / 60 / 5 | Immediately |
| 11 | **Webhook Notifications** | url / allowPrivate | empty / false | Immediately |
| 12 | **Log Capacity Limit** | auditMaxRows / loginMaxRows | 1000000 | Immediately |
| 13 | **Rate Limiting** | rateLimitPerMin / banDurationMinutes | 5 / 5 | Real-time |
| | | oauthStartCount / oauthStartWindowSec | 30 / 300 | Real-time |
| | | pkceStoreMaxEntries | 10000 | Real-time |
| | | terminalReadDeadlineSec / inputRatePerSec / inputBurst | 60 / 200 / 50 | New WS connections |
| | | iconCacheMaxAgeSec / iconRatePerMin | 300 / 10 | Immediately |
| 14 | **Request Size Limits** | maxRequestBodyBytes / maxJsonBodyBytes / maxWsFrameBytes | 128 KiB / 16 MiB / 16 MiB | Real-time |
| 15 | **Content Security Policy (CSP)** | scriptSrcExtra / frameSrcExtra | Cloudflare + reCAPTCHA CDN | Immediately |
| 16 | **Session Lifetime** | jwtTtlMinutes / wsHeartbeatMinutes | 60 / 5 | New sessions |
| 17 | **HTTP Timeouts (anti slow-loris)** | readHeaderTimeoutSec / readTimeoutSec / writeTimeoutSec / idleTimeoutSec | 10 / 60 / 120 / 120 | **Requires restart** |

---

## Details

### Site Branding

- **siteName**: Name displayed in browser title and login page hero area. Character whitelist: letters, digits, CJK characters, common punctuation. CJK characters count as weight 2; limit is 16 weight units (i.e., max 16 ASCII characters or 8 CJK characters).
  - See `panel/internal/api/settings.go validSiteName()`
- **favicon**: Upload PNG / ICO ≤ 64 KiB. **SVG not allowed** (disabled to prevent stored XSS). Server always uses `http.DetectContentType` to sniff the real type, ignoring client Content-Type.
  - See `panel/internal/api/settings.go SetBrandFavicon()`

### Panel Public URL

The Panel's external access URL (including protocol), e.g., `https://taps.example.com`. Multiple features depend on it:

1. **SSO/OIDC callback address**: `<publicUrl>/api/oauth/callback/<provider>`
2. **Terminal WebSocket origin check**: if not set, terminal sessions are rejected
3. **CORS allowed origin fallback**: when the CORS whitelist is empty, publicUrl is used for same-origin comparison

- See `panel/internal/api/panel_public_url.go`

### Panel Listen Port

Written to DB; takes effect after restarting the panel process. Priority: DB > env (`TAPS_ADDR`) > default 24444.

### Trusted Proxy List

Only needed when panel is behind nginx / Caddy / Cloudflare. Without it, `c.ClientIP()` always returns `127.0.0.1`, causing rate limiting / audit / API Key IP whitelist to all malfunction. **Panel must be restarted** after changes.
- See `panel/internal/api/trusted_proxies_settings.go`

### CORS Allowed Origins

Comma-separated origin list (`scheme://host[:port]`). Only browser JS from listed domains can make cross-origin calls to the Panel API. When empty, only the Panel's own publicUrl is allowed (same-origin SPA always passes). API Key server-to-server calls are not affected. Takes effect immediately.
- See `panel/internal/api/cors_settings.go`

### Login CAPTCHA

Only applies to the **login endpoint**.

| Provider | Description |
|----------|-------------|
| `none` | Disabled |
| `turnstile` | Cloudflare Turnstile |
| `recaptcha` | Google reCAPTCHA Enterprise |

**Key behaviors**:
- **Fail-open**: key-level errors (wrong secret, mismatched site key) → `ErrConfig` → login allowed to prevent lockout; network errors/5xx → fail-closed, login rejected
- **Secret encrypted storage**: captcha secret is AES-GCM encrypted in `captcha.secretEnc` column. Admin GET returns `hasSecret: true/false`, **never echoes secret plaintext**
- **Provider switch forces secret reset**: switching from Turnstile to reCAPTCHA (or vice versa), backend rejects empty-secret PUT; frontend auto-clears siteKey + secret inputs
- **scoreThreshold 0 allowed**: `*float64` pointer type; nil = keep old value, 0 = disable threshold (all reCAPTCHA tokens pass), 0.1-0.9 normal threshold

### Login Method

| Value | Description |
|-------|-------------|
| `password-only` | Password login only (even if SSO providers are configured, login page doesn't show SSO buttons) |
| `oidc+password` | Both password + SSO supported |
| `oidc-only` | SSO only (password entry disabled; requires at least one enabled provider + at least one admin bound) |

Recovery (when admin is locked out by oidc-only):
```bash
taps-panel reset-auth-method --to password-only --data-dir /var/lib/taps/panel
```

### SSO Providers (OIDC)

See [SSO / OIDC documentation](sso-oidc.md).

### Server Download Source

| Value | Description |
|-------|-------------|
| `fastmirror` | FastMirror mirror (China-friendly) |
| `official` | Mojang / PaperMC official source (panel needs direct overseas access) |

### Minecraft Java Auto-Hibernation

See [Instance Management → Hibernation](instances.md).

### Webhook Notifications

Monitors **Daemon node** (not instance) connectivity. When a Daemon is disconnected from Panel for **over 60 seconds continuously**, sends `node.offline`; on reconnect, sends `node.online` (only if offline was previously sent).

```json
{ "event": "node.offline", "timestamp": 1714000000, "payload": { "daemonId": 1, "name": "node-a", "address": "10.0.0.5:24445" } }
```

- **SSRF protection**: ClassifyHost tri-classification (public / private / DNS-failed) + DialContext recheck. Admin can check "Allow private/loopback addresses" to permit internal webhooks
- **allowPrivate**: only enable when the webhook receiver is genuinely on an internal and trusted network

### Log Capacity Limit

`loglimit.Manager` checks audit_logs / login_logs row count every 60 seconds; deletes oldest when exceeded.

### Rate Limiting

> Card renamed from "Login Rate Limiting" to "Rate Limiting" (2026-04-26), as the content scope is broader.

**Authentication rate limiting** (3 independent buckets sharing thresholds):
| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| rateLimitPerMin | 5 | 1-100 | Failed attempts per IP per minute (login / changePw / apiKey each counted independently) |
| banDurationMinutes | 5 | 1-1440 | Ban duration after threshold exceeded |

**OAuth start rate limiting** (anonymous endpoint to prevent PKCE store flooding):
| Setting | Default | Range |
|---------|---------|-------|
| oauthStartCount | 30 | 1-1000 |
| oauthStartWindowSec | 300 | 30-3600 |
| pkceStoreMaxEntries | 10000 | 100-1000000 |

**Terminal WebSocket** (per-connection token bucket):
| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| terminalReadDeadlineSec | 60 | 10-600 | Max idle time between frames (including pong) |
| terminalInputRatePerSec | 200 | 1-5000 | Input frames allowed per second |
| terminalInputBurst | 50 | 1-5000 | Burst budget (for pasting commands) |

**Hibernation icon public endpoint**:
| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| iconCacheMaxAgeSec | 300 | 0-86400 | Cache-Control max-age |
| iconRatePerMin | 10 | 1-1000 | Requests per IP per minute |

### Request Size Limits

| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| maxRequestBodyBytes | 128 KiB | 1 KiB - 4 MiB | Global request body limit (Content-Length checked first; exceeding returns 413) |
| maxJsonBodyBytes | 16 MiB | 1-128 MiB | For large JSON endpoints like fs/write |
| maxWsFrameBytes | 16 MiB | 1-128 MiB | Panel terminal WS frame limit |

Paths exempt from global limit: `*/fs/write`, `*/files/upload*`, `*/brand/favicon`, `*/hibernation/icon`.

### Content Security Policy (CSP)

Content-Security-Policy tells the browser which domains are allowed to load scripts and embed iframes. `'self'` is always included and cannot be removed.

| Setting | Default | Description |
|---------|---------|-------------|
| scriptSrcExtra | `https://challenges.cloudflare.com, https://www.recaptcha.net` | External domains allowed to load scripts |
| frameSrcExtra | `https://challenges.cloudflare.com, https://www.google.com, https://www.recaptcha.net` | External domains allowed to embed iframes |

Full generated CSP header:
```
default-src 'self'; script-src 'self' <scriptSrcExtra...>; style-src 'self' 'unsafe-inline'; frame-src 'self' <frameSrcExtra...>; img-src 'self' data:; connect-src 'self' ws: wss:; font-src 'self'
```

- `style-src 'unsafe-inline'`: required for antd CSS-in-JS runtime `<style>` tag injection
- `connect-src ws: wss:`: required for terminal WebSocket connections

**Other security headers** (automatic, non-configurable):
- `X-Frame-Options: SAMEORIGIN`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Strict-Transport-Security`: only sent when panel has its own TLS cert, or request includes `X-Forwarded-Proto: https` (nginx reverse proxy)

See `panel/internal/api/security_headers.go`

### Session Lifetime

| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| jwtTtlMinutes | 60 | 5-1440 | JWT lifetime; auto-renews when remaining < TTL/2 |
| wsHeartbeatMinutes | 5 | 1-60 | Terminal WS re-validates TokensInvalidBefore interval |

### HTTP Timeouts (Anti Slow-Loris)

Four `http.Server` timeout parameters. WebSocket connections are not subject to these timeouts after Hijack. **Panel must be restarted** after changes.

| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| readHeaderTimeoutSec | 10 | 1-3600 | Total time from connection to header read complete |
| readTimeoutSec | 60 | 1-3600 | Total read time including body |
| writeTimeoutSec | 120 | 1-3600 | From header read complete to response write complete |
| idleTimeoutSec | 120 | 1-3600 | Keep-alive idle hold time |

---

## Configuration Not in the UI

Via environment variables or daemon `config.json`:

### Panel Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TAPS_DATA_DIR` | Panel data directory | `./data` |
| `TAPS_WEB_DIR` | Web static directory | `./web` |
| `TAPS_ADDR` | Listen host:port (overridden by DB port) | `:24444` |
| `TAPS_ADMIN_USER` / `TAPS_ADMIN_PASS` | First-time seed only | `admin` / `admin` |
| `TAPS_TLS_CERT` / `TAPS_TLS_KEY` | Enable HTTPS (when not using nginx) | — |
| `TAPS_CORS_DEV` | `=1` opens CORS wildcard (development use) | — |

### Daemon Environment Variables / config.json

All env vars can be overridden by `<DataDir>/config.json` (JSON priority > env > defaults).

| Variable / JSON key | Description | Default | Range |
|---------------------|-------------|---------|-------|
| `TAPS_DAEMON_DATA` | Daemon data directory | `./data` | — |
| `TAPS_DAEMON_ADDR` / `addr` | Listen host:port | `:24445` | — |
| `TAPS_REQUIRE_DOCKER` / `requireDocker` | Reject non-docker instances | `true` | bool |
| `TAPS_DAEMON_RL_THRESHOLD` / `rateLimitThreshold` | Token validation failure threshold | 10 | 1-1000 |
| `TAPS_DAEMON_RL_BAN_MINUTES` / `rateLimitBanMinutes` | Ban duration | 10 | 1-1440 |
| `TAPS_DAEMON_MAX_WS_FRAME_BYTES` / `maxWsFrameBytes` | WS frame limit | 16 MiB | 1-128 MiB |
| `TAPS_DAEMON_WS_DISPATCH_CONCURRENCY` / `wsDispatchConcurrency` | Per-session dispatch concurrency limit | 8192 | 1-65536 |
| `TAPS_DAEMON_HTTP_READ_HEADER_TIMEOUT_SEC` / `httpReadHeaderTimeoutSec` | HTTP read header timeout | 10 | 1-3600 |
| `TAPS_DAEMON_HTTP_READ_TIMEOUT_SEC` / `httpReadTimeoutSec` | HTTP read body timeout | 60 | 1-3600 |
| `TAPS_DAEMON_HTTP_WRITE_TIMEOUT_SEC` / `httpWriteTimeoutSec` | HTTP write timeout | 120 | 1-3600 |
| `TAPS_DAEMON_HTTP_IDLE_TIMEOUT_SEC` / `httpIdleTimeoutSec` | HTTP idle timeout | 120 | 1-3600 |

Daemon auto-writes `config.json.template` to the data directory on startup, containing all supported fields and defaults for admin to copy and edit.
