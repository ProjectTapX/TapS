**English** | [中文](../zh/usage/files.md) | [日本語](../ja/usage/files.md)

# Files & Backups

## File Browser

Instance detail page → **"Files"** tab.

Regular users can only see directories of instances they have permission for (`/data/inst-<short>` subtree, where short is the first 12 hex characters of the instance UUID without hyphens). Admins see the entire `/data` and `/files` trees.

### Operations

| Operation | Notes |
|---|---|
| Upload | Chunked protocol (init → chunk × N), auto-splits at 1 MiB per chunk; each file runs init first to check quota |
| Download | Streaming, direct `Content-Disposition: attachment` |
| Edit | Single file ≤ 4 MiB can be edited online (larger files are read-only) |
| New folder / Rename / Copy / Move / Delete | Standard operations |
| Compress to zip | Zips multiple selected items into one archive |
| Extract zip | Automatic zip-slip protection |

### Upload Quota

Each upload **calls init first**:
- Daemon uses `statfs` to calculate remaining space on the volume containing the instance data directory
- If declared total bytes > remaining space → **HTTP 507 quota_exceeded**
- On success, returns an `uploadId`; subsequent chunks must include `?uploadId=`

If the client crashes mid-upload without sending `final=true`: daemon auto-cleans `.partial` files after 1 hour.

### Known Limitations

- Max single chunk size: 1 GiB (daemon-side `MaxBytesReader`)
- Total file size limited by volume remaining space
- No streaming compress/decompress (compression assembles zip in daemon memory)

## Backups

**"Backups"** tab:

| Operation | Behavior |
|---|---|
| Create | Zips entire instance working directory → stored at `backups/<uuid>/<timestamp>-<note>.zip` |
| List | Shows zip name, size, time, notes |
| Download | Exports zip locally |
| Restore | Extracts to working directory (**overwrites existing files with same names**) |
| Delete | Deletes a single zip |

### Backup Name Validation

`name` field enforced regex `^[A-Za-z0-9._-]{1,128}$` (prevents path traversal / shell characters).
`note` field max 512 characters, no newlines allowed.

### Backup Storage Location

- If the instance has a **managed volume** (disk quota was set at creation): backups are stored in the `.taps-backups/` subdirectory within that volume
- Otherwise stored in daemon data directory `/var/lib/taps/daemon/backups/<uuid>/`

The former counts toward the volume quota (prevents backups from filling disk); the latter does not.

### Backup Strategy Recommendations

- Set up a `command`-type scheduled task → `say` to notify players in advance
- Add another `restart` task via cron, staggered in time
- For critical servers, use host-level snapshots (LVM / btrfs / ZFS) as secondary protection — TapS backups are **application-level** and don't protect against daemon host failures
