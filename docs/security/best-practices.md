**English** | [中文](../zh/security/best-practices.md) | [日本語](../ja/security/best-practices.md)

# Deployment Hardening Checklist

Review before going live. Ordered from highest to lowest severity.

## Must-Do (P0)

- [ ] **TLS**: Use nginx reverse proxy + Let's Encrypt for Panel HTTPS ([guide](../deployment/nginx-https.md))
- [ ] **Change default password**: Change `admin/admin` immediately after first login
- [ ] **Block daemon public inbound** (if daemon and panel are on the same machine): change daemon to `addr=127.0.0.1:24445`, block external 24445 in cloud firewall
- [ ] **Configure trusted proxy list**: System Settings → Trusted Proxy List → add nginx host IP → restart panel. **Without this, rate limiting is effectively useless**
- [ ] **Configure Panel public URL**: System Settings → Panel Public URL → enter `https://yourdomain`. Without this, SSO callback / terminal WS origin check / CORS fallback all fail
- [ ] **Verify Daemon Token & TLS fingerprint**: when adding a node, **byte-for-byte verify** the fingerprint matches the daemon startup log output

## Strongly Recommended (P1)

- [ ] **Change default admin username**: set `TAPS_ADMIN_USER` to something other than `admin` (only effective on first seed)
- [ ] **Tighter rate limiting**: System Settings → Rate Limiting → change 5/min to 3/min; ban duration 5 → 15 min
- [ ] **Shorter JWT TTL**: System Settings → Session Lifetime → 60 → 30 minutes
- [ ] **Shorter WS heartbeat interval**: 5 → 2 minutes
- [ ] **Tighten CORS whitelist**: System Settings → CORS Allowed Origins → list only trusted frontend domains
- [ ] **Review CSP whitelist**: System Settings → Content Security Policy (CSP) → confirm script-src / frame-src only includes CAPTCHA CDNs you actually use
- [ ] **Webhook URL on dedicated domain**: only use trusted domain names
- [ ] **Regular database backups**: see [Backup & Recovery](../operations/backup-restore.md)
- [ ] **Monitor audit logs**: regularly check login logs for unusual IPs / excessive 401s

## Recommended (P2)

- [ ] **Separate accounts for node machines**: run daemon on dedicated VPS / VLAN
- [ ] **Firewall whitelist**: daemon 24445 only allows panel egress IP
- [ ] **API Key with IP whitelist + expiry**: CI keys at 90 days
- [ ] **Tighten HTTP timeouts**: defaults are adequate (10/60/120/120s); high-risk scenarios can use shorter values
- [ ] **Docker image mirrors**: use local/regional mirror accelerators
- [ ] **systemd unit limits**: `MemoryMax=`, `TasksMax=`, `PrivateTmp=true`
- [ ] **SELinux / AppArmor**
- [ ] **Host-level hardening**: disable root SSH login, SSH key-only, ufw default deny

## Optional (P3)

- [ ] **WAF**: Cloudflare / cloud provider WAF
- [ ] **VPN fallback**: WireGuard / Tailscale internal network, no public exposure

## 30-Second Pre-Launch Self-Check

```bash
# 1. Default password changed?
curl -s -X POST https://panel.example.com/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | grep -q "invalid_credentials" \
  && echo "✓ Default password changed" || echo "✗ Default password still active!"

# 2. HTTPS working?
curl -sI https://panel.example.com/healthz | grep -q "200 OK" \
  && echo "✓ HTTPS OK" || echo "✗ HTTPS issue"

# 3. Security headers?
curl -sI https://panel.example.com/ | grep -q "X-Frame-Options" \
  && echo "✓ Security headers present" || echo "✗ Security headers missing"

# 4. Daemon not publicly exposed?
nc -zv panel.example.com 24445 -w 3 2>&1 | grep -q "succeeded" \
  && echo "✗ Daemon 24445 is publicly open!" || echo "✓ Daemon 24445 closed"

# 5. Rate limiting working?
for i in 1 2 3 4 5 6; do
  curl -sw "%{http_code}\n" -o /dev/null -X POST https://panel.example.com/api/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"username":"x","password":"x"}'
done
# Attempts 5/6 should return 429
```

If any of the above five checks fail, **resolve them before accepting production traffic**.
