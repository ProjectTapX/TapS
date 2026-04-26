# 系统设置详解

「**系统设置**」页面（仅 admin 可见）。所有设置项持久化在 SQLite `settings` 表（key/value 文本）。

## 卡片排序与设置项一览

页面从上到下：

| # | 卡片 | 关键设置项 | 默认值 | 生效方式 |
|---|------|----------|--------|---------|
| 1 | **站点品牌** | siteName | `TapS` | 立即 |
| | | favicon (PNG/ICO) | 无 | 立即 |
| 2 | **Panel 公开地址** | publicUrl | 空 | 立即 |
| 3 | **Panel 监听端口** | port | 24444 | **需重启** |
| 4 | **反向代理信任列表** | proxies | `127.0.0.1, ::1` | **需重启** |
| 5 | **跨源访问白名单（CORS）** | origins | 空 | 立即 |
| 6 | **登录验证码** | provider / siteKey / secret / scoreThreshold | `none` / 空 / 加密 / 0.5 | 立即 |
| 7 | **登录方式** | method | `password-only` | 立即 |
| 8 | **SSO 提供商（OIDC）** | provider list | — | 立即 |
| 9 | **服务端下载源** | source | `fastmirror` | 立即 |
| 10 | **Minecraft Java 服务器自动休眠** | defaultEnabled / minutes / warmup / motd / kick / icon | true / 60 / 5 | 立即 |
| 11 | **Webhook 通知** | url / allowPrivate | 空 / false | 立即 |
| 12 | **日志容量上限** | auditMaxRows / loginMaxRows | 1000000 | 立即 |
| 13 | **速率限制** | rateLimitPerMin / banDurationMinutes | 5 / 5 | 实时 |
| | | oauthStartCount / oauthStartWindowSec | 30 / 300 | 实时 |
| | | pkceStoreMaxEntries | 10000 | 实时 |
| | | terminalReadDeadlineSec / inputRatePerSec / inputBurst | 60 / 200 / 50 | 新 WS 生效 |
| | | iconCacheMaxAgeSec / iconRatePerMin | 300 / 10 | 立即 |
| 14 | **请求大小上限** | maxRequestBodyBytes / maxJsonBodyBytes / maxWsFrameBytes | 128 KiB / 16 MiB / 16 MiB | 实时 |
| 15 | **内容安全策略（CSP）** | scriptSrcExtra / frameSrcExtra | Cloudflare + reCAPTCHA CDN | 立即 |
| 16 | **会话有效期** | jwtTtlMinutes / wsHeartbeatMinutes | 60 / 5 | 新会话生效 |
| 17 | **HTTP 超时（防 slow-loris）** | readHeaderTimeoutSec / readTimeoutSec / writeTimeoutSec / idleTimeoutSec | 10 / 60 / 120 / 120 | **需重启** |

---

## 详解

### 站点品牌

- **siteName**：浏览器标题、登录页 hero 区显示的名称。字符白名单：字母、数字、中日韩字符、常见标点。CJK 字符按 2 计权，上限 16 权重（即最多 16 个 ASCII 字符 或 8 个汉字）。
  - 见 `panel/internal/api/settings.go validSiteName()`
- **favicon**：上传 PNG / ICO ≤ 64 KiB。**SVG 不允许**（已禁用，防止 stored XSS）。服务端始终用 `http.DetectContentType` 嗅探真实类型，不信任客户端 Content-Type。
  - 见 `panel/internal/api/settings.go SetBrandFavicon()`

### Panel 公开地址

Panel 的对外访问 URL（含协议），如 `https://taps.example.com`。多处功能依赖它：

1. **SSO/OIDC 回调地址**：`<publicUrl>/api/oauth/callback/<provider>`
2. **终端 WebSocket 同源校验**：未填则拒绝打开终端会话
3. **CORS 允许源回退**：CORS 白名单为空时用 publicUrl 作同源比较

- 见 `panel/internal/api/panel_public_url.go`

### Panel 监听端口

写入 DB，重启 panel 进程后生效。优先级：DB > env (`TAPS_ADDR`) > 默认 24444。

### 反向代理信任列表

仅在 panel 部署于 nginx / Caddy / Cloudflare 后需要配。不配时 `c.ClientIP()` 恒返回 `127.0.0.1`，导致限频 / 审计 / API Key IP 白名单全部失效。改完**必须重启 panel**。
- 见 `panel/internal/api/trusted_proxies_settings.go`

### 跨源访问白名单（CORS）

逗号分隔的 origin 列表（`scheme://host[:port]`）。只有列在这里的域名的浏览器 JS 才能跨源调用 Panel API。留空时仅允许 Panel 自身的 publicUrl（同源 SPA 始终通过）。API Key 走 server-to-server 调用不受此限。立即生效。
- 见 `panel/internal/api/cors_settings.go`

### 登录验证码

仅作用于**登录接口**。

| provider | 说明 |
|----------|------|
| `none` | 关闭 |
| `turnstile` | Cloudflare Turnstile |
| `recaptcha` | Google reCAPTCHA Enterprise |

