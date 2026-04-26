**English** | [中文](../zh/deployment/single-host.md) | [日本語](../ja/deployment/single-host.md)

# Single-Host Deployment (Panel + Daemon on Same Machine)

Install Panel and Daemon on the same Linux host. The most common "personal / small team" setup: one VPS running the panel + a few Minecraft instances.

## Prerequisites

- Linux x86_64 (Debian 12/13, Ubuntu 22.04+ recommended; any systemd distro works)
- Docker installed (Daemon defaults to `requireDocker=true`, all instances run in containers)
- Root privileges (Daemon needs to manage Docker, volumes, networking)
- Open TCP ports **24444** (Panel HTTP), **24445** (Daemon HTTPS); for nginx reverse proxy see [Nginx Reverse Proxy](nginx-https.md)
- Minecraft instance ports (default 25565+)

## 1. Prepare Directories

```bash
mkdir -p /opt/taps /var/lib/taps/panel /var/lib/taps/daemon
chmod 700 /var/lib/taps/panel /var/lib/taps/daemon
```

## 2. Place Binaries + Web Assets

Place the cross-compiled artifacts (build instructions at [development/build.md](../development/build.md)):

```bash
mv panel-linux-amd64  /opt/taps/panel
mv daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/panel /opt/taps/daemon

# Extract frontend static assets to /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf web.tar.gz -C /opt/taps/web
```

Final structure:

```
/opt/taps/
├── panel
├── daemon
└── web/
    ├── index.html
    └── assets/

/var/lib/taps/
├── panel/    # panel data: panel.db, jwt.secret
└── daemon/   # daemon data: token, cert.pem, key.pem, files/, backups/, volumes/
```

> **All files in data directories are auto-generated on first start** (including SQLite DB, JWT secret, Daemon Token, TLS self-signed certificate, `config.json.template` example). No manual initialization required.

## 3. systemd Units

```bash
cat >/etc/systemd/system/taps-daemon.service <<'EOF'
[Unit]
Description=TapS Daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/taps/daemon
WorkingDirectory=/opt/taps
Environment=TAPS_DAEMON_DATA=/var/lib/taps/daemon
Environment=TAPS_DAEMON_ADDR=:24445
Environment=TAPS_REQUIRE_DOCKER=true
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

cat >/etc/systemd/system/taps-panel.service <<'EOF'
[Unit]
Description=TapS Panel
After=network-online.target taps-daemon.service
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
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
```

## 4. Start

**Start Daemon first** (Panel attempts to connect to registered nodes on startup; no nodes on first run is fine, but this order is recommended for production):

```bash
systemctl enable --now taps-daemon
sleep 2
systemctl enable --now taps-panel
```

Verify status:

```bash
systemctl status taps-daemon taps-panel
ss -lnt | grep -E '24444|24445'
```

You should see `*:24444` and `*:24445` both listening.

## 5. Retrieve Daemon Information

```bash
# Daemon Token (needed when adding the node)
cat /var/lib/taps/daemon/token

# Daemon TLS Fingerprint (needed when adding the node, for TOFU verification)
journalctl -u taps-daemon -n 30 --no-pager | grep "tls fingerprint"
```

Or let Panel auto-probe later (see next step).

## 6. First Panel Login

Open `http://<server-IP>:24444/` in a browser:

1. Log in with `admin` / `admin` (set by `TAPS_ADMIN_USER`/`TAPS_ADMIN_PASS` on first start; won't overwrite if DB already exists)
2. System forces a **password change**
3. After successful login, enter the dashboard

## 7. Add Daemon Node

Go to **"Node Management"** → **"Add"**:

| Field | Value |
|---|---|
| Name | `local` (any name) |
| Address | `127.0.0.1:24445` |
| Display Host | External domain/IP for players to connect (e.g., `play.example.com`); leave empty to fall back to the address host |
| Port Range | Range for auto-assigned host ports (default 25565–25600) |
| Token | Content of `cat /var/lib/taps/daemon/token` |

Then click **"Fetch Fingerprint"**: Panel will TLS-probe the daemon to get the fingerprint. **Verify the fingerprint** matches the `journalctl ... grep "tls fingerprint"` output, click **"Accept & Use"** → **"Save"**.

"Connected" in the node list means success.

## 8. Create Your First Instance

Go to **"Instance Management"** → **"New"**, select a template (Vanilla / Paper / Purpur / Custom Docker), fill in name, version, memory, port, disk. See [Quick Start](../usage/quickstart.md) for details.

---

## Common Adjustments

### Change Panel Port
Go to **"System Settings"** → **"Panel Listen Port"**, save then restart:
```bash
systemctl restart taps-panel
```

### Change Daemon Configuration (Port / Rate Limiting / WS Frame Size)
Two approaches:
- **Quick / simple**: edit `Environment=` lines in `/etc/systemd/system/taps-daemon.service` → `systemctl daemon-reload && systemctl restart taps-daemon`
- **Persistent / recommended**: copy `/var/lib/taps/daemon/config.json.template` to `config.json` and edit:
  ```bash
  cd /var/lib/taps/daemon
  cp config.json.template config.json
  vim config.json   # remove fields you don't want to override
  systemctl restart taps-daemon
  journalctl -u taps-daemon -n 5 | grep "applied overrides"
  ```
  Priority: `config.json` > env > defaults.

### Default Credentials
Determined by environment variables on first start. Changing `TAPS_ADMIN_USER`/`TAPS_ADMIN_PASS` **will not** modify existing users — these env vars are only used during initial DB creation.

### Enable HTTPS
Recommended via nginx reverse proxy: [Nginx Reverse Proxy + HTTPS](nginx-https.md).

---

## Uninstall

```bash
systemctl disable --now taps-panel taps-daemon
rm -f /etc/systemd/system/taps-{panel,daemon}.service
rm -rf /opt/taps /var/lib/taps
systemctl daemon-reload
# Docker containers prefixed with taps-: docker rm -f $(docker ps -aq --filter name=taps-)
```
