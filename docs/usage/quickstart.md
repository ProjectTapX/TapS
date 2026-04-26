**English** | [中文](../zh/usage/quickstart.md) | [日本語](../ja/usage/quickstart.md)

# Quick Start

Get a Minecraft server running from scratch in 10 minutes.

## Prerequisites

- Panel is deployed and running ([Single-Host Deployment](../deployment/single-host.md))
- At least one Daemon node is added and shows "Connected"
- The node host has Docker Engine installed (Daemon **only allows Docker-type instances** by default)

## 1. Log In

`http://<panel>:24444/` → `admin` / `admin` → forced password change.

## 2. Initial Configuration (System Settings)

After logging in, go to **System Settings** and complete the following:

### 2.1 Panel Public URL (Required)

Second card **"Panel Public URL"** → enter the Panel's external URL (e.g., `https://taps.example.com` or `http://your-ip:24444`) → Save.

> Without this, SSO, terminal WebSocket origin checks, and CORS fallback will all fail.

### 2.2 Login CAPTCHA (Strongly Recommended)

**"Login CAPTCHA"** card → choose **Cloudflare Turnstile** or **reCAPTCHA Enterprise** → enter Site Key + Secret → click "Test connectivity" → save after it passes.

> A publicly exposed panel without CAPTCHA relies solely on rate limiting (default 5 attempts/min) to block brute force. Enabling CAPTCHA requires human verification on every login, significantly raising the attack barrier.

## 3. Pull Docker Images

Left sidebar **"Images"** page → select node → click **"Pull Image"**:

- Pick from the common image list (e.g., `itzg/minecraft-server`, `eclipse-temurin:21-jre`)
- Or manually enter `repo:tag`

Wait for the pull to complete (progress bar shows real-time per-layer download status).

> Admins can set a "display name" for images (click the edit icon next to the image name). Once set, only the friendly name appears in the runtime selector when creating instances.

## 4. Create an Instance

### Option A: Template Deploy (Recommended for Beginners)

**"Instance Management"** → **"New"** → select template:

- **Paper** (recommended — performance + Bukkit plugin support)
- **Vanilla** (official)
- **Purpur**, **Fabric**, **Forge**, **NeoForge**

Fill in:

| Field | Recommended | Description |
|---|---|---|
| Name | `survival` | Human-readable identifier |
| Node | Select an added node | Or select a node group (auto-picks the least loaded) |
| Version | `1.21.4` | Minecraft version |
| Memory Limit | `2G` | Docker `--memory` |
| Disk Quota | `5G` | Auto-creates a 5 GiB loopback managed volume |
| Host Port | `25565` | Leave empty to auto-assign from node port range |

Click **Create & Deploy**.

### Option B: Custom Docker Instance

**"Instance Management"** → **"New"** → type **Docker**:

| Field | Example | Description |
|---|---|---|
| Runtime | Select from pulled images | Shows friendly name if display name is set |
| Start Command | `java -Xmx2G -jar server.jar nogui` | Command executed inside the container |
| Host Port | `25565` | `host:container` format |
| Environment Variables | `EULA=TRUE` | One `KEY=VALUE` per line |
| Disk Quota | `5G` | Optional; if empty, shares host disk |

> Daemon defaults to `TAPS_REQUIRE_DOCKER=true`, only allowing Docker-type instances. To run non-Docker processes, change the daemon environment variable. (Non-Docker instances are NOT recommended for any production environment!)

## 5. Deploy Progress (Template Mode)

A progress panel appears:
- **Download** server jar (Paper defaults to FastMirror, fast in China)
- **Extract**
- **First launch** auto-accepts EULA and generates the world

Once complete, the instance status becomes "Stopped" — click **Start**.

## 6. View Terminal

Instance detail page → **"Terminal"** tab: real-time console + command input.

## 7. Player Connection

Add server in Minecraft client: `<node's displayHost>:<host port>`, e.g., `play.example.com:25565`.

If **displayHost is not set** in the node editor, Panel falls back to the host portion of the "Address" field. **Always set an externally reachable displayHost**, otherwise players will see an internal IP.

---

## Advanced: Node Groups (Auto-Select Least Loaded Node)

**"Node Groups"** → create group → add multiple nodes → when creating an instance, select "By Group" →
Panel scans CPU/memory/disk load on each node in the group and picks the least loaded one.

## Advanced: Scheduled Tasks

Instance detail page → **"Scheduled Tasks"** → create:

| Field | Value |
|---|---|
| Cron | `0 4 * * *` (daily at 4:00 AM) |
| Action | `command` / `start` / `stop` / `restart` |
| Content | When action = command, e.g., `say Server restarting soon` |

## Advanced: Auto-Hibernation (Save Resources)

Enable globally in **"System Settings"** → **"Minecraft Java Server Auto-Hibernation"**; can also be overridden per-instance on the edit page.

When enabled: no players online for N minutes → auto-stops the real container → Panel runs a **fake SLP listener** on the original port (players see a custom MOTD + icon in their server list) → once a player connects, auto-wakes and starts the real container.
