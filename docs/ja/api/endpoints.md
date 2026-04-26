[English](../../api/endpoints.md) | [中文](../zh/api/endpoints.md) | **日本語**

# API エンドポイントリファレンス

すべてのエンドポイントには `/api/` プレフィックスが付きます。特に記載がない限り、リクエストとレスポンスは `application/json` を使用します。

**認証アイコン**:
- 🌐 = 完全に公開
- 🔓 = ログイン必須（Bearer JWT または API キー）
- 🔑 = `?token=<jwt>` クエリパラメータ
- 👑 = `admin` ロールが必要
- `[scope]` = API キーにこのスコープが必要
- `+ perm` = 対応するインスタンス権限も必要

**規約**: 以下の例では、`$T` は有効な JWT トークン、`$H` は `Authorization: Bearer $T` を表します。

---

## 1. 公開エンドポイント（🌐 認証不要）

### POST /api/auth/login

ユーザー名/パスワードによるログイン。

```bash
curl -X POST http://panel:24444/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"mypassword","captchaToken":""}'
```

成功 200:
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

失敗 401: `{"error":"auth.invalid_credentials","message":"invalid credentials"}`
失敗 400: `{"error":"common.invalid_body","message":"invalid body"}`
失敗 429: `{"error":"auth.rate_limited","message":"...","params":{"retryAfter":298}}`

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
# PNG/ICO バイナリを返します。404 = 未設定
```

### GET /api/settings/hibernation/icon

```bash
curl -o icon.png http://panel:24444/api/settings/hibernation/icon
# 64x64 PNG。Cache-Control: public, max-age=300 付き。404 = 未設定
```

### GET /api/oauth/providers

```bash
curl http://panel:24444/api/oauth/providers
```
```json
[{"name":"logto","displayName":"Logto"}]
```
パスワードモードでは空の配列 `[]` を返します。

### GET /api/oauth/start/:name

```bash
curl -v http://panel:24444/api/oauth/start/logto
# 302 リダイレクト（IdP 認可 URL へ）
```

### GET /api/oauth/callback/:name

IdP コールバック。`<publicUrl>/#oauth-token=<jwt>` または `/#oauth-error=<code>` へ 302 リダイレクトします。

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

## 2. 現在のユーザー（🔓 ログイン必須）

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

失敗 401: `{"error":"user.wrong_current_password",...}`

---

## 3. ユーザー管理（👑 管理者のみ）

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

失敗 409: `{"error":"user.username_taken",...}` または `{"error":"user.email_taken",...}`

### PUT /api/users/:id

```bash
curl -X PUT http://panel:24444/api/users/3 -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","email":"updated@example.com"}'
```
```json
{"id":3,"username":"newuser","role":"admin","email":"updated@example.com",...}
```

失敗 400: `{"error":"user.cannot_demote_last_admin",...}`

### DELETE /api/users/:id

```bash
curl -X DELETE http://panel:24444/api/users/3 -H "$H"
```
```json
{"ok":true}
```

---

## 4. ノード管理（👑 管理者のみ）

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
InstancePermission + Task + NodeGroupMember をカスケード削除します。

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

🔓 ログイン済みのユーザーであれば誰でも呼び出せます（user ロールにノードアドレスを表示）。

```bash
curl http://panel:24444/api/daemons/1/public -H "$H"
```
```json
{"id":1,"name":"node-1","displayHost":"mc.example.com"}
```

---

## 5. インスタンス管理（🔓 scope + perm）

### GET /api/instances

すべてのノードのインスタンス一覧を集約します（ユーザー権限でフィルタリング）。

```bash
curl http://panel:24444/api/instances -H "$H"
```
```json
[{"daemonId":1,"info":{"config":{"uuid":"550e8400-...","name":"survival","type":"docker","command":"itzg/minecraft-server",...},"status":"running","pid":12345}}]
```

### GET /api/daemons/:id/instances

