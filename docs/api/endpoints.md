**English** | [中文](../zh/api/endpoints.md) | [日本語](../ja/api/endpoints.md)

# API Endpoint Reference

All endpoints are prefixed with `/api/`. Unless otherwise noted, requests and responses use `application/json`.

**Auth icons**:
- 🌐 = Fully public
- 🔓 = Login required (Bearer JWT or API Key)
- 🔑 = `?token=<jwt>` query parameter
- 👑 = Requires `admin` role
- `[scope]` = API Key must include this scope
- `+ perm` = Also requires the corresponding instance permission

**Conventions**: In the examples below, `$T` represents a valid JWT token, and `$H` represents `Authorization: Bearer $T`.

---

## 1. Public Endpoints (🌐 No Auth Required)

### POST /api/auth/login

Username/password login.

```bash
curl -X POST http://panel:24444/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"mypassword","captchaToken":""}'
```

Success 200:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": 1, "username": "admin", "role": "admin",
    "email": "admin@example.com",
    "mustChangePassword": false, "hasPassword": true,
    "createdAt": "2026-04-20T10:00:00Z"
  }
}
```

Failure 401: `{"error":"auth.invalid_credentials","message":"invalid credentials"}`
Failure 400: `{"error":"common.invalid_body","message":"invalid body"}`
Failure 429: `{"error":"auth.rate_limited","message":"...","params":{"retryAfter":298}}`

### GET /api/captcha/config

```bash
curl http://panel:24444/api/captcha/config
```
```json
{"provider":"none","siteKey":""}
```

### GET /api/brand

```bash
curl http://panel:24444/api/brand
```
```json
{"siteName":"TapS","hasFavicon":true,"faviconMime":"image/png"}
```

### GET /api/brand/favicon

```bash
curl -o favicon.png http://panel:24444/api/brand/favicon
# Returns PNG/ICO binary; 404 = not set
```

### GET /api/settings/hibernation/icon

```bash
curl -o icon.png http://panel:24444/api/settings/hibernation/icon
# 64x64 PNG; with Cache-Control: public, max-age=300; 404 = not set
```

### GET /api/oauth/providers

```bash
curl http://panel:24444/api/oauth/providers
```
```json
[{"name":"logto","displayName":"Logto"}]
```
Returns an empty array `[]` in password mode.

### GET /api/oauth/start/:name

```bash
curl -v http://panel:24444/api/oauth/start/logto
# 302 redirect to IdP authorization URL
```

### GET /api/oauth/callback/:name

IdP callback, 302 redirect to `<publicUrl>/#oauth-token=<jwt>` or `/#oauth-error=<code>`.

### GET /api/auth/login-method

```bash
curl http://panel:24444/api/auth/login-method
```
```json
{"method":"password-only"}
```

### GET /healthz

```bash
curl http://panel:24444/healthz
```
```json
{"ok":true}
```

---

## 2. Current User (🔓 Login Required)

### GET /api/auth/me

```bash
curl http://panel:24444/api/auth/me -H "$H"
```
```json
{"id":1,"username":"admin","role":"admin","email":"admin@example.com","mustChangePassword":false,"hasPassword":true,"createdAt":"2026-04-20T10:00:00Z"}
```

### POST /api/auth/me/password

```bash
curl -X POST http://panel:24444/api/auth/me/password -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"oldPassword":"current","newPassword":"newpass123"}'
```
```json
{"ok":true}
```

Failure 401: `{"error":"user.wrong_current_password",...}`

---

## 3. User Management (👑 Admin Only)

### GET /api/users

```bash
curl http://panel:24444/api/users -H "$H"
```
```json
[
  {"id":1,"username":"admin","role":"admin","email":"admin@example.com","mustChangePassword":false,"hasPassword":true,"createdAt":"..."},
  {"id":2,"username":"player1","role":"user","email":"","mustChangePassword":false,"hasPassword":true,"createdAt":"..."}
]
```

### POST /api/users

```bash
curl -X POST http://panel:24444/api/users -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"username":"newuser","password":"pass123","role":"user","email":"new@example.com"}'
```
```json
{"id":3,"username":"newuser","role":"user","email":"new@example.com","mustChangePassword":false,"hasPassword":true,"createdAt":"..."}
```

Failure 409: `{"error":"user.username_taken",...}` or `{"error":"user.email_taken",...}`

### PUT /api/users/:id

```bash
curl -X PUT http://panel:24444/api/users/3 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","email":"updated@example.com"}'
```
```json
{"id":3,"username":"newuser","role":"admin","email":"updated@example.com",...}
```

Failure 400: `{"error":"user.cannot_demote_last_admin",...}`

### DELETE /api/users/:id

```bash
curl -X DELETE http://panel:24444/api/users/3 -H "$H"
```
```json
{"ok":true}
```

---

## 4. Node Management (👑 Admin Only)

### GET /api/daemons

```bash
curl http://panel:24444/api/daemons -H "$H"
```
```json
[{"id":1,"name":"node-1","address":"10.0.0.5:24445","connected":true,"displayHost":"mc.example.com","portMin":25565,"portMax":25600,"createdAt":"..."}]
```

### POST /api/daemons

```bash
curl -X POST http://panel:24444/api/daemons -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"node-2","address":"10.0.0.6:24445","token":"<daemon-token>","certFingerprint":"aa:bb:cc:..."}'
```
```json
{"id":2,"name":"node-2","address":"10.0.0.6:24445","connected":false,...}
```

