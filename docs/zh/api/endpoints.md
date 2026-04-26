# API 端点参考

所有端点前缀 `/api/`。除非另注，请求/响应均 `application/json`。

**鉴权图标**：
- 🌐 = 完全公开
- 🔓 = 需登录（Bearer JWT 或 API Key）
- 🔑 = `?token=<jwt>` 查询参数
- 👑 = 必须 `admin` role
- `[scope]` = API Key 必须含此 scope
- `+ perm` = 还需对应实例的 instance permission

**约定**：以下示例中 `$T` 代表有效 JWT token，`$H` 代表 `Authorization: Bearer $T`。

---

## 1. 公开端点（🌐 无需认证）

### POST /api/auth/login

用户名密码登录。

```bash
curl -X POST http://panel:24444/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"mypassword","captchaToken":""}'
```

成功 200：
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

失败 401：`{"error":"auth.invalid_credentials","message":"invalid credentials"}`
失败 400：`{"error":"common.invalid_body","message":"invalid body"}`
失败 429：`{"error":"auth.rate_limited","message":"...","params":{"retryAfter":298}}`

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
# 返回 PNG/ICO 二进制；404 = 未设置
```

### GET /api/settings/hibernation/icon

```bash
curl -o icon.png http://panel:24444/api/settings/hibernation/icon
# 64×64 PNG；带 Cache-Control: public, max-age=300；404 = 未设置
```

### GET /api/oauth/providers

```bash
curl http://panel:24444/api/oauth/providers
```
```json
[{"name":"logto","displayName":"Logto"}]
```
密码模式下返回空数组 `[]`。

### GET /api/oauth/start/:name

```bash
curl -v http://panel:24444/api/oauth/start/logto
# 302 重定向到 IdP 授权 URL
```

### GET /api/oauth/callback/:name

IdP 回调，302 到 `<publicUrl>/#oauth-token=<jwt>` 或 `/#oauth-error=<code>`。

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

## 2. 当前用户（🔓 需登录）

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

失败 401：`{"error":"user.wrong_current_password",...}`

---

## 3. 用户管理（👑 admin only）

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

失败 409：`{"error":"user.username_taken",...}` 或 `{"error":"user.email_taken",...}`

### PUT /api/users/:id

```bash
curl -X PUT http://panel:24444/api/users/3 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","email":"updated@example.com"}'
```
```json
{"id":3,"username":"newuser","role":"admin","email":"updated@example.com",...}
```

失败 400：`{"error":"user.cannot_demote_last_admin",...}`

### DELETE /api/users/:id

```bash
curl -X DELETE http://panel:24444/api/users/3 -H "$H"
```
```json
{"ok":true}
```

---

## 4. 节点管理（👑 admin only）

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
级联删除 InstancePermission + Task + NodeGroupMember。

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

🔓 任何已登录用户可调用（展示给 user 角色看节点地址）。

```bash
curl http://panel:24444/api/daemons/1/public -H "$H"
```
```json
{"id":1,"name":"node-1","displayHost":"mc.example.com"}
```

---

## 5. 实例管理（🔓 scope + perm）

### GET /api/instances

聚合所有节点的实例列表（按用户权限过滤）。

```bash
curl http://panel:24444/api/instances -H "$H"
```
```json
[{"daemonId":1,"info":{"config":{"uuid":"550e8400-...","name":"survival","type":"docker","command":"itzg/minecraft-server",...},"status":"running","pid":12345}}]
```

### GET /api/daemons/:id/instances

单节点实例列表。

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

所有实例的 Docker stats（一次性）。

```bash
curl http://panel:24444/api/daemons/1/instances-dockerstats -H "$H"
```
```json
{"items":[{"name":"taps-550e8400-...","running":true,"cpuPercent":2.5,...}]}
```

### GET /api/daemons/:id/instances-players

所有实例的玩家概览。

```bash
curl http://panel:24444/api/daemons/1/instances-players -H "$H"
```
```json
{"items":[{"uuid":"550e8400-...","online":3,"max":20}]}
```

---

## 6. 文件系统（🔓 scope `files` + path 权限）

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

