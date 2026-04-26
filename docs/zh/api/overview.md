# API 概览

Panel 暴露 RESTful + WebSocket 两类接口，全部前缀 `/api/`。

**基地址（生产示例）**：`https://taps.example.com`  
**默认端口**：24444（HTTP，可改 / 可上 nginx）

## 鉴权

三种凭据，挑一种用：

### 1. JWT Bearer Token

登录后拿到，放 `Authorization` 头：

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

- HS256，secret 在 `data/jwt.secret`（首次启动自动生成）
- 默认 1 小时（系统设置可改 5–1440 分钟）
- 滑动续期：剩余 < TTL/2 时响应头 `X-Refreshed-Token` 携带新 JWT
- 改密 / 改 Role / 删除用户后，旧 JWT **立即失效**（HTTP 401 `auth.token_revoked`）
- `alg: none` 攻击被显式拒绝

### 2. JWT in Query

仅用于浏览器无法设置 header 的场景（`<a href>` 下载、表单上传、WebSocket）：

```
GET /api/daemons/1/files/download?token=<jwt>&path=/data/x.txt
```

行为与 Bearer 相同，含 `tokens_invalid_before` 撤销校验。

### 3. API Key

`tps_` 前缀的固定凭据，用 Bearer 头：

```http
Authorization: Bearer tps_3fe3c349dd703a4c...
```

- 永久或带过期；可被撤销
- 可限 IP 白名单 + Scope
- 详见 [API Key](../usage/api-keys.md)

## 错误格式

所有错误统一返回 JSON，使用**稳定错误码**（`domain.snake_case` 格式）：

```json
{ "error": "auth.invalid_credentials", "message": "invalid credentials" }
```

部分错误带参数：

```json
{ "error": "auth.rate_limited", "message": "...", "params": { "retryAfter": 298 } }
```

```json
{ "error": "common.request_too_large", "message": "...", "params": { "maxBytes": 131072 } }
```

错误码可直接用于前端 i18n 查表：`t('errors.' + error)`。

### 常见状态码

| Code | 含义 |
|------|------|
| 200 | 成功 |
| 400 | 请求格式错 / 参数校验失败 |
| 401 | 凭据缺失 / 无效 / 撤销 / 过期 |
| 403 | 已认证但无权限（角色 / scope / 实例 perm 不够） |
| 404 | 资源不存在 |
| 405 | 方法不允许（JSON body `common.method_not_allowed`） |
| 409 | 冲突（重复用户名/邮箱、上传路径占用等） |
| 410 | upload session 过期 |
| 413 | 请求体超限 |
| 429 | 速率限制；响应头带 `Retry-After: <秒>` |
| 502 | Daemon 不可达 / Daemon 上游错 |

## 速率限制

| Bucket | 默认阈值 | 默认封禁 | 可配位置 |
|--------|---------|---------|---------|
| 登录失败 | 5/min/IP | 5 分钟 | 系统设置 → 速率限制 |
| 改密失败 | 同上 | 同上 | 同上 |
| API Key 失败 | 同上 | 同上 | 同上 |
| OAuth Start | 30/5min/IP | 5 分钟 | 同上 |
| Daemon Token 失败 | 10/min/IP | 10 分钟 | daemon config |

每次失败额外 sleep（指数退避，最多 3 秒）。成功认证清空该 IP 的失败计数。

## 请求体大小

| 端点 | 上限 | 配置项 |
|------|------|--------|
| 全局（除豁免外） | 128 KiB | 系统设置 → 请求大小上限 |
| `POST /daemons/:id/fs/write` | 16 MiB | 同上 maxJsonBodyBytes |
| `POST /daemons/:id/files/upload` 单分片 | 1 GiB | daemon 硬限 |
| `POST /settings/brand/favicon` | 64 KiB | 硬编码 |
| `POST /settings/hibernation/icon` | 32 KiB | 硬编码 |

WebSocket 单帧 ≤ 16 MiB（panel 系统设置 / daemon config 各自控制）。

## CORS

- 允许的源：系统设置 → 跨源访问白名单 配置的域名列表 + Panel 自身 publicUrl
- 允许的请求头：`Origin, Content-Type, Authorization`
- 允许的方法：`GET, POST, PUT, DELETE, OPTIONS`
- **暴露的响应头**：`X-Refreshed-Token, Retry-After, Content-Disposition`
- 开发时 `TAPS_CORS_DEV=1` 可临时开放 wildcard

## 安全 Header

每个响应自动携带：

| Header | 值 |
|--------|-----|
| Content-Security-Policy | `default-src 'self'; script-src 'self' + 可配白名单; ...` |
| X-Frame-Options | `SAMEORIGIN` |
| X-Content-Type-Options | `nosniff` |
| Referrer-Policy | `strict-origin-when-cross-origin` |
| Strict-Transport-Security | 仅 HTTPS 时发 |

CSP 的 script-src / frame-src 可在系统设置 → 内容安全策略（CSP）中热配置。

## TLS

- **Panel**：默认 HTTP；提供 `TAPS_TLS_CERT` + `TAPS_TLS_KEY` 走 HTTPS；推荐用 nginx 反代
- **Daemon**：强制 HTTPS（自签 99 年 ECDSA 证书，Panel 端按 SHA-256 指纹 pin）

## WebSocket 端点

| 路径 | 用途 | 鉴权 |
|------|------|------|
| `GET /api/ws/instance/:id/:uuid/terminal` | 实时终端 | `?token=<jwt>` + PermView（只读）/ PermTerminal（读写） |

- Origin 校验：必须匹配 Panel 公开地址（未配则拒绝）
- 读超时 + pong handler：可配（默认 60s）
- 输入 token bucket：可配（默认 200/s burst 50）

## 路径参数约定

- `:id` = 节点 ID（uint）
- `:uuid` = 实例 UUID（8-4-4-4-12 hex）
- `:taskId` = 计划任务 ID（uint）
- `:ref` = Docker 镜像引用（repository:tag，URL 编码）

## 时间格式

所有时间用 RFC 3339：`2026-04-23T18:55:07.020890690-04:00`。