### PUT /api/daemons/:id

```bash
curl -X PUT http://panel:24444/api/daemons/1 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"node-1-renamed","displayHost":"play.example.com","portMin":25565,"portMax":25700}'
```

### DELETE /api/daemons/:id

```bash
curl -X DELETE http://panel:24444/api/daemons/1 -H "$H"
```
```json
{"ok":true}
```
Cascade deletes InstancePermission + Task + NodeGroupMember.

### POST /api/daemons/probe-fingerprint

```bash
curl -X POST http://panel:24444/api/daemons/probe-fingerprint -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"address":"10.0.0.5:24445"}'
```
```json
{"fingerprint":"aa:bb:cc:dd:...","certPem":"-----BEGIN CERTIFICATE-----\n..."}
```

### GET /api/daemons/:id/public

🔓 Any logged-in user can call this (shows node address to user role).

```bash
curl http://panel:24444/api/daemons/1/public -H "$H"
```
```json
{"id":1,"name":"node-1","displayHost":"mc.example.com"}
```

---

## 5. Instance Management (🔓 scope + perm)

### GET /api/instances

Aggregates instance lists from all nodes (filtered by user permissions).

```bash
curl http://panel:24444/api/instances -H "$H"
```
```json
[{"daemonId":1,"info":{"config":{"uuid":"550e8400-...","name":"survival","type":"docker","command":"itzg/minecraft-server",...},"status":"running","pid":12345}}]
```

### GET /api/daemons/:id/instances

Instance list for a single node.

```bash
curl http://panel:24444/api/daemons/1/instances -H "$H"
```

### POST /api/daemons/:id/instances — 👑 admin

```bash
curl -X POST http://panel:24444/api/daemons/1/instances -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"survival","type":"docker","command":"itzg/minecraft-server:latest","dockerPorts":["25565:25565"],"dockerEnv":["EULA=TRUE","MEMORY=2G"]}'
```
```json
{"config":{"uuid":"550e8400-...","name":"survival",...},"status":"stopped","pid":0}
```

### PUT /api/daemons/:id/instances/:uuid

```bash
curl -X PUT http://panel:24444/api/daemons/1/instances/550e8400-... -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"survival-v2","stopCmd":"stop"}'
```

### DELETE /api/daemons/:id/instances/:uuid — 👑 admin

```bash
curl -X DELETE http://panel:24444/api/daemons/1/instances/550e8400-... -H "$H"
```

### POST /api/daemons/:id/instances/:uuid/start

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../start -H "$H"
```
```json
{"config":{...},"status":"running","pid":54321}
```

### POST /api/daemons/:id/instances/:uuid/stop

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../stop -H "$H"
```

### POST /api/daemons/:id/instances/:uuid/kill

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../kill -H "$H"
```

### POST /api/daemons/:id/instances/:uuid/input

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../input -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"data":"say Hello from API\n"}'
```

### GET /api/daemons/:id/instances/:uuid/dockerstats

```bash
curl http://panel:24444/api/daemons/1/instances/550e8400-.../dockerstats -H "$H"
```
```json
{"name":"taps-550e8400-...","running":true,"cpuPercent":2.5,"memBytes":268435456,"memLimit":2147483648,"memPercent":12.5,"netRxBytes":1024,"netTxBytes":512,"diskUsedBytes":1073741824,"diskTotalBytes":5368709120}
```

### GET /api/daemons/:id/instances-dockerstats

Docker stats for all instances (one-shot).

```bash
curl http://panel:24444/api/daemons/1/instances-dockerstats -H "$H"
```
```json
{"items":[{"name":"taps-550e8400-...","running":true,"cpuPercent":2.5,...}]}
```

### GET /api/daemons/:id/instances-players

Player overview for all instances.

```bash
curl http://panel:24444/api/daemons/1/instances-players -H "$H"
```
```json
{"items":[{"uuid":"550e8400-...","online":3,"max":20}]}
```

---

## 6. File System (🔓 scope `files` + path permissions)

### GET /api/daemons/:id/fs/list

```bash
curl "http://panel:24444/api/daemons/1/fs/list?path=/" -H "$H"
```
```json
{"path":"/","entries":[{"name":"files","isDir":true,"mode":"drwxr-xr-x"},{"name":"data","isDir":true,"mode":"drwxr-xr-x"}]}
```

### GET /api/daemons/:id/fs/read

```bash
curl "http://panel:24444/api/daemons/1/fs/read?path=/files/survival/server.properties" -H "$H"
```
```json
{"content":"server-port=25565\nmotd=A Minecraft Server\n...","size":1234}
```

### POST /api/daemons/:id/fs/write

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/write -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"path":"/files/survival/server.properties","content":"server-port=25565\nmotd=Hello\n"}'
```
```json
{"ok":true}
```

### POST /api/daemons/:id/fs/mkdir

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/mkdir -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"path":"/files/survival/plugins"}'
```

### DELETE /api/daemons/:id/fs/delete

```bash
curl -X DELETE "http://panel:24444/api/daemons/1/fs/delete?path=/files/survival/old.log" -H "$H"
```

### POST /api/daemons/:id/fs/rename

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/rename -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"from":"/files/survival/world","to":"/files/survival/world-backup"}'
```

### POST /api/daemons/:id/fs/copy

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/copy -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"from":"/files/survival/server.properties","to":"/files/survival/server.properties.bak"}'
```