## 7. 文件上传/下载（🔑 query token）

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
# 返回文件二进制 + Content-Disposition
```

---

## 8. 备份（🔓 scope `files`）

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

## 9. Docker 镜像（🔓/👑）

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

SSE 流式进度。

```bash
curl -X POST http://panel:24444/api/daemons/1/docker/pull -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"image":"itzg/minecraft-server:latest"}'
# SSE 事件流：
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

设置/清除镜像显示名称。`:ref` = `repository:tag`（URL 编码）。

```bash
# 设置别名
curl -X PUT "http://panel:24444/api/daemons/1/docker/images/eclipse-temurin%3A21-jre/alias" -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Temurin 21 JRE"}'
```
```json
{"ok":true}
```

```bash
# 清除别名（空 displayName）
curl -X PUT "http://panel:24444/api/daemons/1/docker/images/eclipse-temurin%3A21-jre/alias" -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":""}'
```

---

## 10. 托管卷（👑 admin only）

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

## 11. 监控（👑 admin only）

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

## 12. 节点组（👑 admin only）

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

调度器选节点 + 分配端口。

```bash
curl -X POST http://panel:24444/api/groups/1/resolve -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"type":"docker","port":0}'
```
```json
{"daemonId":1,"daemonName":"node-1","port":25565,"portFree":true,"fallbackUsed":false}
```

### POST /api/groups/:id/instances

通过 group 调度器创建实例。

```bash
curl -X POST http://panel:24444/api/groups/1/instances -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"new-mc","type":"docker","command":"itzg/minecraft-server:latest","hostPort":0,"containerPort":25565}'
```
```json
{"daemonId":1,"daemonName":"node-1","info":{...}}
```

---

## 13. 计划任务（🔓 scope `tasks`）

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

## 14. API Key（🔓 任何已登录用户）

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

> ⚠️ `key` 仅创建时返回一次，之后不可查。

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

## 15. 权限管理（👑 admin only）

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

## 16. SSO 用户自管（🔓 scope `account`）

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

失败 400：`{"error":"sso.unlink_no_password_left",...}`

---

## 17. SSO Admin（👑 admin only）

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

## 18. 审计日志（👑 admin only）

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

## 19. 服务端部署

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

## 20. 模板部署

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

## 22. 系统设置（👑 admin only）

所有设置均 GET 返回当前值、PUT 写入新值。

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

### GET/PUT /api/settings/panel-port ⚠️需重启

```bash
curl http://panel:24444/api/settings/panel-port -H "$H"
```
```json
{"port":24444}
```

### GET/PUT /api/settings/http-timeouts ⚠️需重启

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

### GET/PUT /api/settings/trusted-proxies ⚠️需重启

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

## 23. WebSocket 终端

### GET /api/ws/instance/:id/:uuid/terminal

```
ws://panel:24444/api/ws/instance/1/550e8400-.../terminal?token=<jwt>
```

**入站帧**（JSON text）：
```json
{"type":"input","data":"help\n"}
{"type":"resize","cols":120,"rows":40}
```

**出站帧**（plain text）：实例 stdout 内容，直推 xterm。

**权限**：PermView 可连（只读 stdout）；PermTerminal / PermControl 可发 input。

**Origin 校验**：必须匹配 Panel 公开地址，否则 403。

---

## 字段定义

### InstanceConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| uuid | string | 实例唯一 ID（8-4-4-4-12 hex） |
| name | string | 人类可读名称 |
| type | string | `"docker"` / `"generic"` / `"minecraft"` |
| workingDir | string | 工作目录（相对 daemon 数据目录；绝对路径直接用） |
| command | string | docker 时为镜像名；generic 时为可执行命令 |
| args | string[] | 命令参数 |
| stopCmd | string | 写入 stdin 的优雅停止命令（如 `"stop"`） |
| autoStart | bool | daemon 启动时是否自动拉起 |
| autoRestart | bool | 崩溃后是否自动重启 |
| restartDelay | int | 自动重启延迟秒数（默认 5） |
| createdAt | int64 | 创建时间（unix 秒） |
| outputEncoding | string | 输出编码（`""` / `"utf-8"` = 直通；`"gbk"` = 中文 Windows 服务端） |
| minecraftHost | string | MC SLP 查询地址（默认 127.0.0.1） |
| minecraftPort | int | MC SLP 查询端口（默认 25565） |
| dockerEnv | string[] | 环境变量 `KEY=VAL` |
| dockerVolumes | string[] | 挂载 `host:container[:mode]` |
| dockerPorts | string[] | 端口映射 `host:container[/proto]` |
| dockerCpu | string | CPU 限制（如 `"1.5"`） |
| dockerMemory | string | 内存限制（如 `"2g"`） |
| dockerDiskSize | string | 磁盘配额（如 `"10g"`），自动创建 loopback 卷 |
| managedVolume | string | 自动创建的卷名（daemon 管理，删除实例时清理） |
| autoDataDir | bool | 是否自动分配 /data 目录（非 loopback） |
| completionWords | string[] | 终端 Tab 补全词汇表 |
| hibernationEnabled | bool\|null | 休眠开关（null = 跟随全局默认） |
| hibernationIdleMinutes | int | 空闲分钟数（0 = 跟随全局） |
| hibernationActive | bool | 当前是否处于休眠状态（daemon 运行时状态） |

### InstanceInfo

| 字段 | 类型 | 说明 |
|------|------|------|
| config | InstanceConfig | 实例配置 |
| status | string | `stopped` / `starting` / `running` / `stopping` / `crashed` / `hibernating` |
| pid | int | 进程 PID（0 = 未运行） |
| exitCode | int | 上次退出码 |

### DockerImage

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 镜像 ID（sha256:...） |
| repository | string | 仓库名（如 `itzg/minecraft-server`） |
| tag | string | 标签（如 `latest`） |
| size | int64 | 字节数 |
| created | int64 | 创建时间（unix 秒） |
| displayName | string | 显示名称（优先级：admin 别名 > taps.displayName label > OCI title） |
| description | string | 描述（优先级：taps.description label > OCI description） |

### DockerStatsResp

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 容器名（`taps-<uuid>`） |
| running | bool | 是否运行中 |
| cpuPercent | float64 | CPU 使用率 % |
| memBytes | int64 | 内存使用字节 |
| memLimit | int64 | 内存上限字节 |
| memPercent | float64 | 内存使用率 % |
| netRxBytes | int64 | 网络接收字节 |
| netTxBytes | int64 | 网络发送字节 |
| blockReadBytes | int64 | 磁盘读字节 |
| blockWriteBytes | int64 | 磁盘写字节 |
| diskUsedBytes | int64 | 托管卷已用字节（无卷时 0） |
| diskTotalBytes | int64 | 托管卷总字节（无卷时 0） |

### FsEntry

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 文件/目录名 |
| isDir | bool | 是否目录 |
| size | int64 | 文件大小字节（目录为 0） |
| modified | int64 | 最后修改时间（unix 秒） |
| mode | string | 权限字符串（如 `drwxr-xr-x`） |

### BackupEntry

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 备份文件名（带时间戳的 .zip） |
| size | int64 | 字节数 |
| created | int64 | 创建时间（unix 秒） |
| instanceUuid | string | 所属实例 UUID |

### McPlayersResp

| 字段 | 类型 | 说明 |
|------|------|------|
| online | bool | SLP 是否可达 |
| count | int | 在线玩家数 |
| max | int | 最大玩家数 |
| players | McPlayer[] | 玩家列表 `[{name, uuid}]` |
| version | string | 服务端版本 |
| description | string | MOTD |

### PlayersBrief（仪表盘批量玩家概览）

| 字段 | 类型 | 说明 |
|------|------|------|
| uuid | string | 实例 UUID |
| online | bool | SLP 是否可达 |
| count | int | 在线玩家数 |
| max | int | 最大玩家数 |

### ProcessSnapshot

| 字段 | 类型 | 说明 |
|------|------|------|
| uuid | string | 实例 UUID |
| pid | int | 进程 PID |
| running | bool | 是否运行中 |
| cpuPercent | float64 | CPU % |
| memBytes | uint64 | 内存字节 |
| numThreads | int32 | 线程数 |
| timestamp | int64 | 采样时间（unix 秒） |

### Volume

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 卷名 |
| sizeBytes | int64 | 总大小 |
| usedBytes | int64 | 已用大小 |
| mounted | bool | 是否已挂载 |
| imagePath | string | .img 文件路径 |
| mountPath | string | 挂载点路径 |

