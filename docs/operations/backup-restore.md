**English** | [中文](../zh/operations/backup-restore.md) | [日本語](../ja/operations/backup-restore.md)

# Backup & Recovery

## Three Levels of Backup

| Level | Contents | Recommended Frequency | Tools |
|---|---|---|---|
| **Application** | Individual instance working directory zip | Daily / before major changes | Panel UI "Backups" tab |
| **Control plane** | `panel.db` + `jwt.secret` (Panel) + `daemon/{token,cert.pem,key.pem,config.json}` (Daemon) | Daily | rsync / tar |
| **Host** | `/opt/taps`, `/var/lib/taps`, Docker volumes, Docker images | Weekly / disaster recovery | LVM / btrfs / ZFS snapshots, cloud disk snapshots |

## Panel + Daemon Critical File List

### Panel (`/var/lib/taps/panel/`)
```
panel.db           # All business data (users, nodes, instance permissions, API keys, settings, logs)
jwt.secret         # Used to sign JWTs; deleting it invalidates all tokens immediately
```

### Daemon (`/var/lib/taps/daemon/`)
```
token              # Panel ↔ Daemon shared secret
cert.pem           # Self-signed TLS certificate (Panel pins its fingerprint)
key.pem            # Corresponding private key
config.json        # Optional; admin-written env overrides
files/             # Generic file root (generic instance working directories, user uploads, etc.)
backups/           # Application-level backup zips
volumes/           # Managed volumes + docker instance /data directories (inst-<short> subdirectory per instance)
```

> `files/` and `volumes/` are **business data** and can be very large; backing them up = backing up all instance world files, etc.

## Simple rsync Script

```bash
#!/bin/bash
# /usr/local/bin/taps-backup.sh
set -e
DATE=$(date +%Y%m%d-%H%M%S)
DEST=/srv/backup/taps/$DATE
mkdir -p $DEST

# Panel
cp /var/lib/taps/panel/panel.db   $DEST/
cp /var/lib/taps/panel/jwt.secret $DEST/

# Daemon
cp /var/lib/taps/daemon/token     $DEST/
cp /var/lib/taps/daemon/cert.pem  $DEST/
cp /var/lib/taps/daemon/key.pem   $DEST/
[ -f /var/lib/taps/daemon/config.json ] && cp /var/lib/taps/daemon/config.json $DEST/

# Instance data + backup zips
rsync -a /var/lib/taps/daemon/files/   $DEST/files/
rsync -a /var/lib/taps/daemon/backups/ $DEST/backups/
# volumes are typically large; include as needed
# rsync -a /var/lib/taps/daemon/volumes/ $DEST/volumes/

# Clean up backups older than 30 days
find /srv/backup/taps -maxdepth 1 -type d -mtime +30 -exec rm -rf {} +
```

Add to cron:
```cron
0 4 * * *  /usr/local/bin/taps-backup.sh >> /var/log/taps-backup.log 2>&1
```

> Panel.db is SQLite — **hot copying may capture an inconsistent state**. For production, use `sqlite3 panel.db ".backup '/srv/backup/.../panel.db'"` which uses SQLite's own backup API to guarantee consistency.

```bash
sqlite3 /var/lib/taps/panel/panel.db ".backup '$DEST/panel.db'"
```

## Disaster Recovery: Rebuild Panel from Scratch

Assuming the Panel host is completely destroyed but you have a backup:

```bash
# 1. Set up a new panel host (follow panel-only.md steps 1-3 for directories + systemd)

# 2. Restore data
systemctl stop taps-panel
cp /backup/.../panel.db   /var/lib/taps/panel/panel.db
cp /backup/.../jwt.secret /var/lib/taps/panel/jwt.secret
chmod 600 /var/lib/taps/panel/jwt.secret
systemctl start taps-panel

# 3. Verify
journalctl -u taps-panel -n 20 --no-pager | tail -10
# Should see "panel listening" and (if nodes exist) "panel connected" for each daemon
```

As long as `panel.db` + `jwt.secret` are intact, all users, nodes, API Keys, and settings are restored, and **previously issued JWTs remain valid** (jwt.secret unchanged + tokens_invalid_before unchanged).

## Disaster Recovery: Rebuild Daemon from Scratch

Assuming the Daemon host is destroyed:

```bash
# 1. Set up daemon on new machine (follow daemon-only.md steps 1-2)

# 2. Restore
systemctl stop taps-daemon
cp /backup/.../token      /var/lib/taps/daemon/token
cp /backup/.../cert.pem   /var/lib/taps/daemon/cert.pem
cp /backup/.../key.pem    /var/lib/taps/daemon/key.pem
chmod 600 /var/lib/taps/daemon/{token,cert.pem,key.pem}
rsync -a /backup/.../files/   /var/lib/taps/daemon/files/
rsync -a /backup/.../backups/ /var/lib/taps/daemon/backups/

systemctl start taps-daemon
```

As long as token + cert/key match, **Panel doesn't need to re-TOFU the fingerprint** — the fingerprint still matches.

If token or cert are lost, update the new token in Panel's node editor; for new cert, re-probe the fingerprint in the node editor.

## Instance-Level Restore

```bash
# Via Panel UI: Backups page → select target zip → click "Restore"
# Or via API:
curl -X POST -H "Authorization: Bearer $JWT" \
     -H "Content-Type: application/json" \
     -d '{"name":"<backup-zip>"}' \
     https://panel/api/daemons/$ID/instances/$UUID/backups/restore
```

This **overwrites existing files with matching names**; no incremental sync. Recommend stopping the instance before restoring.