### POST /api/daemons/:id/fs/move

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/move -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"from":"/files/survival/old","to":"/data/vol1/old"}'
```

### POST /api/daemons/:id/fs/zip

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/zip -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"paths":["/files/survival/world"],"dest":"/files/survival/world.zip"}'
```

### POST /api/daemons/:id/fs/unzip

```bash
curl -X POST http://panel:24444/api/daemons/1/fs/unzip -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"src":"/files/survival/world.zip","destDir":"/files/survival/world-restored"}'
```

---

## 7. File Upload/Download (🔑 Query Token)

### POST /api/daemons/:id/files/upload/init

```bash
curl -X POST "http://panel:24444/api/daemons/1/files/upload/init?token=$T" \
  -H 'Content-Type: application/json' \
  -d '{"path":"/files/survival/plugins/MyPlugin.jar","filename":"MyPlugin.jar","totalBytes":1048576,"totalChunks":1}'
```
```json
{"uploadId":"ul_a1b2c3d4...","expiresAt":1714000000}
```

### POST /api/daemons/:id/files/upload

```bash
curl -X POST "http://panel:24444/api/daemons/1/files/upload?token=$T&path=/files/survival/plugins/MyPlugin.jar&uploadId=ul_a1b2c3d4&seq=0&total=1&final=true" \
  -F "file=@MyPlugin.jar"
```
```json
{"ok":true}
```

### GET /api/daemons/:id/files/download

```bash
curl -o server.jar "http://panel:24444/api/daemons/1/files/download?token=$T&path=/files/survival/server.jar"
# Returns file binary + Content-Disposition
```

---

## 8. Backups (🔓 scope `files`)

### GET /api/daemons/:id/instances/:uuid/backups

```bash
curl http://panel:24444/api/daemons/1/instances/550e8400-.../backups -H "$H"
```
```json
[{"name":"20260425-120000.zip","size":52428800,"created":1714000000,"instanceUUID":"550e8400-..."}]
```

### POST /api/daemons/:id/instances/:uuid/backups

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../backups -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"note":"before-update"}'
```
```json
{"name":"20260425-120000-before-update.zip","size":52428800,"created":1714000000,"instanceUUID":"550e8400-..."}
```

### POST /api/daemons/:id/instances/:uuid/backups/restore

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../backups/restore -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"20260425-120000-before-update.zip"}'
```
```json
{"ok":true}
```

### DELETE /api/daemons/:id/instances/:uuid/backups

```bash
curl -X DELETE "http://panel:24444/api/daemons/1/instances/550e8400-.../backups?name=20260425-120000.zip" -H "$H"
```

### GET /api/daemons/:id/instances/:uuid/backups/download

```bash
curl -o backup.zip "http://panel:24444/api/daemons/1/instances/550e8400-.../backups/download?token=$T&uuid=550e8400-...&name=20260425-120000.zip"
```

---

## 9. Docker Images (🔓/👑)

### GET /api/daemons/:id/docker/images — 🔓 [instance.read]

```bash
curl http://panel:24444/api/daemons/1/docker/images -H "$H"
```
```json
{
  "available": true,
  "images": [
    {"id":"sha256:abc123...","repository":"itzg/minecraft-server","tag":"latest","size":734003200,"created":1714000000,"displayName":"MC Server","description":"Dockerized Minecraft"},
    {"id":"sha256:def456...","repository":"eclipse-temurin","tag":"21-jre","size":209715200,"created":1714000000,"displayName":"Temurin 21 JRE"}
  ]
}
```

### POST /api/daemons/:id/docker/pull — 👑

SSE streaming progress.

```bash
curl -X POST http://panel:24444/api/daemons/1/docker/pull -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"image":"itzg/minecraft-server:latest"}'
# SSE event stream:
# data: {"type":"start","image":"itzg/minecraft-server:latest","pullId":"..."}
# data: {"type":"line","line":"latest: Pulling from itzg/minecraft-server"}
# data: {"type":"line","line":"abc123: Downloading [=>    ] 50%"}
# data: {"type":"done","error":""}
```

### DELETE /api/daemons/:id/docker/remove — 👑

```bash
curl -X DELETE "http://panel:24444/api/daemons/1/docker/remove?id=sha256:abc123..." -H "$H"
```

### PUT /api/daemons/:id/docker/images/:ref/alias — 👑

Set/clear the image display name. `:ref` = `repository:tag` (URL-encoded).

```bash
# Set alias
curl -X PUT "http://panel:24444/api/daemons/1/docker/images/eclipse-temurin%3A21-jre/alias" -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Temurin 21 JRE"}'
```
```json
{"ok":true}
```

```bash
# Clear alias (empty displayName)
curl -X PUT "http://panel:24444/api/daemons/1/docker/images/eclipse-temurin%3A21-jre/alias" -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":""}'
```

---

## 10. Managed Volumes (👑 Admin Only)

### GET /api/daemons/:id/volumes

```bash
curl http://panel:24444/api/daemons/1/volumes -H "$H"
```
```json
{"available":true,"volumes":[{"name":"inst-550e8400ab","sizeBytes":5368709120,"usedBytes":1073741824,"mounted":true,"imagePath":"/var/lib/taps/daemon/volumes/inst-550e8400ab.img","mountPath":"/var/lib/taps/daemon/volumes/inst-550e8400ab"}]}
```