---

### 数据库模型

#### User

| 字段 | 类型 | JSON | 说明 |
|------|------|------|------|
| id | uint | `id` | 主键 |
| username | string | `username` | 唯一（大小写不敏感） |
| role | string | `role` | `admin` / `user` |
| email | string | `email` | 可选；LOWER 唯一索引（非空时） |
| mustChangePassword | bool | `mustChangePassword` | 首次登录强制改密 |
| hasPassword | bool | `hasPassword` | false = SSO 自动创建的账户 |
| createdAt | time | `createdAt` | |

> passwordHash / tokensInvalidBefore 不对外暴露。

#### Daemon

| 字段 | 类型 | JSON | 说明 |
|------|------|------|------|
| id | uint | `id` | 主键 |
| name | string | `name` | 显示名 |
| address | string | `address` | host:port（Panel 连 daemon 用） |
| displayHost | string | `displayHost` | 玩家连接用的公开地址 |
| portMin / portMax | int | `portMin` / `portMax` | 自动分配端口范围（默认 25565-25600） |
| certFingerprint | string | `certFingerprint` | SHA-256 指纹（冒号十六进制） |
| connected | bool | 运行时 | daemon 是否在线（非 DB 字段） |

> token 不对外暴露。

#### InstancePermission

| 字段 | 类型 | 说明 |
|------|------|------|
| userId | uint | 用户 ID |
| daemonId | uint | 节点 ID |
| uuid | string | 实例 UUID |
| perms | uint32 | 权限位掩码：1=View 2=Control 4=Files 8=Terminal 16=Manage |

#### Task（计划任务）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| daemonId | uint | 节点 ID |
| uuid | string | 实例 UUID |
| name | string | 任务名 |
| cron | string | cron 表达式（5 字段） |
| action | string | `command` / `start` / `stop` / `restart` / `backup` |
| data | string | action=command 时为发送到 stdin 的文本 |
| enabled | bool | 是否启用 |

#### APIKey

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| userId | uint | 所属用户 |
| name | string | 名称 |
| prefix | string | 前 8 字符（显示用） |
| ipWhitelist | string | 允许 IP（CSV；空 = 任意） |
| scopes | string | 允许 scope（CSV；空 = 全部） |
| expiresAt | time\|null | 过期时间（null = 永不过期） |
| revokedAt | time\|null | 撤销时间（null = 未撤销） |

> 创建时返回完整 key（`tps_...`），之后不可查。

#### AuditLog

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| time | time | 操作时间 |
| userId | uint | 操作用户 |
| username | string | 用户名 |
| method | string | HTTP 方法 |
| path | string | 请求路径 |
| status | int | HTTP 状态码 |
| ip | string | 客户端 IP |
| durationMs | int64 | 处理耗时 ms |

#### LoginLog

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| time | time | 登录时间 |
| username | string | 尝试的用户名 |
| userId | uint | 用户 ID（0 = 不存在的用户） |
| success | bool | 是否成功 |
| reason | string | 失败原因（成功时为 captcha bypass 信息或 `sso:<name>`） |
| ip | string | 客户端 IP |
| userAgent | string | User-Agent |

#### SSOProvider

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| name | string | URL slug（`[a-z0-9_-]{1,64}`） |
| displayName | string | UI 显示名 |
| enabled | bool | 是否启用 |
| issuer | string | OIDC issuer URL |
| clientId | string | OAuth client ID |
| hasSecret | bool | 是否已存 secret（GET 返回；secret 本身不回显） |
| scopes | string | 请求的 scope（默认 `openid profile email`） |
| autoCreate | bool | 是否自动创建本地用户 |
| defaultRole | string | 自动创建时的角色（`user` / `admin`） |
| emailDomains | string | 允许的邮箱域（CSV；空 = 不限） |
| trustUnverifiedEmail | bool | 是否信任未验证邮箱 |
| callbackUrl | string | 计算出的回调 URL（只读） |

#### DockerImageAlias

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 主键 |
| daemonId | uint | 节点 ID |
| imageRef | string | 镜像引用（`repository:tag`） |
| displayName | string | admin 设的显示名称 |