**关键行为**：
- **Fail-open**：密钥级别错误（secret 错、site key 不匹配）→ `ErrConfig` → 本次登录放行避免锁死；网络错/5xx → fail-closed 拒绝登录
- **Secret 加密存储**：captcha secret 用 AES-GCM 加密在 `captcha.secretEnc` 列（审计 N3）。admin GET 返回 `hasSecret: true/false`，**不回显 secret 明文**
- **切换 provider 时强制重设 secret**：从 Turnstile 切到 reCAPTCHA（或反过来）时，后端拒绝空 secret 的 PUT，前端自动清空 siteKey + secret 输入框（审计 H1）
- **scoreThreshold 0 允许**：`*float64` 指针型，nil = 沿用旧值，0 = 禁用阈值（所有 reCAPTCHA token 通过），0.1-0.9 正常阈值（审计 MED8）

### 登录方式

| 值 | 说明 |
|----|------|
| `password-only` | 仅密码登录（即使 SSO provider 已配，登录页不显示 SSO 按钮） |
| `oidc+password` | 密码 + SSO 均可 |
| `oidc-only` | 仅 SSO（密码入口禁用；至少需要一个启用的 provider + 至少一个 admin 已绑定） |

恢复方法（admin 被 oidc-only 锁死时）：
```bash
taps-panel reset-auth-method --to password-only --data-dir /var/lib/taps/panel
```

### SSO 提供商（OIDC）

详见 [SSO / OIDC 文档](sso-oidc.md)。

### 服务端下载源

| 值 | 说明 |
|----|------|
| `fastmirror` | FastMirror 镜像（中国友好） |
| `official` | Mojang / PaperMC 官方源（需面板可直连海外） |

### Minecraft Java 服务器自动休眠

详见 [实例管理 → 休眠](instances.md)。

### Webhook 通知

监控 **Daemon 节点**（非实例）连通性。当 Daemon 与 Panel 断开**持续超过 60 秒**时发 `node.offline`；重连时补 `node.online`（仅当之前已发过 offline）。

```json
{ "event": "node.offline", "timestamp": 1714000000, "payload": { "daemonId": 1, "name": "node-a", "address": "10.0.0.5:24445" } }
```

- **SSRF 防护**：ClassifyHost 三分法（public / private / DNS-failed）+ DialContext 重检。admin 可勾选"允许私有/回环地址"放行内网 webhook
- **allowPrivate**：仅当 webhook 接收端确实在内网且可信时才开

### 日志容量上限

`loglimit.Manager` 每 60 秒检查 audit_logs / login_logs 行数，超出删最旧。

### 速率限制

> 卡片名已从"登录速率限制"改为"速率限制"（2026-04-26），因内容覆盖面更广。

**认证限速**（3 个独立 bucket 共享阈值）：
| 设置 | 默认 | 范围 | 说明 |
|------|------|------|------|
| rateLimitPerMin | 5 | 1-100 | 同 IP 每分钟允许失败次数（login / changePw / apiKey 各自独立计数） |
| banDurationMinutes | 5 | 1-1440 | 超过阈值后封禁时长 |

**OAuth 启动限速**（匿名端点防 PKCE store 灌满）：
| 设置 | 默认 | 范围 |
|------|------|------|
| oauthStartCount | 30 | 1-1000 |
| oauthStartWindowSec | 300 | 30-3600 |
| pkceStoreMaxEntries | 10000 | 100-1000000 |

**终端 WebSocket**（每连接 token bucket）：
| 设置 | 默认 | 范围 | 说明 |
|------|------|------|------|
| terminalReadDeadlineSec | 60 | 10-600 | 两帧间最大空闲时间（含 pong） |
| terminalInputRatePerSec | 200 | 1-5000 | 每秒允许的输入帧数 |
| terminalInputBurst | 50 | 1-5000 | 瞬时连发预算（粘贴命令用） |

**休眠图标公开端点**：
| 设置 | 默认 | 范围 | 说明 |
|------|------|------|------|
| iconCacheMaxAgeSec | 300 | 0-86400 | Cache-Control max-age |
| iconRatePerMin | 10 | 1-1000 | 同 IP 每分钟请求上限 |

### 请求大小上限

| 设置 | 默认 | 范围 | 说明 |
|------|------|------|------|
| maxRequestBodyBytes | 128 KiB | 1 KiB - 4 MiB | 全局请求体上限（Content-Length 先检查，超过直接 413） |
| maxJsonBodyBytes | 16 MiB | 1-128 MiB | fs/write 等大 JSON 端点 |
| maxWsFrameBytes | 16 MiB | 1-128 MiB | Panel 终端 WS 帧上限 |

豁免全局上限的路径：`*/fs/write`、`*/files/upload*`、`*/brand/favicon`、`*/hibernation/icon`。

### 内容安全策略（CSP）

Content-Security-Policy 告诉浏览器只允许从哪些域加载脚本和嵌入 iframe。`'self'` 始终包含且不可删除。