### POST /api/daemons/:id/volumes

```bash
curl -X POST http://panel:24444/api/daemons/1/volumes -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"myvol","sizeBytes":10737418240}'
```

### DELETE /api/daemons/:id/volumes

```bash
curl -X DELETE "http://panel:24444/api/daemons/1/volumes?name=myvol" -H "$H"
```

---

## 11. Monitoring (👑 Admin Only)

### GET /api/daemons/:id/monitor

```bash
curl http://panel:24444/api/daemons/1/monitor -H "$H"
```
```json
{"cpuPercent":15.2,"memPercent":42.8,"memTotalBytes":8589934592,"diskPercent":55.0,"diskTotalBytes":107374182400,"uptime":86400,"loadAvg":[0.5,0.3,0.2]}
```

### GET /api/daemons/:id/monitor/history

```bash
curl http://panel:24444/api/daemons/1/monitor/history -H "$H"
```
```json
[{"time":1714000000,"cpuPercent":15.2,"memPercent":42.8},...]
```

### GET /api/daemons/:id/instances/:uuid/process

```bash
curl http://panel:24444/api/daemons/1/instances/550e8400-.../process -H "$H"
```

---

## 12. Node Groups (👑 Admin Only)

### GET /api/groups

```bash
curl http://panel:24444/api/groups -H "$H"
```
```json
[{"id":1,"name":"mc-survival","daemonIds":[1,2]}]
```

### POST /api/groups

```bash
curl -X POST http://panel:24444/api/groups -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"mc-creative","daemonIds":[1]}'
```
```json
{"id":2,"name":"mc-creative","daemonIds":[1]}
```

### PUT /api/groups/:id

```bash
curl -X PUT http://panel:24444/api/groups/2 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"mc-creative-v2","daemonIds":[1,2]}'
```

### DELETE /api/groups/:id

```bash
curl -X DELETE http://panel:24444/api/groups/2 -H "$H"
```
```json
{"ok":true}
```

### POST /api/groups/:id/resolve

Scheduler selects a node and assigns a port.

```bash
curl -X POST http://panel:24444/api/groups/1/resolve -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"type":"docker","port":0}'
```
```json
{"daemonId":1,"daemonName":"node-1","port":25565,"portFree":true,"fallbackUsed":false}
```

### POST /api/groups/:id/instances

Create an instance via the group scheduler.

```bash
curl -X POST http://panel:24444/api/groups/1/instances -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"new-mc","type":"docker","command":"itzg/minecraft-server:latest","hostPort":0,"containerPort":25565}'
```
```json
{"daemonId":1,"daemonName":"node-1","info":{...}}
```

---

## 13. Scheduled Tasks (🔓 scope `tasks`)

### GET /api/daemons/:id/instances/:uuid/tasks

```bash
curl http://panel:24444/api/daemons/1/instances/550e8400-.../tasks -H "$H"
```
```json
[{"id":1,"daemonId":1,"uuid":"550e8400-...","name":"daily-restart","cron":"0 4 * * *","action":"restart","enabled":true}]
```