単一ノードのインスタンス一覧。

```bash
curl http://panel:24444/api/daemons/1/instances -H "$H"
```

### POST /api/daemons/:id/instances — 👑 管理者

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

### DELETE /api/daemons/:id/instances/:uuid — 👑 管理者

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

全インスタンスの Docker 統計情報（ワンショット）。

```bash
curl http://panel:24444/api/daemons/1/instances-dockerstats -H "$H"
```
```json
{"items":[{"name":"taps-550e8400-...","running":true,"cpuPercent":2.5,...}]}
```

### GET /api/daemons/:id/instances-players

全インスタンスのプレイヤー概要。

```bash
curl http://panel:24444/api/daemons/1/instances-players -H "$H"
```
```json
{"items":[{"uuid":"550e8400-...","online":3,"max":20}]}
```

---

## 6. ファイルシステム（🔓 scope `files` + パス権限）

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

## 7. ファイルアップロード/ダウンロード（🔑 クエリトークン）

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
# ファイルバイナリ + Content-Disposition を返します
```

---

## 8. バックアップ（🔓 scope `files`）

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

## 9. Docker イメージ（🔓/👑）

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

SSE ストリーミング進捗。

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

イメージの表示名を設定/クリアします。`:ref` = `repository:tag`（URL エンコード）。

```bash
# 表示名を設定
curl -X PUT "http://panel:24444/api/daemons/1/docker/images/eclipse-temurin%3A21-jre/alias" -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Temurin 21 JRE"}'
```
```json
{"ok":true}
```

```bash
# 表示名をクリア（空の displayName）
curl -X PUT "http://panel:24444/api/daemons/1/docker/images/eclipse-temurin%3A21-jre/alias" -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":""}'
```

---

## 10. マネージドボリューム（👑 管理者のみ）

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

## 11. モニタリング（👑 管理者のみ）

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

## 12. ノードグループ（👑 管理者のみ）

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

スケジューラがノードを選択し、ポートを割り当てます。

```bash
curl -X POST http://panel:24444/api/groups/1/resolve -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"type":"docker","port":0}'
```
```json
{"daemonId":1,"daemonName":"node-1","port":25565,"portFree":true,"fallbackUsed":false}
```

### POST /api/groups/:id/instances

グループスケジューラ経由でインスタンスを作成します。

```bash
curl -X POST http://panel:24444/api/groups/1/instances -H "$H" \
  -H 'Content-Type: application/json' \
  -d '{"name":"new-mc","type":"docker","command":"itzg/minecraft-server:latest","hostPort":0,"containerPort":25565}'
