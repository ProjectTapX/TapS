# 故障排查

## Panel 起不来

```bash
systemctl status taps-panel
journalctl -u taps-panel -n 50 --no-pager
```

| 症状 | 原因 | 解决 |
|---|---|---|
| `bind: address already in use` | 端口被占 | `ss -lntp \| grep 24444` 找占用进程；改 panel 端口（系统设置或 env） |
| `database is locked` | 另一个 panel 进程在跑 / SQLite 文件锁残留 | `ps aux \| grep panel` 杀掉残留；删掉 `panel.db-shm` `panel.db-wal` |
| `jwt.secret: permission denied` | 文件权限错 | `chown root:root /var/lib/taps/panel/jwt.secret && chmod 600` |
| `panel listening on :2444`（端口少一位） | DB 里 `system.panelPort` 被错改 | `sqlite3 panel.db "UPDATE settings SET value='24444' WHERE key='system.panelPort'"` |

## Daemon 起不来

| 症状 | 原因 | 解决 |
|---|---|---|
| `tls cert: ...` | cert/key 文件损坏 | 删掉 `cert.pem` `key.pem` 重启；之后 Panel re-probe 指纹 |
| `docker daemon not running` | Docker 没启 | `systemctl start docker` |
| `bind: permission denied` | 非 root 跑 + 端口 < 1024 | systemd 单元 `User=root` |

## Panel 显示节点「离线」

```bash
# 在 Panel 主机上
nc -zv <daemon-host> 24445       # 端口通吗？
journalctl -u taps-panel -n 50 | grep daemon
```

| 错误 | 原因 | 解决 |
|---|---|---|
| `tls handshake: tls: failed to verify certificate` | TOFU 时存的指纹 ≠ daemon 实际指纹 | Panel UI 节点编辑 → 抓取指纹 → 接受 → 保存 |
| `dial: ... connection refused` | daemon 没起 / 防火墙挡了 | 在 daemon 主机 `systemctl status taps-daemon` |
| `not pinned` | 节点行没有 certFingerprint | UI 编辑 → 抓取指纹 → 接受 |

## 实例无法启动

```bash
# 在 daemon 主机
docker ps -a | grep taps-
docker logs taps-<uuid>
```

常见：
- `OCI runtime create failed: ... mounts: ... no such file or directory` → 工作目录被删了；Panel UI 重新部署或修复目录
- `Address already in use` → 该实例的主机端口被另一个进程占；改实例配置换端口
- `pull access denied` → 镜像名错 / registry 不可达
- `EULA must be accepted` → 一次性，daemon 自动写 EULA=TRUE 到 itzg env 应该不出现；自定义 docker 实例需自己加

## 终端连不上

| 症状 | 排查 |
|---|---|
| WebSocket 连接立刻 401 | `?token=` 失效（被吊销 / 改密了）→ 重登 |
| 前端打开终端 spinner 转圈不动 | nginx 没传 `Upgrade` 头；检查 [nginx 配置](../deployment/nginx-https.md) |
| 终端连上 5 分钟后自动断 | 你的 token 被 admin 撤销了；重登 |

## 上传失败

| 错误码 | 原因 |
|---|---|
| 413 `request_too_large` | `init` 接口被全局 128 KiB 限制拦了？检查 nginx `client_max_body_size` 是否 ≥ 1100M |
| 507 `quota_exceeded` | 文件 + 已用 > 卷剩余空间；扩卷或清理 |
| 410 unknown or expired uploadId | 单分片上传超过 1 小时未 final；客户端重试整个上传 |
| 400 missing uploadId | 客户端没先调 `/upload/init`；前端版本太旧，刷新页面 |
| 400 path does not match init declaration | upload session 的 path 与 chunk 的 path 字段不一致 |