### POST /api/daemons/:id/instances/:uuid/tasks

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../tasks -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"daily-restart","cron":"0 4 * * *","action":"restart","enabled":true}'
```

### PUT /api/daemons/:id/instances/:uuid/tasks/:taskId

```bash
curl -X PUT http://panel:24444/api/daemons/1/instances/550e8400-.../tasks/1 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"enabled":false}'
```

### DELETE /api/daemons/:id/instances/:uuid/tasks/:taskId

```bash
curl -X DELETE http://panel:24444/api/daemons/1/instances/550e8400-.../tasks/1 -H "$H"
```

---

## 14. API Keys (🔓 Any Logged-in User)

### GET /api/apikeys

```bash
curl http://panel:24444/api/apikeys -H "$H"
```
```json
[{"id":1,"userId":1,"name":"ci-deploy","prefix":"tps_3fe3c349","ipWhitelist":"10.0.0.0/8","scopes":"instance.control,files","lastUsed":"2026-04-25T10:00:00Z","createdAt":"..."}]
```

### POST /api/apikeys

```bash
curl -X POST http://panel:24444/api/apikeys -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-key","scopes":"instance.read","ipWhitelist":"10.0.0.5"}'
```
```json
{
  "key": "tps_3fe3c349dd703a4c29de211ffbee1663a5647573ae95ab51",
  "row": {"id":2,"userId":1,"name":"ci-key","prefix":"tps_3fe3c349","scopes":"instance.read","ipWhitelist":"10.0.0.5","createdAt":"..."}
}
```

> ⚠️ The `key` is returned only once at creation time and cannot be retrieved afterwards.

### POST /api/apikeys/:id/revoke

```bash
curl -X POST http://panel:24444/api/apikeys/2/revoke -H "$H"
```

### POST /api/apikeys/revoke-all

```bash
curl -X POST http://panel:24444/api/apikeys/revoke-all -H "$H"
```

### DELETE /api/apikeys/:id

```bash
curl -X DELETE http://panel:24444/api/apikeys/2 -H "$H"
```
```json
{"ok":true}
```

---

## 15. Permission Management (👑 Admin Only)

### GET /api/permissions

```bash
curl http://panel:24444/api/permissions -H "$H"
```
```json
[{"userId":2,"daemonId":1,"uuid":"550e8400-...","perms":7}]
```

### POST /api/permissions

```bash
curl -X POST http://panel:24444/api/permissions -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"userId":2,"daemonId":1,"uuid":"550e8400-...","perms":15}'
```

### DELETE /api/permissions

```bash
curl -X DELETE http://panel:24444/api/permissions -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"userId":2,"daemonId":1,"uuid":"550e8400-..."}'
```

---

## 16. SSO User Self-Management (🔓 scope `account`)

### GET /api/oauth/me/identities

```bash
curl http://panel:24444/api/oauth/me/identities -H "$H"
```
```json
[{"id":1,"providerName":"logto","providerDisplayName":"Logto","email":"user@example.com","linkedAt":"...","lastUsedAt":"..."}]
```

### DELETE /api/oauth/me/identities/:id

```bash
curl -X DELETE http://panel:24444/api/oauth/me/identities/1 -H "$H"
```
```json
{"ok":true}
```

Failure 400: `{"error":"sso.unlink_no_password_left",...}`

---

## 17. SSO Admin (👑 Admin Only)

### GET /api/admin/sso/providers

```bash
curl http://panel:24444/api/admin/sso/providers -H "$H"
```
```json
[{"id":1,"name":"logto","displayName":"Logto","enabled":true,"issuer":"https://logto.example.com/oidc","clientId":"xxx","hasSecret":true,"scopes":"openid profile email","autoCreate":true,"defaultRole":"user","emailDomains":"","trustUnverifiedEmail":false,"callbackUrl":"https://taps.example.com/api/oauth/callback/logto","createdAt":"..."}]
```

### POST /api/admin/sso/providers

```bash
curl -X POST http://panel:24444/api/admin/sso/providers -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"logto","displayName":"Logto","issuer":"https://logto.example.com/oidc","clientId":"xxx","clientSecret":"yyy","scopes":"openid profile email","autoCreate":true,"defaultRole":"user"}'
```

### PUT /api/admin/sso/providers/:id

```bash
curl -X PUT http://panel:24444/api/admin/sso/providers/1 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Logto SSO","enabled":false}'
```

### DELETE /api/admin/sso/providers/:id

```bash
curl -X DELETE http://panel:24444/api/admin/sso/providers/1 -H "$H"
```

### POST /api/admin/sso/providers/test

```bash
curl -X POST http://panel:24444/api/admin/sso/providers/test -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"issuer":"https://logto.example.com/oidc"}'
```
```json
{"ok":true,"authUrl":"https://logto.example.com/oidc/auth","tokenUrl":"https://logto.example.com/oidc/token"}
```

---

## 18. Audit Logs (👑 Admin Only)

### GET /api/audit

```bash
curl http://panel:24444/api/audit -H "$H"
```
```json
{"items":[{"id":100,"time":"...","userId":1,"username":"admin","method":"POST","path":"/api/users","status":200,"ip":"10.0.0.1","durationMs":5}],"total":1959}
```

### GET /api/logins

```bash
curl http://panel:24444/api/logins -H "$H"
```
```json
{"items":[{"id":50,"time":"...","username":"admin","userId":1,"success":true,"reason":"","ip":"10.0.0.1","userAgent":"Mozilla/5.0..."}],"total":877}
```

---

## 19. Server Deployment

### GET /api/serverdeploy/types — 🔓 [instance.read]

```bash
curl http://panel:24444/api/serverdeploy/types -H "$H"
```
```json
["vanilla","paper","purpur","fabric","forge","neoforge"]
```

### GET /api/serverdeploy/versions — 🔓 [instance.read]

```bash
curl "http://panel:24444/api/serverdeploy/versions?type=paper" -H "$H"
```
```json
["1.21.4","1.21.3","1.20.6",...]
```

### GET /api/serverdeploy/builds — 🔓 [instance.read]

```bash
curl "http://panel:24444/api/serverdeploy/builds?type=paper&version=1.21.4" -H "$H"
```

### POST /api/daemons/:id/instances/:uuid/serverdeploy

```bash
curl -X POST http://panel:24444/api/daemons/1/instances/550e8400-.../serverdeploy -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"type":"paper","version":"1.21.4"}'
```

### GET /api/daemons/:id/instances/:uuid/serverdeploy/status

```bash
curl http://panel:24444/api/daemons/1/instances/550e8400-.../serverdeploy/status -H "$H"
```
```json
{"uuid":"550e8400-...","active":true,"stage":"downloading","progress":45}
```

---

## 20. Template Deployment

### GET /api/templates — 🔓 [instance.read]

```bash
curl http://panel:24444/api/templates -H "$H"
```

### GET /api/templates/paper/versions — 🔓 [instance.read]

```bash
curl http://panel:24444/api/templates/paper/versions -H "$H"
```

### POST /api/daemons/:id/templates/deploy — 🔓 [instance.control]

```bash
curl -X POST http://panel:24444/api/daemons/1/templates/deploy -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"template":"paper","version":"1.21.4","name":"survival","port":25565,"memory":"2G","diskGiB":5}'
```

---

## 21. Minecraft

### GET /api/daemons/:id/instances/:uuid/players — 🔓 [instance.read]

```bash
curl http://panel:24444/api/daemons/1/instances/550e8400-.../players -H "$H"
```
```json
{"online":3,"max":20,"players":[{"name":"Steve","uuid":"..."},{"name":"Alex","uuid":"..."}]}
```

### GET /api/daemons/:id/free-port — 👑

```bash
curl "http://panel:24444/api/daemons/1/free-port?prefer=25565" -H "$H"
```
```json
{"port":25565,"free":true}
```

---

## 22. System Settings (👑 Admin Only)

All settings: GET returns the current value, PUT writes a new value.

### GET/PUT /api/settings/webhook

```bash
curl http://panel:24444/api/settings/webhook -H "$H"
```
```json
{"url":"https://hooks.example.com/taps","allowPrivate":false}
```

```bash
curl -X PUT http://panel:24444/api/settings/webhook -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://hooks.example.com/taps","allowPrivate":false}'
```
```json
{"ok":true}
```

### POST /api/settings/webhook/test

```bash
curl -X POST http://panel:24444/api/settings/webhook/test -H "$H"
```
```json
{"ok":true}
```

### GET/PUT /api/settings/deploy-source

```bash
curl http://panel:24444/api/settings/deploy-source -H "$H"
```
```json
{"source":"fastmirror"}
```

### GET/PUT /api/settings/captcha

```bash
curl http://panel:24444/api/settings/captcha -H "$H"
```
```json
{"provider":"none","siteKey":"","hasSecret":false,"scoreThreshold":0.5}
```

```bash
curl -X PUT http://panel:24444/api/settings/captcha -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"provider":"turnstile","siteKey":"0xABC","secret":"ts_secret","scoreThreshold":0.5}'
```

### POST /api/settings/captcha/test

```bash
curl -X POST http://panel:24444/api/settings/captcha/test -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"provider":"turnstile","siteKey":"0xABC","secret":"ts_secret","token":"widget-token","action":"tapstest"}'
```
```json
{"ok":true}
```

### PUT /api/settings/brand/site-name

```bash
curl -X PUT http://panel:24444/api/settings/brand/site-name -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"siteName":"My TapS"}'
```

### POST /api/settings/brand/favicon

```bash
curl -X POST http://panel:24444/api/settings/brand/favicon -H "$H" \
  -F "file=@favicon.png"
