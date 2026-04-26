**English** | [中文](../zh/deployment/daemon-only.md) | [日本語](../ja/deployment/daemon-only.md)

# Daemon-Only Deployment

Add a new host to an existing Panel. The Daemon is the agent that actually runs Minecraft containers.

## Use Cases

- Adding a game server node to an existing Panel
- Daemon runs in a datacenter / home network / overseas VPS, centrally managed by a remote Panel

## Prerequisites

- Linux x86_64
- Docker installed (`docker version` works)
- Root privileges
- Inbound ports open:
  - **24445** (Daemon HTTPS / wss, for Panel outbound connections)
  - Instance ports (Minecraft 25565+ etc.)
- Outbound requirements (depends on configuration):
  - Docker image pulls (docker.io, ghcr.io, etc.)
  - Server jar download sources (fastmirror / papermc.io)

## 1. Prepare

```bash
mkdir -p /opt/taps /var/lib/taps/daemon
mv daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/daemon
```

## 2. systemd Unit

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

systemctl daemon-reload
systemctl enable --now taps-daemon
```

## 3. Retrieve Token + TLS Fingerprint

Daemon auto-generates these on first start:

```bash
echo "Token:"
cat /var/lib/taps/daemon/token

echo "TLS Fingerprint:"
journalctl -u taps-daemon -n 30 --no-pager | grep "tls fingerprint"
```

Note these two values — needed when adding to Panel.

## 4. Add Node in Panel

Go to the existing Panel's **"Node Management"** → **"Add"**:

| Field | Value |
|---|---|
| Name | Any (e.g., `bj-1`) |
| Address | `<daemon-public-ip>:24445` or `<internal-ip>:24445` |
| Display Host | Domain/IP players connect to |
| Port Range | 25565-25600 or custom |
| Token | Token from step 3 |

Click **"Fetch Fingerprint"** → **byte-for-byte verify** the displayed fingerprint matches step 3's output → **"Accept & Use"** → **"Save"**.

"Connected" in the node list = communication link established.

---

## Daemon Data Directory Details

| File / Directory | Auto-generated | Description |
|---|---|---|
| `token` | Yes | 32-byte random hex; Panel ↔ Daemon shared secret |
| `cert.pem` / `key.pem` | Yes | ECDSA P-256 self-signed 99 years; Panel pins its SHA-256 fingerprint |
| `files/` | Yes | Instance working directory root (subdirectories by UUID) |
| `backups/` | Yes | Backup zip storage |
| `volumes/` | Yes | Managed volume mount points (loopback img for disk quota) |
| `volumes/<name>.img` | On demand | Created when volume is created |
| `volumes/<name>/` | On demand | Volume mount point |
| `config.json.template` | Yes | Example config, **rewritten on every startup to stay in sync with version** |
| `config.json` | No | Optional admin-edited file that overrides env configuration |

## Key Operations

### Rotate Daemon Token

```bash
# On the Daemon host
rm /var/lib/taps/daemon/token
systemctl restart taps-daemon
cat /var/lib/taps/daemon/token   # new token

# In Panel UI: edit the node → update Token field → Save
```

### Rotate Daemon TLS Certificate

```bash
# On the Daemon host
rm /var/lib/taps/daemon/{cert,key}.pem
systemctl restart taps-daemon
journalctl -u taps-daemon -n 10 | grep "tls fingerprint"

# In Panel UI: edit the node → click "Fetch Fingerprint" → verify new fingerprint → "Accept & Use" → Save
```

### Adjust Daemon Configuration (Port / Rate Limiting / WS Frame Size)

Copy and edit `config.json`:

```bash
cd /var/lib/taps/daemon
cp config.json.template config.json
vim config.json   # remove fields you don't want to override, keep ones to change
systemctl restart taps-daemon
journalctl -u taps-daemon -n 5 | grep "applied overrides"
```

Supported fields (`config.json`):

```json
{
  "addr":                ":24445",
  "requireDocker":       true,
  "rateLimitThreshold":  10,
  "rateLimitBanMinutes": 10,
  "maxWsFrameBytes":     16777216
}
```

Priority: **config.json > env > defaults**.

## Security Notes

- **Daemon listening on public network = root equivalent**: anyone with the Token can create containers mounting host paths. Production recommendations:
  - Change `addr` to `127.0.0.1:24445` and use SSH tunnels or Tailscale to expose to Panel
  - Or use cloud firewall to restrict 24445 source IP to Panel host only
- **Token file mode 0600**, only root can read
- **TLS fingerprint pinning** prevents man-in-the-middle replacement of the Daemon — fingerprint must be verified when first adding a node
