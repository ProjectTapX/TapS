**English** | [中文](../zh/security/architecture.md) | [日本語](../ja/security/architecture.md)

# Security Architecture

## High-Level Model

```
Browser ──HTTPS──▶ [nginx/Caddy] ──HTTP──▶ Panel (:24444)
                                           │  wss + TLS fingerprint pin
                                           ▼
                                        Daemon (:24445, self-signed TLS)
                                           │
                                        Docker Engine
```

Panel is the center of all authentication/authorization decisions; Daemon only trusts Panel's shared token. Browser authenticates with Panel via JWT; Panel authenticates with Daemon via TLS + shared token.

---

## Defense Layers

### HTTP Security Headers

All responses automatically include (see `panel/internal/api/security_headers.go`):

| Header | Value | Purpose |
|--------|-------|---------|
| Content-Security-Policy | `default-src 'self'; script-src 'self' + configurable whitelist; ...` | Prevent XSS external script injection |
| X-Frame-Options | `SAMEORIGIN` | Prevent clickjacking |
| X-Content-Type-Options | `nosniff` | Prevent MIME sniffing attacks |
| Referrer-Policy | `strict-origin-when-cross-origin` | Prevent Referer leakage |
| Strict-Transport-Security | `max-age=31536000; includeSubDomains` (HTTPS only) | Enforce HTTPS |

CSP script-src / frame-src whitelists are hot-configurable in the admin panel (takes effect immediately, no restart needed).

### Authentication

| Measure | Description |
|---------|-------------|
| JWT HS256 | Random secret (`jwt.secret` file, generated on first start) |
| bcrypt cost 10 | Password hashing |
| Dummy-hash timing equalization | Non-existent users still run one bcrypt comparison to prevent timing attacks for username enumeration |
| Sliding renewal | Auto-issues new token in `X-Refreshed-Token` response header when JWT remaining < TTL/2 |
| Token revocation | `TokensInvalidBefore` field; set to current iat-1s on password change / admin demotion |
| MustChangePassword | Forced password change on first login |
| `alg: none` rejection | jwt-go ParseToken explicitly rejects none algorithm |

### Authorization

| Layer | Implementation |
|-------|---------------|
| Role | admin / user, `auth.RequireRole()` middleware |
| Per-instance permissions | PermView / PermControl / PermTerminal / PermFiles |
| API Key Scope | `RequireScope()` middleware, comma-separated scope tags |

### SSO / OIDC

| Measure | Description |
|---------|-------------|
| PKCE server-side store | Verifier not in URL, stored in Panel process memory (10-minute TTL) |
| HMAC state | provider + nonce + expiry + HMAC-SHA256 signature |
| Nonce binding | id_token.nonce must match nonce in state |
| Email ToLower | Lowercased at entry, preventing case variation bypasses of admin auto-bind guard |
| Admin auto-bind rejection | Local accounts with existing admin email disallow IdP auto-binding |
| Email domain whitelist | Per-provider configurable allowed domain list |
| CallbackError typed codes | URL fragment only passes stable codes, never leaking IdP internal errors to browser |
| clientSecret encrypted storage | AES-GCM at-rest |

### Input Validation

| Validation | Location |
|------------|----------|
| ValidImage regex + `--` separator | Docker CLI flag injection prevention |
| validInstanceUUID | Before all `taps-<uuid>` docker commands |
| validBackupName regex | Backup filenames |
| validSiteName character whitelist | Brand name |
| normalizeEmail / normalizeUsername | Unified lowercase + trim |
| LOWER() unique indexes | SQLite unique indexes use `lower()` function |

### Path Security (File Operations)

| Measure | Description |
|---------|-------------|
| fs.Resolve EvalSymlinks | Double symlink resolution + containment check |
| containedIn dual-root | Backup restore target must be under instancesRoot or volumesRoot |
| Zip/Copy symlink containment | EvalSymlinks → if within mount then follow, if escapes then skip + log |
| O_NOFOLLOW | Zip extraction / backup restore use nofollow flag when opening files |
| isProtectedBackingFile | Rejects direct fs operations on `.img` / `.json` volume backing files |
| Zip entry rejection | Rejects symlink entries / leading `/` / `..` segments |

### SSRF Protection

| Scenario | Measures |
|----------|----------|
| Webhook URL | ClassifyHost tri-classification (public / private / DNS-failed) + DialContext recheck |
| SSO Test | Same + SafeHTTPClient against DNS rebinding |

### Data Protection

| Measure | Coverage |
|---------|----------|
| AES-GCM at-rest | Captcha secret, SSO clientSecret |
| Independent key | sso-state.key independent of jwt.secret |
| bcrypt | User passwords |
| crypto/rand | All random number generation |

### DoS Protection

| Measure | Configuration |
|---------|--------------|
| Per-IP rate limiting (login / changePw / apiKey) | Rate Limiting card |
| OAuth-start budget | Rate Limiting card |
| PKCE store maxEntries | Rate Limiting card |
| WS dispatch semaphore 8192 | Daemon config |
| WS frame size cap | Request Size Limits / daemon config |
| HTTP server timeouts | HTTP Timeouts card / daemon config |
| Request body cap | Request Size Limits card |
| Hibernation icon cache + rate limit | Rate Limiting card |

### Transaction Consistency

The following multi-key settings writes are all wrapped in `db.Transaction`:
- SetCaptchaConfig, SetLimits, SetAuthTimings, SetRateLimit, SetHTTPTimeouts
- daemon.Delete (cascades InstancePermission / Task / NodeGroupMember)
- groups.Delete (cascades NodeGroupMember)
- User.Update / User.Delete (clause.Locking)

### Frontend Security

| Measure | Description |
|---------|-------------|
| i18next escapeValue: true | Global HTML escaping |
| CSP script-src 'self' | Restricts executable script sources |
| 926 i18n keys aligned | zh / en / ja fully matched |
| Unified error codes | Backend apiErr(code, msg), frontend formatApiError auto-looks up i18n |
| Partialize persist | zustand only persists token + {id, username, role} |
| Terminal token re-read | Re-reads latest token on every WS reconnect |
| waitFor timeout | Captcha SDK loading 5-second timeout |
| ChunkErrorBoundary | getDerivedStateFromError returns null (doesn't throw) |

### Operational Security

| Measure | Description |
|---------|-------------|
| Graceful shutdown | SIGTERM → srv.Shutdown(30s) → hib.Shutdown → vm.UnmountAll |
| systemd TimeoutStopSec=30s + KillSignal=SIGTERM | Works with graceful shutdown |
| MountAll synchronous | Daemon waits for all loopback mounts to complete before accepting requests |

---

## Audit History

As of 2026-04-26, six rounds of manual/AI audit with 99 cumulative fixes. Current rating: **A** (0 Critical / 0 High / 0 Medium / 0 Low open vulnerabilities).