## 限频 429

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 298
{"error":"rate_limited","retryAfter":298}
```

- 来自登录/改密/API key 失败累计达阈值（默认 5/min）
- 等 Retry-After 秒
- 紧急情况：`systemctl restart taps-panel` 清空所有内存计数

## 反代后所有客户端 IP 都是 127.0.0.1

可能原因（按排查优先级）：

1. **nginx 没传真实 IP 头**：检查 nginx 站点配置里是否有以下三行：
   ```nginx
   proxy_set_header X-Real-IP         $remote_addr;
   proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
   proxy_set_header X-Forwarded-Proto $scheme;
   ```
   缺少任一行都会导致 Panel 拿不到真实客户端 IP。

2. **Panel 没配信任列表**：即使 nginx 传了 `X-Forwarded-For`，gin 默认不信任该 header。进「系统设置」→「反向代理信任列表」→ 添加 nginx 主机 IP（同机默认 `127.0.0.1, ::1` 已覆盖）→ 保存。

3. **没重启 Panel**：`SetTrustedProxies` 只在启动时生效。保存后必须 `systemctl restart taps-panel`。

4. **nginx 在远程主机**：信任列表只写了 `127.0.0.1, ::1`，但 nginx 跑在另一台机器（如 `10.0.0.5`）→ 把 nginx 的 IP 加进去。

**验证方法**：访问 Panel 后看「登录日志」里 IP 列——应该是你的公网 IP 而非 `127.0.0.1`。

## SQLite 体积膨胀

```bash
ls -lh /var/lib/taps/panel/panel.db
sqlite3 /var/lib/taps/panel/panel.db "VACUUM;"
```

- 检查日志限制：「系统设置」→「日志容量上限」是不是设太大
- 检查 audit_logs / login_logs 表行数：`sqlite3 panel.db "SELECT COUNT(*) FROM audit_logs"`

## "Token revoked" 不停地出现

可能：admin 频繁改 Role / 改密 → 每次都 bump `tokens_invalid_before` → 之前签发的所有 JWT 一刀切失效。

正常行为，无需处理；让用户重登一次就好。

## 自动休眠玩家进不去

- 玩家在客户端连接 → 应该看到"服务器正在启动中, 请约 30 秒后重新连接"踢出消息
- daemon 后台启动真实容器（看 `journalctl -u taps-daemon -n 30`）
- 等 `warmupMinutes` 到 → 玩家重连 → 进得去

如果一直进不去：
- 看实例日志：是不是没起来 / EULA / 端口被占
- 看 daemon 日志：hibernation manager 是否报错

## 日志在哪

```bash
# Panel
journalctl -u taps-panel -f
journalctl -u taps-panel -n 200 --no-pager

# Daemon
journalctl -u taps-daemon -f

# 实例容器
docker logs -f taps-<uuid>

# 审计 / 登录日志（要登录看）
浏览器 → Panel → 用户管理 → 审计日志 / 登录日志
```

## 数据库被锁

如果 panel 进程 panic 退出可能留下 `panel.db-shm` `panel.db-wal`：

```bash
systemctl stop taps-panel
sqlite3 /var/lib/taps/panel/panel.db ".quit"   # 整理 WAL
systemctl start taps-panel
```

如果还卡：检查是不是有人手工开了 `sqlite3` shell 没退出。

## 找不到的问题

打开 panel + daemon 的 `journalctl -f`，复现问题，把日志贴给开发 / issue。

## CORS

监控工具 / 健康检查 / 第三方仪表盘对 `/api/*` 请求收到 **403 Forbidden** 而不是预期的数据，多半是 CORS 拦下了。

**症状**：
- 浏览器 DevTools 里 `Access-Control-Allow-Origin: *` 没有了，请求被本地 SOP 拦
- curl 带 `-H 'Origin: https://yourtool.example.com'` 请求得到 `HTTP/1.1 403`
- panel 日志看 GIN 那行响应码是 403

**原因**：CORS 默认只允许"系统设置 → 跨源访问白名单"里列出的源。空白名单时只允许 Panel 公开地址（publicUrl）。任何带 `Origin` header 但源不在列表里的请求不会收到 ACAO 头，浏览器会拒绝。

**修复**（择一）：
1. **加白名单（推荐）**：浏览器登录 panel → 系统设置 → 跨源访问白名单（CORS）→ 把监控工具的源加进去（`https://prometheus.internal`、`https://uptime.example.com` 等）→ 保存。立即生效，无需重启
2. **临时开发模式**：systemd 单元里加 `Environment=TAPS_CORS_DEV=1`，重启 panel——这会重新放开成 wildcard。**仅用于开发联调，绝不要在生产开**
3. **绕过 Origin header**：如果监控工具能配置不发 Origin header（多数 cURL-based 工具默认不发），从根源上避免触发 CORS

**注意事项**：
- API key 的 server-to-server 调用如果客户端不发 Origin header，**完全不受 CORS 影响**——大多数自动化场景默认就是这样
- 浏览器内 JS 跨源访问 panel API 的场景（如把 panel 嵌 iframe、第三方 SPA 调 panel API）**必须**走白名单
- panel 自己的 SPA 跑在同一个 origin，发的请求 Origin 等于 publicURL → 始终允许，不需要额外配置
