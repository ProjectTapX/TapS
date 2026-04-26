**English** | [中文](../zh/api/overview.md) | [日本語](../ja/api/overview.md)

# API Overview

Panel exposes both RESTful and WebSocket interfaces, all prefixed with `/api/`.

**Base URL (production example)**: `https://taps.example.com`
**Default port**: 24444 (HTTP, changeable / can use nginx)

## Authentication

Three credential types — pick one:

### 1. JWT Bearer Token

Obtained after login, placed in the `Authorization` header:

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

- HS256, secret in `data/jwt.secret` (auto-generated on first start)
- Default 1 hour (configurable 5–1440 minutes in system settings)
- Sliding renewal: when remaining < TTL/2, response header `X-Refreshed-Token` carries a new JWT
- After password change / role change / user deletion, old JWT is **immediately invalidated** (HTTP 401 `auth.token_revoked`)
- `alg: none` attacks are explicitly rejected

### 2. JWT in Query

Only for scenarios where the browser can't set headers (`<a href>` downloads, form uploads, WebSocket):

```
GET /api/daemons/1/files/download?token=<jwt>&path=/data/x.txt
```

Behaves identically to Bearer, including `tokens_invalid_before` revocation check.

### 3. API Key

`tps_`-prefixed fixed credentials, using the Bearer header:

```http
Authorization: Bearer tps_3fe3c349dd703a4c...
```

- Permanent or with expiry; can be revoked
- Supports IP whitelist + Scope
- See [API Key](../usage/api-keys.md) for details

## Error Format

All errors return uniform JSON with **stable error codes** (`domain.snake_case` format):

```json
{ "error": "auth.invalid_credentials", "message": "invalid credentials" }
```

Some errors include parameters:

```json
{ "error": "auth.rate_limited", "message": "...", "params": { "retryAfter": 298 } }
```

```json
{ "error": "common.request_too_large", "message": "...", "params": { "maxBytes": 131072 } }
```

Error codes can be directly used for frontend i18n lookup: `t('errors.' + error)`.

### Common Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 400 | Bad request / parameter validation failure |
| 401 | Missing / invalid / revoked / expired credentials |
| 403 | Authenticated but insufficient permissions (role / scope / instance perm) |
| 404 | Resource not found |
| 405 | Method not allowed (JSON body `common.method_not_allowed`) |
| 409 | Conflict (duplicate username/email, upload path occupied, etc.) |
| 410 | Upload session expired |
| 413 | Request body too large |
| 429 | Rate limited; response header includes `Retry-After: <seconds>` |
| 502 | Daemon unreachable / daemon upstream error |

## Rate Limiting

| Bucket | Default Threshold | Default Ban | Configurable At |
|--------|------------------|-------------|-----------------|
| Login failure | 5/min/IP | 5 minutes | System Settings → Rate Limiting |
| Password change failure | Same | Same | Same |
| API Key failure | Same | Same | Same |
| OAuth Start | 30/5min/IP | 5 minutes | Same |
| Daemon Token failure | 10/min/IP | 10 minutes | Daemon config |

Each failure adds an extra sleep (exponential backoff, max 3 seconds). Successful authentication clears that IP's failure count.

## Request Body Size

| Endpoint | Limit | Configuration |
|----------|-------|--------------|
| Global (except exemptions) | 128 KiB | System Settings → Request Size Limits |
| `POST /daemons/:id/fs/write` | 16 MiB | Same, maxJsonBodyBytes |
| `POST /daemons/:id/files/upload` single chunk | 1 GiB | Daemon hard limit |
| `POST /settings/brand/favicon` | 64 KiB | Hardcoded |
| `POST /settings/hibernation/icon` | 32 KiB | Hardcoded |

WebSocket single frame ≤ 16 MiB (controlled separately by panel system settings / daemon config).

## CORS

- Allowed origins: domains configured in System Settings → CORS Allowed Origins + Panel's own publicUrl
- Allowed headers: `Origin, Content-Type, Authorization`
- Allowed methods: `GET, POST, PUT, DELETE, OPTIONS`
- **Exposed response headers**: `X-Refreshed-Token, Retry-After, Content-Disposition`
- Development: `TAPS_CORS_DEV=1` temporarily opens wildcard

## Security Headers

Every response automatically includes:

| Header | Value |
|--------|-------|
| Content-Security-Policy | `default-src 'self'; script-src 'self' + configurable whitelist; ...` |
| X-Frame-Options | `SAMEORIGIN` |
| X-Content-Type-Options | `nosniff` |
| Referrer-Policy | `strict-origin-when-cross-origin` |
| Strict-Transport-Security | Only sent over HTTPS |

CSP script-src / frame-src is hot-configurable in System Settings → Content Security Policy (CSP).

## TLS

- **Panel**: HTTP by default; provide `TAPS_TLS_CERT` + `TAPS_TLS_KEY` for HTTPS; nginx reverse proxy recommended
- **Daemon**: HTTPS enforced (self-signed 99-year ECDSA certificate, Panel pins SHA-256 fingerprint)

## WebSocket Endpoints

| Path | Purpose | Auth |
|------|---------|------|
| `GET /api/ws/instance/:id/:uuid/terminal` | Real-time terminal | `?token=<jwt>` + PermView (read-only) / PermTerminal (read-write) |

- Origin check: must match Panel public URL (rejected if not configured)
- Read timeout + pong handler: configurable (default 60s)
- Input token bucket: configurable (default 200/s burst 50)

## Path Parameter Conventions

- `:id` = Node ID (uint)
- `:uuid` = Instance UUID (8-4-4-4-12 hex)
- `:taskId` = Scheduled task ID (uint)
- `:ref` = Docker image reference (repository:tag, URL-encoded)

## Time Format

All timestamps use RFC 3339: `2026-04-23T18:55:07.020890690-04:00`.