| 设置 | 默认 | 说明 |
|------|------|------|
| scriptSrcExtra | `https://challenges.cloudflare.com, https://www.recaptcha.net` | 允许加载脚本的外部域 |
| frameSrcExtra | `https://challenges.cloudflare.com, https://www.google.com, https://www.recaptcha.net` | 允许嵌入 iframe 的外部域 |

完整生成的 CSP 头：
```
default-src 'self'; script-src 'self' <scriptSrcExtra...>; style-src 'self' 'unsafe-inline'; frame-src 'self' <frameSrcExtra...>; img-src 'self' data:; connect-src 'self' ws: wss:; font-src 'self'
```

- `style-src 'unsafe-inline'`：antd CSS-in-JS 运行时注入 `<style>` 标签需要
- `connect-src ws: wss:`：终端 WebSocket 连接需要

**其他安全 header**（自动发、不可配）：
- `X-Frame-Options: SAMEORIGIN`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Strict-Transport-Security`：仅当 panel 自身配了 TLS cert、或请求头含 `X-Forwarded-Proto: https`（nginx 反代）时发

见 `panel/internal/api/security_headers.go`

### 会话有效期

| 设置 | 默认 | 范围 | 说明 |
|------|------|------|------|
| jwtTtlMinutes | 60 | 5-1440 | JWT 有效期；剩余 < TTL/2 时自动续期 |
| wsHeartbeatMinutes | 5 | 1-60 | 终端 WS 重新校验 TokensInvalidBefore 间隔 |

### HTTP 超时（防 slow-loris）

四个 `http.Server` 超时参数。WebSocket 在 Hijack 后不受这些超时约束。改完**需重启 panel**。

| 设置 | 默认 | 范围 | 说明 |
|------|------|------|------|
| readHeaderTimeoutSec | 10 | 1-3600 | 连接到 header 读完的总时间 |
| readTimeoutSec | 60 | 1-3600 | 含 body 的总读取时间 |
| writeTimeoutSec | 120 | 1-3600 | header 读完到 response 写完 |
| idleTimeoutSec | 120 | 1-3600 | keep-alive 空闲保持时间 |

---

## 不在 UI 里的配置

走环境变量或 daemon `config.json`：

### Panel 环境变量

| 变量 | 说明 | 默认 |
|------|------|------|
| `TAPS_DATA_DIR` | panel 数据目录 | `./data` |
| `TAPS_WEB_DIR` | web 静态目录 | `./web` |
| `TAPS_ADDR` | 监听 host:port（会被 DB 端口覆盖） | `:24444` |
| `TAPS_ADMIN_USER` / `TAPS_ADMIN_PASS` | 仅首次 seed 用 | `admin` / `admin` |
| `TAPS_TLS_CERT` / `TAPS_TLS_KEY` | 启用 HTTPS（不走 nginx 时） | — |
| `TAPS_CORS_DEV` | `=1` 开放 CORS wildcard（开发用） | — |

### Daemon 环境变量 / config.json

所有 env 均可被 `<DataDir>/config.json` 覆盖（JSON 优先级 > env > 默认值）。

| 变量 / JSON key | 说明 | 默认 | 范围 |
|------|------|------|------|
| `TAPS_DAEMON_DATA` | daemon 数据目录 | `./data` | — |
| `TAPS_DAEMON_ADDR` / `addr` | 监听 host:port | `:24445` | — |
| `TAPS_REQUIRE_DOCKER` / `requireDocker` | 拒绝非 docker 实例 | `true` | bool |
| `TAPS_DAEMON_RL_THRESHOLD` / `rateLimitThreshold` | token 校验失败阈值 | 10 | 1-1000 |
| `TAPS_DAEMON_RL_BAN_MINUTES` / `rateLimitBanMinutes` | 封禁时长 | 10 | 1-1440 |
| `TAPS_DAEMON_MAX_WS_FRAME_BYTES` / `maxWsFrameBytes` | WS 帧上限 | 16 MiB | 1-128 MiB |
| `TAPS_DAEMON_WS_DISPATCH_CONCURRENCY` / `wsDispatchConcurrency` | 每 session 并发 dispatch 上限 | 8192 | 1-65536 |
| `TAPS_DAEMON_HTTP_READ_HEADER_TIMEOUT_SEC` / `httpReadHeaderTimeoutSec` | HTTP 读 header 超时 | 10 | 1-3600 |
| `TAPS_DAEMON_HTTP_READ_TIMEOUT_SEC` / `httpReadTimeoutSec` | HTTP 读 body 超时 | 60 | 1-3600 |
| `TAPS_DAEMON_HTTP_WRITE_TIMEOUT_SEC` / `httpWriteTimeoutSec` | HTTP 写超时 | 120 | 1-3600 |
| `TAPS_DAEMON_HTTP_IDLE_TIMEOUT_SEC` / `httpIdleTimeoutSec` | HTTP 空闲超时 | 120 | 1-3600 |

Daemon 启动时自动写 `config.json.template` 到数据目录，包含所有支持的字段和默认值，供 admin 复制编辑。