```

### DELETE /api/settings/brand/favicon

```bash
curl -X DELETE http://panel:24444/api/settings/brand/favicon -H "$H"
```

### GET/PUT /api/settings/log-limits

```bash
curl http://panel:24444/api/settings/log-limits -H "$H"
```
```json
{"auditMaxRows":1000000,"loginMaxRows":1000000}
```

### GET/PUT /api/settings/rate-limit

```bash
curl http://panel:24444/api/settings/rate-limit -H "$H"
```
```json
{"rateLimitPerMin":5,"banDurationMinutes":5,"oauthStartCount":30,"oauthStartWindowSec":300,"pkceStoreMaxEntries":10000,"terminalReadDeadlineSec":60,"terminalInputRatePerSec":200,"terminalInputBurst":50,"iconCacheMaxAgeSec":300,"iconRatePerMin":10}
```

### GET/PUT /api/settings/limits

```bash
curl http://panel:24444/api/settings/limits -H "$H"
```
```json
{"maxJsonBodyBytes":16777216,"maxWsFrameBytes":16777216,"maxRequestBodyBytes":131072}
```

### GET/PUT /api/settings/auth-timings

```bash
curl http://panel:24444/api/settings/auth-timings -H "$H"
```
```json
{"jwtTtlMinutes":60,"wsHeartbeatMinutes":5}
```

### GET/PUT /api/settings/panel-port ⚠️ Requires restart

```bash
curl http://panel:24444/api/settings/panel-port -H "$H"
```
```json
{"port":24444}
```

### GET/PUT /api/settings/http-timeouts ⚠️ Requires restart

```bash
curl http://panel:24444/api/settings/http-timeouts -H "$H"
```
```json
{"readHeaderTimeoutSec":10,"readTimeoutSec":60,"writeTimeoutSec":120,"idleTimeoutSec":120}
```

### GET/PUT /api/settings/panel-public-url

```bash
curl http://panel:24444/api/settings/panel-public-url -H "$H"
```
```json
{"url":"https://taps.example.com"}
```

### GET/PUT /api/settings/login-method

```bash
curl http://panel:24444/api/settings/login-method -H "$H"
```
```json
{"method":"password-only"}
```

### GET/PUT /api/settings/trusted-proxies ⚠️ Requires restart

```bash
curl http://panel:24444/api/settings/trusted-proxies -H "$H"
```
```json
{"proxies":"127.0.0.1, ::1"}
```

### GET/PUT /api/settings/cors-origins

```bash
curl http://panel:24444/api/settings/cors-origins -H "$H"
```
```json
{"origins":""}
```

### GET/PUT /api/settings/csp

```bash
curl http://panel:24444/api/settings/csp -H "$H"
```
```json
{"scriptSrcExtra":"https://challenges.cloudflare.com,https://www.recaptcha.net","frameSrcExtra":"https://challenges.cloudflare.com,https://www.google.com,https://www.recaptcha.net"}
```

### GET/PUT /api/settings/hibernation

```bash
curl http://panel:24444/api/settings/hibernation -H "$H"
```
```json
{"defaultEnabled":true,"defaultMinutes":60,"warmupMinutes":5,"motd":"§e§l[休眠中]...","kickMessage":"§e服务器正在启动...","hasIcon":true}
```

### POST /api/settings/hibernation/icon

```bash
curl -X POST http://panel:24444/api/settings/hibernation/icon -H "$H" \
  -F "file=@icon.png"
