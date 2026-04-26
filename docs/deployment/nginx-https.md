**English** | [中文](../zh/deployment/nginx-https.md) | [日本語](../ja/deployment/nginx-https.md)

# Nginx Reverse Proxy + HTTPS

Put Panel behind nginx with Let's Encrypt certificates for HTTPS. **No changes needed on the Daemon side** (Daemon already uses wss + self-signed certificate + fingerprint pinning, with Panel initiating the TLS connection).

## Architecture

```
Browser ──https──> nginx(443) ──http──> panel(127.0.0.1:24444)
                                          │
                                          └──wss──> daemon(*:24445)  [self-signed + fingerprint pin]
```

## 1. Bind Panel to Loopback Only

Prevent direct access to port 24444 bypassing nginx. Two approaches:

- **Edit systemd unit**: `Environment=TAPS_ADDR=127.0.0.1:24444` → `systemctl restart taps-panel`
- **Or in System Settings**: "Panel Listen Port" currently only configures the port (without host); use the former to lock to loopback

## 2. Install nginx + certbot

```bash
apt-get install -y nginx certbot python3-certbot-nginx
```

## 3. nginx Site Configuration

```bash
cat >/etc/nginx/sites-available/taps <<'EOF'
upstream taps_panel {
    server 127.0.0.1:24444;
    keepalive 16;
}

# WebSocket upgrade detection: only set Connection: upgrade
# when the client sends an Upgrade header.
# Regular HTTP requests use Connection: close.
# Hardcoding "upgrade" for all requests can confuse some CDN/proxies.
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    listen [::]:80;
    server_name taps.example.com;
    # certbot will modify this block; allow ACME, 301 everything else to HTTPS
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name taps.example.com;

    # certbot will auto-fill these two lines
    # ssl_certificate     /etc/letsencrypt/live/taps.example.com/fullchain.pem;
    # ssl_certificate_key /etc/letsencrypt/live/taps.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    # TLS performance: session cache + OCSP stapling
    ssl_session_cache   shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_stapling        on;
    ssl_stapling_verify on;

    # Upload chunks can reach 1 GiB (daemon-side limit) + metadata headroom
    client_max_body_size 1100M;

    # Timeout settings
    proxy_connect_timeout 10s;       # localhost to panel is plenty; increase for remote proxy
    proxy_read_timeout    3600s;     # WebSocket long connections + SSE streaming progress
    proxy_send_timeout    3600s;     # Large file uploads

    # gzip: compress Panel's JSON / JS / CSS at the nginx layer
    gzip              on;
    gzip_vary         on;
    gzip_min_length   256;
    gzip_proxied      any;
    gzip_types        text/plain application/json application/javascript text/css
                      application/xml text/xml image/svg+xml;

    # Security headers
    # Panel already includes CSP / X-Frame-Options / nosniff / Referrer-Policy.
    # Panel auto-adds HSTS when it detects X-Forwarded-Proto: https.
    # The header below is redundant but harmless — remove if you prefer
    # Panel's built-in headers only.
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    location / {
        proxy_pass http://taps_panel;
        proxy_http_version 1.1;

        # Required: let panel resolve real client IP from X-Forwarded-For
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Required: terminal WebSocket upgrade (uses map variable; non-WS requests get close)
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        # Disable buffering for streaming downloads / SSE / large file uploads
        proxy_buffering         off;
        proxy_request_buffering off;
    }
}
EOF

ln -s /etc/nginx/sites-available/taps /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx
```

## 4. Obtain Certificate

```bash
certbot --nginx -d taps.example.com --redirect --agree-tos -m you@example.com
# certbot will auto-inject ssl_certificate lines + 80→443 redirect
systemctl reload nginx
```

## 5. Tell Panel to Trust nginx

Without this step, **all client IPs appear as 127.0.0.1 to Panel**, breaking rate limiting / audit logs / API Key IP whitelist.

Go to **"System Settings"** → **"Trusted Proxy List"**:

- Default `127.0.0.1, ::1` already covers the local nginx scenario, **no changes needed**
- If nginx is on a different host: add the nginx server IP (e.g., `127.0.0.1, ::1, 10.0.0.5`)
- CIDR supported: `127.0.0.1, ::1, 10.0.0.0/24`

Save → **restart Panel**: `systemctl restart taps-panel`

> **Won't take effect without restart** — `gin.Engine.SetTrustedProxies()` is applied once at startup.

## 6. Verify

Open `https://taps.example.com/`:
- Browser shows lock icon
- Login works
- **"Audit Log"** / **"Login Log"** show your real public IP, **not** `127.0.0.1`

## 7. Firewall Cleanup

```bash
# Only allow 80 / 443 inbound; 24444 only on loopback (already done via panel host binding)
ufw allow 22,80,443/tcp
ufw deny  24444/tcp
ufw deny  24445/tcp   # if daemon is on same machine, Panel uses 127.0.0.1; no external access needed
ufw enable
```

If Daemon is on a **different machine**, the Daemon host must keep 24445 inbound open for the Panel host.

---

## FAQ

### Large File Upload Returns 413 Request Entity Too Large
nginx defaults to 1 MiB body limit. We've set `client_max_body_size 1100M;` to cover single chunks up to 1 GiB + metadata headroom. If you still get 413, check whether a smaller `client_max_body_size` in the global `http {}` block of `nginx.conf` is overriding the server-level setting.

### Terminal WebSocket Won't Connect
- nginx site must have the `map $http_upgrade` + `proxy_set_header Upgrade / Connection` trio
- `proxy_read_timeout` must be large enough (default 60s is insufficient; we set 3600s)
- If Panel public URL is not configured, Panel rejects WS upgrades (returns 503 `settings.public_url_required`)

### Trusted Proxy List Updated but Panel Still Shows 127.0.0.1
- Confirm you restarted panel: `systemctl restart taps-panel`
- Confirm nginx is actually sending `X-Forwarded-For`: `curl -sD - https://taps.example.com/api/healthz | grep -i x-`

### Should Daemon Also Go Through nginx?
**Not necessary**. Daemon already uses HTTPS (self-signed + fingerprint pinning). Adding nginx in between means handling wss forwarding + re-establishing the fingerprint chain. Direct Panel-to-Daemon connections are simplest.
