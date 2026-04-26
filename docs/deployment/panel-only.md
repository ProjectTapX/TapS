**English** | [中文](../zh/deployment/panel-only.md) | [日本語](../ja/deployment/panel-only.md)

# Panel-Only Deployment

Panel is the control plane + Web UI and does not run any Minecraft instances itself. Deploy it standalone on a lightweight host (cloud VPS, home NAS, internal management server) and connect to multiple remote Daemons via wss.

## Use Cases

- Multi-host cluster: 1 Panel managing N Daemons (each running game servers)
- Panel on public internet / all Daemons on internal network (Panel makes outbound connections to Daemons)
- Panel + nginx/Caddy on a frontend machine, compute nodes dedicated to workloads

## Prerequisites

- Linux x86_64 (Panel does not depend on Docker)
- Open TCP **24444** (HTTP; only expose 443 after nginx reverse proxy)
- Panel host must be able to **actively** connect to each Daemon's port 24445 (wss outbound)

## 1. Prepare Directories

```bash
mkdir -p /opt/taps /var/lib/taps/panel
chmod 700 /var/lib/taps/panel
```

## 2. Place Binaries + Web Assets

```bash
mv panel-linux-amd64 /opt/taps/panel
chmod +x /opt/taps/panel
mkdir -p /opt/taps/web
tar -xzf web.tar.gz -C /opt/taps/web
```

## 3. systemd Unit

```bash
cat >/etc/systemd/system/taps-panel.service <<'EOF'
[Unit]
Description=TapS Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/taps/panel
WorkingDirectory=/opt/taps
Environment=TAPS_DATA_DIR=/var/lib/taps/panel
Environment=TAPS_WEB_DIR=/opt/taps/web
Environment=TAPS_ADDR=:24444
Environment=TAPS_ADMIN_USER=admin
Environment=TAPS_ADMIN_PASS=admin
# To enable HTTPS directly on panel (without nginx), uncomment and provide certs:
# Environment=TAPS_TLS_CERT=/etc/letsencrypt/live/example.com/fullchain.pem
# Environment=TAPS_TLS_KEY=/etc/letsencrypt/live/example.com/privkey.pem
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now taps-panel
```

Verify:

```bash
systemctl status taps-panel
ss -lnt | grep 24444
```

## 4. First Login + Password Change

`http://<panel-host>:24444/`, admin/admin → forced password change.

## 5. Add Remote Daemon Nodes

Ensure the Panel host can reach the Daemon host:

```bash
# Test connectivity from Panel host
nc -zv <daemon-host> 24445
# Optionally fetch daemon fingerprint via openssl to know the expected value
echo | openssl s_client -connect <daemon-host>:24445 2>/dev/null | openssl x509 -fingerprint -sha256 -noout
```

Go to Panel **"Node Management"** → **"Add"**:

| Field | Value |
|---|---|
| Name | `node-1` |
| Address | `<daemon-host>:24445` |
| Display Host | External domain/IP for players |
| Port Range | 25565-25600 |
| Token | From Daemon host: `cat /var/lib/taps/daemon/token` |

Click **"Fetch Fingerprint"** → verify (matches daemon startup log) → **"Accept & Use"** → **"Save"**.

Repeat for additional Daemons.

## Next Steps

- How to deploy Daemon independently: [Daemon-Only Deployment](daemon-only.md)
- Add HTTPS via nginx reverse proxy: [Nginx Reverse Proxy + HTTPS](nginx-https.md)
- Node groups (auto-select least loaded node when creating instances): configure in Panel "Node Groups" page

---

## Panel Data Directory Contents

| File | Auto-generated | Notes |
|---|---|---|
| `panel.db` | Yes | SQLite, all business data; GORM AutoMigrate auto-creates tables + adds columns |
| `jwt.secret` | Yes | 96-character hex; deleting it invalidates all JWTs immediately |

There is no `config.json` concept (Panel configuration is entirely via env + System Settings DB).
