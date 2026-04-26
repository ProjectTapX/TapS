**English** | [中文](../zh/operations/troubleshooting.md) | [日本語](../ja/operations/troubleshooting.md)

# Troubleshooting

## Panel Won't Start

```bash
systemctl status taps-panel
journalctl -u taps-panel -n 50 --no-pager
```

| Symptom | Cause | Fix |
|---|---|---|
| `bind: address already in use` | Port occupied | `ss -lntp \| grep 24444` to find the process; change panel port (system settings or env) |
| `database is locked` | Another panel process running / SQLite file lock residue | `ps aux \| grep panel` and kill residuals; delete `panel.db-shm` `panel.db-wal` |
| `jwt.secret: permission denied` | Wrong file permissions | `chown root:root /var/lib/taps/panel/jwt.secret && chmod 600` |
| `panel listening on :2444` (port off by a digit) | `system.panelPort` was incorrectly modified in DB | `sqlite3 panel.db "UPDATE settings SET value='24444' WHERE key='system.panelPort'"` |

## Daemon Won't Start

| Symptom | Cause | Fix |
|---|---|---|
| `tls cert: ...` | Corrupted cert/key files | Delete `cert.pem` `key.pem` and restart; then re-probe fingerprint in Panel |
| `docker daemon not running` | Docker not started | `systemctl start docker` |
| `bind: permission denied` | Non-root + port < 1024 | systemd unit `User=root` |

## Panel Shows Node "Offline"

```bash
# On the Panel host
nc -zv <daemon-host> 24445       # Port reachable?
journalctl -u taps-panel -n 50 | grep daemon
```

| Error | Cause | Fix |
|---|---|---|
| `tls handshake: tls: failed to verify certificate` | TOFU-stored fingerprint ≠ daemon's actual fingerprint | Panel UI: edit node → Fetch Fingerprint → Accept → Save |
| `dial: ... connection refused` | Daemon not running / firewall blocking | On daemon host: `systemctl status taps-daemon` |
| `not pinned` | Node row has no certFingerprint | UI: edit → Fetch Fingerprint → Accept |

## Instance Won't Start

```bash
# On the daemon host
docker ps -a | grep taps-
docker logs taps-<uuid>
```

Common causes:
- `OCI runtime create failed: ... mounts: ... no such file or directory` → working directory was deleted; redeploy via Panel UI or fix directory
- `Address already in use` → instance's host port occupied by another process; change port in instance config
- `pull access denied` → wrong image name / registry unreachable
- `EULA must be accepted` → one-time; daemon auto-writes EULA=TRUE for itzg env; custom docker instances need it manually

## Terminal Won't Connect

| Symptom | Investigation |
|---|---|
| WebSocket connection immediately 401 | `?token=` expired (revoked / password changed) → re-login |
| Frontend terminal shows spinning loader | nginx not forwarding `Upgrade` header; check [nginx config](../deployment/nginx-https.md) |
| Terminal disconnects after 5 minutes | Your token was revoked by admin; re-login |

## Upload Failures

| Error Code | Cause |
|---|---|
| 413 `request_too_large` | `init` endpoint blocked by global 128 KiB limit? Check nginx `client_max_body_size` is ≥ 1100M |
| 507 `quota_exceeded` | Files + used > volume remaining space; expand volume or clean up |
| 410 unknown or expired uploadId | Single-chunk upload exceeded 1 hour without final; client should retry entire upload |
| 400 missing uploadId | Client didn't call `/upload/init` first; frontend version too old, refresh page |
| 400 path does not match init declaration | Upload session's path doesn't match chunk's path field |

