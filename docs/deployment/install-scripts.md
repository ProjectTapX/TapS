**English** | [中文](../zh/deployment/install-scripts.md) | [日本語](../ja/deployment/install-scripts.md)

# One-Click Installation Scripts

Three scripts to quickly install TapS from the latest GitHub release. They auto-detect your CPU architecture (x86_64 / ARM64), download binaries, configure systemd services, and start everything.

## Quick Start

```bash
# Single-host (Panel + Daemon on the same machine) — most common
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash

# Panel only (control plane, no game instances)
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-panel.sh | bash

# Daemon only (add a game server node to an existing Panel)
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-daemon.sh | bash
```

## Prerequisites

| Requirement | Panel | Daemon | Single-Host |
|-------------|-------|--------|-------------|
| Linux x86_64 or ARM64 | Yes | Yes | Yes |
| Root privileges | Yes | Yes | Yes |
| curl | Yes | Yes | Yes |
| Docker | No | Yes | Yes |
| systemd | Yes | Yes | Yes |

## What Each Script Does

### `install.sh` (Single-Host)

1. Detects CPU architecture
2. Fetches the latest release version from GitHub
3. Prompts for configuration (ports, data directories, admin credentials)
4. Downloads `panel-linux-{arch}`, `daemon-linux-{arch}`, and `web.tar.gz`
5. Creates data directories with `chmod 700`
6. Writes systemd units for both `taps-daemon` and `taps-panel`
7. Starts Daemon first, waits, then starts Panel
8. Prints token, TLS fingerprint, and access URL

### `install-panel.sh` (Panel Only)

1. Detects CPU architecture
2. Fetches the latest release version
3. Prompts for: port, data directory, web directory, admin username/password
4. Downloads `panel-linux-{arch}` and `web.tar.gz`
5. Creates systemd unit `taps-panel`
6. Starts Panel and prints access URL

### `install-daemon.sh` (Daemon Only)

1. Detects CPU architecture
2. Fetches the latest release version
3. Prompts for: listen address, data directory
4. Downloads `daemon-linux-{arch}`
5. Creates systemd unit `taps-daemon`
6. Starts Daemon and prints token + TLS fingerprint

## Configuration Options

All options have sensible defaults — just press Enter to accept them.

| Option | Default | Script |
|--------|---------|--------|
| Panel listen port | `24444` | Panel, Single-Host |
| Panel data directory | `/var/lib/taps/panel` | Panel, Single-Host |
| Web static directory | `/opt/taps/web` | Panel, Single-Host |
| Admin username | `admin` | Panel, Single-Host |
| Admin password | `admin` | Panel, Single-Host |
| Daemon listen address | `:24445` | Daemon, Single-Host |
| Daemon data directory | `/var/lib/taps/daemon` | Daemon, Single-Host |

## Post-Install Verification

```bash
# Check service status
systemctl status taps-panel taps-daemon

# Check listening ports
ss -lnt | grep -E '24444|24445'

# View Daemon token (needed to add node in Panel)
cat /var/lib/taps/daemon/token

# View Daemon TLS fingerprint
journalctl -u taps-daemon | grep "tls fingerprint"
```

## Proxy Support

The scripts use `curl` for downloads. If you're behind a proxy, set the environment variable before running:

```bash
export HTTPS_PROXY=http://proxy:port
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash
```

## Uninstall

```bash
systemctl disable --now taps-panel taps-daemon
rm -f /etc/systemd/system/taps-{panel,daemon}.service
systemctl daemon-reload
rm -rf /opt/taps /var/lib/taps
```