```

### DELETE /api/settings/hibernation/icon

```bash
curl -X DELETE http://panel:24444/api/settings/hibernation/icon -H "$H"
```

---

## 23. WebSocket Terminal

### GET /api/ws/instance/:id/:uuid/terminal

```
ws://panel:24444/api/ws/instance/1/550e8400-.../terminal?token=<jwt>
```

**Inbound frames** (JSON text):
```json
{"type":"input","data":"help\n"}
{"type":"resize","cols":120,"rows":40}
```

**Outbound frames** (plain text): Instance stdout content, pushed directly to xterm.

**Permissions**: PermView allows connecting (read-only stdout); PermTerminal / PermControl allows sending input.

**Origin check**: Must match the Panel public URL, otherwise 403.

---

## Field Definitions

### InstanceConfig

| Field | Type | Description |
|-------|------|-------------|
| uuid | string | Instance unique ID (8-4-4-4-12 hex) |
| name | string | Human-readable name |
| type | string | `"docker"` / `"generic"` / `"minecraft"` |
| workingDir | string | Working directory (relative to daemon data dir; absolute paths used directly) |
| command | string | Image name for docker; executable command for generic |
| args | string[] | Command arguments |
| stopCmd | string | Graceful stop command written to stdin (e.g. `"stop"`) |
| autoStart | bool | Whether to auto-start when daemon starts |
| autoRestart | bool | Whether to auto-restart after crash |
| restartDelay | int | Auto-restart delay in seconds (default 5) |
| createdAt | int64 | Creation time (unix seconds) |
| outputEncoding | string | Output encoding (`""` / `"utf-8"` = passthrough; `"gbk"` = Chinese Windows servers) |
| minecraftHost | string | MC SLP query address (default 127.0.0.1) |
| minecraftPort | int | MC SLP query port (default 25565) |
| dockerEnv | string[] | Environment variables `KEY=VAL` |
| dockerVolumes | string[] | Mounts `host:container[:mode]` |
| dockerPorts | string[] | Port mappings `host:container[/proto]` |
| dockerCpu | string | CPU limit (e.g. `"1.5"`) |
| dockerMemory | string | Memory limit (e.g. `"2g"`) |
| dockerDiskSize | string | Disk quota (e.g. `"10g"`), automatically creates a loopback volume |
| managedVolume | string | Auto-created volume name (managed by daemon, cleaned up on instance deletion) |
| autoDataDir | bool | Whether to auto-assign a /data directory (non-loopback) |
| completionWords | string[] | Terminal tab-completion word list |
| hibernationEnabled | bool\|null | Hibernation toggle (null = follow global default) |
| hibernationIdleMinutes | int | Idle minutes (0 = follow global) |
| hibernationActive | bool | Whether currently in hibernation state (daemon runtime state) |

### InstanceInfo

| Field | Type | Description |
|-------|------|-------------|
| config | InstanceConfig | Instance configuration |
| status | string | `stopped` / `starting` / `running` / `stopping` / `crashed` / `hibernating` |
| pid | int | Process PID (0 = not running) |
| exitCode | int | Last exit code |

### DockerImage

| Field | Type | Description |
|-------|------|-------------|
| id | string | Image ID (sha256:...) |
| repository | string | Repository name (e.g. `itzg/minecraft-server`) |
| tag | string | Tag (e.g. `latest`) |
| size | int64 | Size in bytes |
| created | int64 | Creation time (unix seconds) |
| displayName | string | Display name (priority: admin alias > taps.displayName label > OCI title) |
| description | string | Description (priority: taps.description label > OCI description) |

### DockerStatsResp

| Field | Type | Description |
|-------|------|-------------|
| name | string | Container name (`taps-<uuid>`) |
| running | bool | Whether running |
| cpuPercent | float64 | CPU usage % |
| memBytes | int64 | Memory usage in bytes |
| memLimit | int64 | Memory limit in bytes |
| memPercent | float64 | Memory usage % |
| netRxBytes | int64 | Network received bytes |
| netTxBytes | int64 | Network transmitted bytes |
| blockReadBytes | int64 | Disk read bytes |
| blockWriteBytes | int64 | Disk write bytes |
| diskUsedBytes | int64 | Managed volume used bytes (0 if no volume) |
| diskTotalBytes | int64 | Managed volume total bytes (0 if no volume) |

### FsEntry

| Field | Type | Description |
|-------|------|-------------|
| name | string | File/directory name |
| isDir | bool | Whether it is a directory |
| size | int64 | File size in bytes (0 for directories) |
| modified | int64 | Last modified time (unix seconds) |
| mode | string | Permission string (e.g. `drwxr-xr-x`) |

### BackupEntry

| Field | Type | Description |
|-------|------|-------------|
| name | string | Backup filename (timestamped .zip) |
| size | int64 | Size in bytes |
| created | int64 | Creation time (unix seconds) |
| instanceUuid | string | Owning instance UUID |

### McPlayersResp

| Field | Type | Description |
|-------|------|-------------|
| online | bool | Whether SLP is reachable |
| count | int | Online player count |
| max | int | Maximum player count |
| players | McPlayer[] | Player list `[{name, uuid}]` |
| version | string | Server version |
| description | string | MOTD |

### PlayersBrief (Dashboard Bulk Player Overview)

| Field | Type | Description |
|-------|------|-------------|
| uuid | string | Instance UUID |
| online | bool | Whether SLP is reachable |
| count | int | Online player count |
| max | int | Maximum player count |

### ProcessSnapshot

| Field | Type | Description |
|-------|------|-------------|
| uuid | string | Instance UUID |
| pid | int | Process PID |
| running | bool | Whether running |
| cpuPercent | float64 | CPU % |
| memBytes | uint64 | Memory in bytes |
| numThreads | int32 | Thread count |
| timestamp | int64 | Sample time (unix seconds) |

### Volume

| Field | Type | Description |
|-------|------|-------------|
| name | string | Volume name |
| sizeBytes | int64 | Total size |
| usedBytes | int64 | Used size |
| mounted | bool | Whether mounted |
| imagePath | string | .img file path |
| mountPath | string | Mount point path |

---

### Database Models

#### User

| Field | Type | JSON | Description |
|-------|------|------|-------------|
| id | uint | `id` | Primary key |
| username | string | `username` | Unique (case-insensitive) |
| role | string | `role` | `admin` / `user` |
| email | string | `email` | Optional; LOWER unique index (when non-empty) |
| mustChangePassword | bool | `mustChangePassword` | Force password change on first login |
| hasPassword | bool | `hasPassword` | false = account auto-created via SSO |
| createdAt | time | `createdAt` | |

> passwordHash / tokensInvalidBefore are not exposed externally.

#### Daemon

| Field | Type | JSON | Description |
|-------|------|------|-------------|
| id | uint | `id` | Primary key |
| name | string | `name` | Display name |
| address | string | `address` | host:port (used by Panel to connect to daemon) |
| displayHost | string | `displayHost` | Public address for player connections |
| portMin / portMax | int | `portMin` / `portMax` | Auto-assigned port range (default 25565-25600) |
| certFingerprint | string | `certFingerprint` | SHA-256 fingerprint (colon-separated hex) |
| connected | bool | runtime | Whether daemon is online (not a DB field) |

> token is not exposed externally.

#### InstancePermission

| Field | Type | Description |
|-------|------|-------------|
| userId | uint | User ID |
| daemonId | uint | Node ID |
| uuid | string | Instance UUID |
| perms | uint32 | Permission bitmask: 1=View 2=Control 4=Files 8=Terminal 16=Manage |

#### Task (Scheduled Task)

| Field | Type | Description |
|-------|------|-------------|
| id | uint | Primary key |
| daemonId | uint | Node ID |
| uuid | string | Instance UUID |
| name | string | Task name |
| cron | string | Cron expression (5 fields) |
| action | string | `command` / `start` / `stop` / `restart` / `backup` |
| data | string | Text sent to stdin when action=command |
| enabled | bool | Whether enabled |

#### APIKey

| Field | Type | Description |
|-------|------|-------------|
| id | uint | Primary key |
| userId | uint | Owning user |
| name | string | Name |
| prefix | string | First 8 characters (for display) |
| ipWhitelist | string | Allowed IPs (CSV; empty = any) |
| scopes | string | Allowed scopes (CSV; empty = all) |
| expiresAt | time\|null | Expiration time (null = never expires) |
| revokedAt | time\|null | Revocation time (null = not revoked) |

> The full key (`tps_...`) is returned at creation time only and cannot be retrieved afterwards.

#### AuditLog

| Field | Type | Description |
|-------|------|-------------|
| id | uint | Primary key |
| time | time | Operation time |
| userId | uint | Operating user |
| username | string | Username |
| method | string | HTTP method |
| path | string | Request path |
| status | int | HTTP status code |
| ip | string | Client IP |
| durationMs | int64 | Processing duration in ms |

#### LoginLog

| Field | Type | Description |
|-------|------|-------------|
| id | uint | Primary key |
| time | time | Login time |
| username | string | Attempted username |
| userId | uint | User ID (0 = non-existent user) |
| success | bool | Whether successful |
| reason | string | Failure reason (on success: captcha bypass info or `sso:<name>`) |
| ip | string | Client IP |
| userAgent | string | User-Agent |

#### SSOProvider

| Field | Type | Description |
|-------|------|-------------|
| id | uint | Primary key |
| name | string | URL slug (`[a-z0-9_-]{1,64}`) |
| displayName | string | UI display name |
| enabled | bool | Whether enabled |
| issuer | string | OIDC issuer URL |
| clientId | string | OAuth client ID |
| hasSecret | bool | Whether a secret is stored (returned in GET; the secret itself is not echoed) |
| scopes | string | Requested scopes (default `openid profile email`) |
| autoCreate | bool | Whether to auto-create local users |
| defaultRole | string | Role for auto-created users (`user` / `admin`) |
| emailDomains | string | Allowed email domains (CSV; empty = unrestricted) |
| trustUnverifiedEmail | bool | Whether to trust unverified emails |
| callbackUrl | string | Computed callback URL (read-only) |

#### DockerImageAlias

| Field | Type | Description |
|-------|------|-------------|
| id | uint | Primary key |
| daemonId | uint | Node ID |
| imageRef | string | Image reference (`repository:tag`) |
| displayName | string | Admin-set display name |