```
```json
{"daemonId":1,"daemonName":"node-1","info":{...}}
```

---

## 13. スケジュールタスク（🔓 scope `tasks`）

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

## 14. API キー（🔓 ログイン済みユーザー）

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

> ⚠️ `key` は作成時に一度だけ返され、以降は取得できません。

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

## 15. 権限管理（👑 管理者のみ）

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

## 16. SSO ユーザーセルフ管理（🔓 scope `account`）

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

失敗 400: `{"error":"sso.unlink_no_password_left",...}`

---

## 17. SSO 管理（👑 管理者のみ）

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

## 18. 監査ログ（👑 管理者のみ）

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

## 19. サーバーデプロイ

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

## 20. テンプレートデプロイ

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

## 22. システム設定（👑 管理者のみ）

すべての設定: GET は現在の値を返し、PUT は新しい値を書き込みます。

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

### GET/PUT /api/settings/panel-port ⚠️ 再起動が必要

```bash
curl http://panel:24444/api/settings/panel-port -H "$H"
```
```json
{"port":24444}
```

### GET/PUT /api/settings/http-timeouts ⚠️ 再起動が必要

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

### GET/PUT /api/settings/trusted-proxies ⚠️ 再起動が必要

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

## 23. WebSocket ターミナル

### GET /api/ws/instance/:id/:uuid/terminal

```
ws://panel:24444/api/ws/instance/1/550e8400-.../terminal?token=<jwt>
```

**受信フレーム**（JSON テキスト）:
```json
{"type":"input","data":"help\n"}
{"type":"resize","cols":120,"rows":40}
```

**送信フレーム**（プレーンテキスト）: インスタンスの stdout コンテンツが xterm に直接プッシュされます。

**権限**: PermView で接続可能（stdout の読み取り専用）。PermTerminal / PermControl で入力の送信が可能。

**オリジンチェック**: パネルの公開 URL と一致する必要があります。一致しない場合は 403 が返されます。

---

## フィールド定義

### InstanceConfig

| フィールド | 型 | 説明 |
|-----------|-----|------|
| uuid | string | インスタンス固有 ID（8-4-4-4-12 の 16 進数） |
| name | string | 人間が読める名前 |
| type | string | `"docker"` / `"generic"` / `"minecraft"` |
| workingDir | string | 作業ディレクトリ（デーモンデータディレクトリからの相対パス。絶対パスの場合はそのまま使用） |
| command | string | docker の場合はイメージ名。generic の場合は実行コマンド |
| args | string[] | コマンド引数 |
| stopCmd | string | stdin に送信される正常停止コマンド（例: `"stop"`） |
| autoStart | bool | デーモン起動時に自動開始するか |
| autoRestart | bool | クラッシュ後に自動再起動するか |
| restartDelay | int | 自動再起動の遅延（秒、デフォルト 5） |
| createdAt | int64 | 作成時刻（Unix 秒） |
| outputEncoding | string | 出力エンコーディング（`""` / `"utf-8"` = パススルー、`"gbk"` = 中国語 Windows サーバー） |
| minecraftHost | string | MC SLP クエリアドレス（デフォルト 127.0.0.1） |
| minecraftPort | int | MC SLP クエリポート（デフォルト 25565） |
| dockerEnv | string[] | 環境変数 `KEY=VAL` |
| dockerVolumes | string[] | マウント `host:container[:mode]` |
| dockerPorts | string[] | ポートマッピング `host:container[/proto]` |
| dockerCpu | string | CPU 制限（例: `"1.5"`） |
| dockerMemory | string | メモリ制限（例: `"2g"`） |
| dockerDiskSize | string | ディスククォータ（例: `"10g"`）、自動的にループバックボリュームを作成 |
| managedVolume | string | 自動作成されたボリューム名（デーモンが管理、インスタンス削除時にクリーンアップ） |
| autoDataDir | bool | /data ディレクトリを自動割り当てするか（非ループバック） |
| completionWords | string[] | ターミナルのタブ補完ワードリスト |
| hibernationEnabled | bool\|null | 休眠トグル（null = グローバルデフォルトに従う） |
| hibernationIdleMinutes | int | アイドル分数（0 = グローバルに従う） |
| hibernationActive | bool | 現在休眠状態かどうか（デーモンのランタイム状態） |

### InstanceInfo

| フィールド | 型 | 説明 |
|-----------|-----|------|
| config | InstanceConfig | インスタンス設定 |
| status | string | `stopped` / `starting` / `running` / `stopping` / `crashed` / `hibernating` |
| pid | int | プロセス PID（0 = 未実行） |
| exitCode | int | 最後の終了コード |

### DockerImage

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | string | イメージ ID（sha256:...） |
| repository | string | リポジトリ名（例: `itzg/minecraft-server`） |
| tag | string | タグ（例: `latest`） |
| size | int64 | サイズ（バイト） |
| created | int64 | 作成時刻（Unix 秒） |
| displayName | string | 表示名（優先順位: 管理者エイリアス > taps.displayName ラベル > OCI タイトル） |
| description | string | 説明（優先順位: taps.description ラベル > OCI description） |

### DockerStatsResp

| フィールド | 型 | 説明 |
|-----------|-----|------|
| name | string | コンテナ名（`taps-<uuid>`） |
| running | bool | 実行中かどうか |
| cpuPercent | float64 | CPU 使用率 % |
| memBytes | int64 | メモリ使用量（バイト） |
| memLimit | int64 | メモリ制限（バイト） |
| memPercent | float64 | メモリ使用率 % |
| netRxBytes | int64 | ネットワーク受信バイト数 |
| netTxBytes | int64 | ネットワーク送信バイト数 |
| blockReadBytes | int64 | ディスク読み取りバイト数 |
| blockWriteBytes | int64 | ディスク書き込みバイト数 |
| diskUsedBytes | int64 | マネージドボリューム使用バイト数（ボリュームなしの場合は 0） |
| diskTotalBytes | int64 | マネージドボリューム合計バイト数（ボリュームなしの場合は 0） |

### FsEntry

| フィールド | 型 | 説明 |
|-----------|-----|------|
| name | string | ファイル/ディレクトリ名 |
| isDir | bool | ディレクトリかどうか |
| size | int64 | ファイルサイズ（バイト、ディレクトリの場合は 0） |
| modified | int64 | 最終更新時刻（Unix 秒） |
| mode | string | パーミッション文字列（例: `drwxr-xr-x`） |

### BackupEntry

| フィールド | 型 | 説明 |
|-----------|-----|------|
| name | string | バックアップファイル名（タイムスタンプ付き .zip） |
| size | int64 | サイズ（バイト） |
| created | int64 | 作成時刻（Unix 秒） |
| instanceUuid | string | 所有インスタンスの UUID |

### McPlayersResp

| フィールド | 型 | 説明 |
|-----------|-----|------|
| online | bool | SLP に到達可能かどうか |
| count | int | オンラインプレイヤー数 |
| max | int | 最大プレイヤー数 |
| players | McPlayer[] | プレイヤーリスト `[{name, uuid}]` |
| version | string | サーバーバージョン |
| description | string | MOTD |

### PlayersBrief（ダッシュボード一括プレイヤー概要）

| フィールド | 型 | 説明 |
|-----------|-----|------|
| uuid | string | インスタンス UUID |
| online | bool | SLP に到達可能かどうか |
| count | int | オンラインプレイヤー数 |
| max | int | 最大プレイヤー数 |

### ProcessSnapshot

| フィールド | 型 | 説明 |
|-----------|-----|------|
| uuid | string | インスタンス UUID |
| pid | int | プロセス PID |
| running | bool | 実行中かどうか |
| cpuPercent | float64 | CPU % |
| memBytes | uint64 | メモリ（バイト） |
| numThreads | int32 | スレッド数 |
| timestamp | int64 | サンプル時刻（Unix 秒） |

### Volume

| フィールド | 型 | 説明 |
|-----------|-----|------|
| name | string | ボリューム名 |
| sizeBytes | int64 | 合計サイズ |
| usedBytes | int64 | 使用サイズ |
| mounted | bool | マウントされているかどうか |
| imagePath | string | .img ファイルパス |
| mountPath | string | マウントポイントパス |

---

### データベースモデル

#### User

| フィールド | 型 | JSON | 説明 |
|-----------|-----|------|------|
| id | uint | `id` | 主キー |
| username | string | `username` | 一意（大文字小文字を区別しない） |
| role | string | `role` | `admin` / `user` |
| email | string | `email` | 任意。LOWER ユニークインデックス（空でない場合） |
| mustChangePassword | bool | `mustChangePassword` | 初回ログイン時にパスワード変更を強制 |
| hasPassword | bool | `hasPassword` | false = SSO 経由で自動作成されたアカウント |
| createdAt | time | `createdAt` | |

> passwordHash / tokensInvalidBefore は外部に公開されません。

#### Daemon

| フィールド | 型 | JSON | 説明 |
|-----------|-----|------|------|
| id | uint | `id` | 主キー |
| name | string | `name` | 表示名 |
| address | string | `address` | host:port（パネルがデーモンに接続するために使用） |
| displayHost | string | `displayHost` | プレイヤー接続用の公開アドレス |
| portMin / portMax | int | `portMin` / `portMax` | 自動割り当てポート範囲（デフォルト 25565-25600） |
| certFingerprint | string | `certFingerprint` | SHA-256 フィンガープリント（コロン区切りの 16 進数） |
| connected | bool | runtime | デーモンがオンラインかどうか（DB フィールドではない） |

> token は外部に公開されません。

#### InstancePermission

| フィールド | 型 | 説明 |
|-----------|-----|------|
| userId | uint | ユーザー ID |
| daemonId | uint | ノード ID |
| uuid | string | インスタンス UUID |
| perms | uint32 | 権限ビットマスク: 1=View 2=Control 4=Files 8=Terminal 16=Manage |

#### Task（スケジュールタスク）

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | uint | 主キー |
| daemonId | uint | ノード ID |
| uuid | string | インスタンス UUID |
| name | string | タスク名 |
| cron | string | cron 式（5 フィールド） |
| action | string | `command` / `start` / `stop` / `restart` / `backup` |
| data | string | action=command の場合に stdin に送信されるテキスト |
| enabled | bool | 有効かどうか |

#### APIKey

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | uint | 主キー |
| userId | uint | 所有ユーザー |
| name | string | 名前 |
| prefix | string | 先頭 8 文字（表示用） |
| ipWhitelist | string | 許可 IP（CSV、空 = すべて） |
| scopes | string | 許可スコープ（CSV、空 = すべて） |
| expiresAt | time\|null | 有効期限（null = 無期限） |
| revokedAt | time\|null | 失効時刻（null = 未失効） |

> 完全なキー（`tps_...`）は作成時にのみ返され、以降は取得できません。

#### AuditLog

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | uint | 主キー |
| time | time | 操作時刻 |
| userId | uint | 操作ユーザー |
| username | string | ユーザー名 |
| method | string | HTTP メソッド |
| path | string | リクエストパス |
| status | int | HTTP ステータスコード |
| ip | string | クライアント IP |
| durationMs | int64 | 処理時間（ミリ秒） |

#### LoginLog

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | uint | 主キー |
| time | time | ログイン時刻 |
| username | string | 試行されたユーザー名 |
| userId | uint | ユーザー ID（0 = 存在しないユーザー） |
| success | bool | 成功したかどうか |
| reason | string | 失敗理由（成功時: CAPTCHA バイパス情報または `sso:<name>`） |
| ip | string | クライアント IP |
| userAgent | string | User-Agent |

#### SSOProvider

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | uint | 主キー |
| name | string | URL スラッグ（`[a-z0-9_-]{1,64}`） |
| displayName | string | UI 表示名 |
| enabled | bool | 有効かどうか |
| issuer | string | OIDC 発行者 URL |
| clientId | string | OAuth クライアント ID |
| hasSecret | bool | シークレットが保存されているか（GET で返される。シークレット自体はエコーされない） |
| scopes | string | リクエストスコープ（デフォルト `openid profile email`） |
| autoCreate | bool | ローカルユーザーを自動作成するか |
| defaultRole | string | 自動作成ユーザーのロール（`user` / `admin`） |
| emailDomains | string | 許可メールドメイン（CSV、空 = 制限なし） |
| trustUnverifiedEmail | bool | 未検証メールを信頼するか |
| callbackUrl | string | 計算されたコールバック URL（読み取り専用） |

#### DockerImageAlias

| フィールド | 型 | 説明 |
|-----------|-----|------|
| id | uint | 主キー |
| daemonId | uint | ノード ID |
| imageRef | string | イメージリファレンス（`repository:tag`） |
| displayName | string | 管理者が設定した表示名 |
