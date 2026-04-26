**English** | [中文](../zh/usage/instances.md) | [日本語](../ja/usage/instances.md)

# Instance Management

## State Machine

```
┌────────┐  start    ┌─────────┐  ready    ┌─────────┐
│stopped │ ────────> │starting │ ────────> │ running │
└────┬───┘           └─────────┘           └────┬────┘
     │                                          │
     │   delete                            stop │
     │                                          v
     │                                     ┌─────────┐
     │                                     │stopping │
     │                                     └────┬────┘
     │                                          │
     │                                          v
     │            ┌─────────┐  exit ≠ 0    ┌────────┐
     │            │ crashed │ <─────────── │stopped │
     │            └─────────┘              └────────┘
     v
   (gone)
```

With auto-hibernation:

```
running ──idle──> hibernating ──client connect──> starting ──> running
```

## Creating Instances

**"Instance Management"** → **"New"** → select type:
- **Template** (Vanilla / Paper / Purpur / Fabric / Forge / NeoForge): quick wizard + auto-downloads jar
- **Docker**: freely specify image / mounts / ports

Field descriptions:

| Field | Purpose |
|---|---|
| Name | Display name, duplicates allowed |
| Node | Which Daemon to deploy to (or select a group for auto-selection) |
| Working Directory | Empty = `<DataDir>/files` root; relative path appends to that root; absolute path used directly. For Docker instances the `/data` volume is auto-created by "Disk Quota" at `<DataDir>/volumes/inst-<short>/`, unrelated to this field |
| Command | For type=docker: image name (`itzg/minecraft-server`). The selector shows admin-set display names first (editable on the Images page); for type=generic: the executable |
| Arguments | Command args (array) |
| Stop Command | Command written to stdin, e.g., `stop` (graceful Minecraft shutdown) |
| Auto Start | Whether Daemon auto-starts this instance on boot |
| Auto Restart | Whether to auto-restart after crash |
| Restart Delay | Seconds to wait before auto-restart (default 5) |

## Start / Stop

- **Start** / **Stop**: invokes stopCmd (default writes `stop` to stdin, giving MC graceful shutdown time)
- **Force Kill**: direct `docker kill` / `SIGKILL`
- **Restart**: stop → wait for exit → start (you can `say` in the terminal to notify players)

## Terminal

Instance detail page → **"Terminal"**: full xterm.js terminal, **real-time** stdout, **keyboard** stdin.

Permission requirements:
- **Open** terminal: `PermView`
- **Send input**: `PermTerminal` or `PermControl`

Read-only users can see scrollback history; keystrokes are silently discarded.

## File Management

Instance detail page → **"Files"** tab: browse / upload / download / edit / compress / extract / rename / move / copy / delete.

See [Files & Backups](files.md) for details.

## Backups

Instance detail page → **"Backups"** tab:

- **Create backup**: zips the entire working directory to `backups/<uuid>/<timestamp>.zip`, with optional notes
- **Download**: export zip
- **Restore**: extracts back to working directory (overwrites existing files)
- **Delete**: removes a single backup zip

Backups count toward the **instance's disk quota** (if using managed volumes).

## Monitoring

Instance detail page → **"Monitoring"** tab: CPU / memory / disk / network real-time graphs (one sample every 5 seconds).

## Player List

Minecraft instances use automatic SLP probing; the detail page shows online player count + names.

## Deleting Instances

Instance detail page → **"Delete"**:

- Container is stopped and `docker rm`'d
- Working directory / managed volume is **retained** (to prevent accidental deletion); to fully clean disk space, manually delete via "File Management"

## Auto-Hibernation (Minecraft Java Only)

When enabled, the container auto-stops after N minutes idle, and Panel runs a fake SLP listener on the original port:
- Players see a custom MOTD + icon in their client server list
- Player connects → triggers wake → real container starts → countdown N seconds before player can fully join

Configuration: **"System Settings"** → **"Minecraft Java Server Auto-Hibernation"** for global defaults; per-instance overrides on the edit page (`hibernationEnabled`, `hibernationIdleMinutes`).