## Rate Limiting 429

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 298
{"error":"rate_limited","retryAfter":298}
```

- Caused by cumulative login/changePw/API key failures reaching threshold (default 5/min)
- Wait for Retry-After seconds
- Emergency: `systemctl restart taps-panel` clears all in-memory counters

## All Client IPs Show 127.0.0.1 Behind Reverse Proxy

Possible causes (in troubleshooting priority order):

1. **nginx not forwarding real IP headers**: check nginx site config for these three lines:
   ```nginx
   proxy_set_header X-Real-IP         $remote_addr;
   proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
   proxy_set_header X-Forwarded-Proto $scheme;
   ```
   Missing any of these prevents Panel from getting the real client IP.

2. **Panel trusted proxy list not configured**: even if nginx sends `X-Forwarded-For`, gin doesn't trust the header by default. Go to "System Settings" → "Trusted Proxy List" → add nginx host IP (localhost default `127.0.0.1, ::1` already covers same-machine nginx) → Save.

3. **Panel not restarted**: `SetTrustedProxies` only takes effect at startup. Must `systemctl restart taps-panel` after saving.

4. **nginx on a remote host**: trusted list only has `127.0.0.1, ::1` but nginx runs on another machine (e.g., `10.0.0.5`) → add nginx's IP.

**Verification**: after accessing Panel, check the "Login Log" IP column — it should show your public IP, not `127.0.0.1`.

## SQLite Size Bloat

```bash
ls -lh /var/lib/taps/panel/panel.db
sqlite3 /var/lib/taps/panel/panel.db "VACUUM;"
```

- Check log limits: "System Settings" → "Log Capacity Limit" — is it set too high?
- Check audit_logs / login_logs row count: `sqlite3 panel.db "SELECT COUNT(*) FROM audit_logs"`

## "Token revoked" Keeps Appearing

Likely cause: admin frequently changing roles / passwords → each change bumps `tokens_invalid_before` → all previously issued JWTs are blanket-invalidated.

Normal behavior; no action needed. Users just need to re-login once.

## Auto-Hibernation: Players Can't Join

- Player connects in client → should see "Server is starting, please reconnect in ~30 seconds" kick message
- Daemon starts real container in background (check `journalctl -u taps-daemon -n 30`)
- Wait for `warmupMinutes` → player reconnects → can join

If players still can't join:
- Check instance logs: did it fail to start / EULA / port conflict
- Check daemon logs: is the hibernation manager reporting errors

## Where Are the Logs

```bash
# Panel
journalctl -u taps-panel -f
journalctl -u taps-panel -n 200 --no-pager

# Daemon
journalctl -u taps-daemon -f

# Instance container
docker logs -f taps-<uuid>

# Audit / login logs (requires login)
Browser → Panel → User Management → Audit Log / Login Log
```

## Database Locked

If the panel process panics, it may leave behind `panel.db-shm` `panel.db-wal`:

```bash
systemctl stop taps-panel
sqlite3 /var/lib/taps/panel/panel.db ".quit"   # consolidates WAL
systemctl start taps-panel
```

If still stuck: check if someone has a `sqlite3` shell open that hasn't been closed.

## Issue Not Listed Here

Open `journalctl -f` for both panel and daemon, reproduce the problem, and paste the logs to the developer / issue tracker.

## CORS

Monitoring tools / health checks / third-party dashboards receiving **403 Forbidden** from `/api/*` instead of expected data — most likely blocked by CORS.

**Symptoms**:
- Browser DevTools shows no `Access-Control-Allow-Origin: *` header; request blocked by SOP
- curl with `-H 'Origin: https://yourtool.example.com'` gets `HTTP/1.1 403`
- Panel logs show GIN response code 403

**Cause**: CORS only allows origins listed in "System Settings → CORS Allowed Origins". With an empty whitelist, only the Panel public URL (publicUrl) is allowed. Any request with an `Origin` header whose origin isn't in the list won't receive an ACAO header, and the browser will reject it.

**Fixes** (pick one):
1. **Add to whitelist (recommended)**: log into Panel → System Settings → CORS Allowed Origins → add the monitoring tool's origin (`https://prometheus.internal`, `https://uptime.example.com`, etc.) → Save. Takes effect immediately, no restart needed
2. **Temporary dev mode**: add `Environment=TAPS_CORS_DEV=1` to the systemd unit, restart panel — this opens wildcard CORS. **Development only, never use in production**
3. **Bypass Origin header**: if the monitoring tool can be configured not to send Origin headers (most cURL-based tools don't by default), this avoids triggering CORS entirely

**Notes**:
- API Key server-to-server calls without Origin headers are **completely unaffected by CORS** — most automation scenarios work this way by default
- Browser JS cross-origin access to Panel API (e.g., embedding Panel in iframe, third-party SPA calling Panel API) **must** use the whitelist
- Panel's own SPA runs on the same origin; its requests have Origin matching publicURL → always allowed, no extra configuration needed
