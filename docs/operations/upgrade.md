**English** | [中文](../zh/operations/upgrade.md) | [日本語](../ja/operations/upgrade.md)

# Upgrade Procedure

## Rolling Strategy

- **Daemon**: upgrading Daemon first doesn't affect Panel; running instances on that node are unaffected by Daemon restart (Docker containers are independent processes). Daemon supports graceful shutdown (SIGTERM → 30s wait → hib.Shutdown → volumes.UnmountAll); systemd restart follows this path correctly
- **Panel**: upgrading Panel disconnects all Panel↔Daemon WebSocket connections; Panel is unavailable for a few seconds until reconnection; Daemon-side instances continue running. Panel also supports graceful shutdown
- **Recommended order**: Daemon first → wait a few seconds → then Panel; this way Daemon is already ready when Panel starts

## Pre-Upgrade

```bash
# 1. Back up SQLite + critical config
TS=$(date +%Y%m%d-%H%M%S)
mkdir -p /opt/taps/backup
cp /var/lib/taps/panel/panel.db /opt/taps/backup/panel.db.$TS
cp /var/lib/taps/panel/jwt.secret /opt/taps/backup/jwt.secret.$TS
cp /var/lib/taps/daemon/token /opt/taps/backup/daemon-token.$TS
cp /var/lib/taps/daemon/cert.pem /opt/taps/backup/daemon-cert.$TS
cp /var/lib/taps/daemon/key.pem /opt/taps/backup/daemon-key.$TS
cp /opt/taps/panel  /opt/taps/backup/panel.$TS
cp /opt/taps/daemon /opt/taps/backup/daemon.$TS

# 2. Check running instances (stopped ones don't matter; running ones do)
systemctl status taps-panel taps-daemon
ss -lnt | grep -E '24444|24445'
```

## Upgrade Daemon

```bash
# Assuming new binary is at /tmp/daemon-linux-amd64
systemctl stop taps-daemon
mv /tmp/daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/daemon
systemctl start taps-daemon
sleep 3

# Check if running normally
systemctl is-active taps-daemon
journalctl -u taps-daemon -n 20 --no-pager | tail -10
# Check if config.json was applied
journalctl -u taps-daemon -n 20 | grep "applied overrides"
# Check if token / fingerprint changed (shouldn't; if so, token / cert files were lost)
journalctl -u taps-daemon -n 20 | grep -E 'token:|fingerprint:'
```

If `cert.pem` / `key.pem` are missing (e.g., accidentally deleted during cleanup), daemon will regenerate a new certificate → **Panel must re-accept the new fingerprint**.

## Upgrade Panel

```bash
# Assuming new binary + web at /tmp/
systemctl stop taps-panel
mv /tmp/panel-linux-amd64 /opt/taps/panel
chmod +x /opt/taps/panel
rm -rf /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf /tmp/web.tar.gz -C /opt/taps/web
rm /tmp/web.tar.gz
systemctl start taps-panel
sleep 3

# Verify
systemctl is-active taps-panel
journalctl -u taps-panel -n 30 --no-pager | tail -15
# Should see "panel listening on :24444"
# Should see "panel connected: ..." indicating successful reconnection to each daemon
```

## DB Migration Auto-Applied

On startup, Panel's GORM `AutoMigrate` automatically:
- Adds new tables (if the new version introduces any)
- Adds new columns (e.g., Batch #4's `tokens_invalid_before`, Batch #7's `expires_at`/`revoked_at`)
- Adds new indexes

**Will not**: drop columns, change column types, or rollback.

If upgrade logs show `record not found` warnings (for specific `settings` table keys), those are new settings introduced by the new version that haven't been used yet → normal, defaults will be used.

## Rollback

```bash
TS=timestamp_of_latest_backup

systemctl stop taps-panel taps-daemon

# Restore binaries
cp /opt/taps/backup/panel.$TS  /opt/taps/panel
cp /opt/taps/backup/daemon.$TS /opt/taps/daemon

# Restore DB (if new version added columns, old panel will ignore extra columns — OK)
cp /opt/taps/backup/panel.db.$TS /var/lib/taps/panel/panel.db

systemctl start taps-daemon
sleep 2
systemctl start taps-panel
```

> If the new version wrote any **new settings** or **API keys with expiresAt/revokedAt**, that data remains in the DB after rollback but the old panel won't use it — doesn't break functionality, new features just "disappear".

## Upgrade Frontend Only (No Panel Restart)

Replace web static assets without restarting panel:

```bash
rm -rf /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf /tmp/web.tar.gz -C /opt/taps/web
# Panel's built-in http.FileServer doesn't cache directory listings; next browser request takes effect
```

Have users **hard-refresh** (Ctrl+F5) to clear Vite's hashed filename cache.

## Upgrade systemd Units

If the new version requires more env vars / changed ExecStart / added `LimitNOFILE`, etc.:

```bash
vim /etc/systemd/system/taps-panel.service
systemctl daemon-reload
systemctl restart taps-panel
```
