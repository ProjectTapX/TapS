# 安全架构

## 高层模型

```
浏览器 ──HTTPS──▶ [nginx/Caddy] ──HTTP──▶ Panel (:24444)
                                           │  wss + TLS fingerprint pin
                                           ▼
                                        Daemon (:24445, self-signed TLS)
                                           │
                                        Docker Engine
```

Panel 是所有认证/授权决策的中心；Daemon 只信任 Panel 的共享 token。浏览器与 Panel 之间通过 JWT 认证；Panel 与 Daemon 之间通过 TLS + 共享 token 认证。

---

## 安全防御层

### HTTP 安全 Header

所有响应自动携带（见 `panel/internal/api/security_headers.go`）：

| Header | 值 | 用途 |
|--------|-----|------|
| Content-Security-Policy | `default-src 'self'; script-src 'self' + 可配白名单; ...` | 防止 XSS 注入外部脚本 |
| X-Frame-Options | `SAMEORIGIN` | 防 clickjacking |
| X-Content-Type-Options | `nosniff` | 防 MIME 嗅探攻击 |
| Referrer-Policy | `strict-origin-when-cross-origin` | 防 Referer 泄漏 |
| Strict-Transport-Security | `max-age=31536000; includeSubDomains`（仅 HTTPS） | 强制 HTTPS |

CSP 的 script-src / frame-src 白名单可在管理面板热配置（即时生效，无需重启）。

### 认证

| 措施 | 说明 |
|------|------|
| JWT HS256 | 随机 secret（`jwt.secret` 文件，首次启动生成） |
| bcrypt cost 10 | 密码哈希 |
| dummy-hash timing-equalize | 不存在的用户仍执行一次 bcrypt 比较，防时序攻击枚举用户名 |
| 滑动续期 | JWT 剩余时长 < TTL/2 时自动在 `X-Refreshed-Token` 响应头发新 token |
| Token 吊销 | `TokensInvalidBefore` 字段；密码修改 / admin 降级时设为当前 iat-1s |
| MustChangePassword | 首次登录强制改密 |
| `alg: none` 拒绝 | jwt-go ParseToken 显式拒绝 none 算法 |

### 授权

| 层 | 实现 |
|----|------|
| 角色 | admin / user，`auth.RequireRole()` 中间件 |
| 按实例权限 | PermView / PermControl / PermTerminal / PermFiles |
| API Key Scope | `RequireScope()` 中间件，逗号分隔的 scope 标签 |

### SSO / OIDC

| 措施 | 说明 |
|------|------|
| PKCE server-side store | 验证器不在 URL 中，存在 Panel 进程内存（10 分钟 TTL） |
| HMAC state | provider + nonce + expiry + HMAC-SHA256 签名 |
| Nonce binding | id_token.nonce 必须匹配 state 中的 nonce |
| Email ToLower | 入口即小写化，防大小写绕过 admin auto-bind guard |
| Admin auto-bind 拒绝 | 已有 admin 邮箱的本地账户不允许 IdP 自动绑定 |
| Email domain 白名单 | 每 provider 可配允许域名列表 |
| CallbackError typed codes | URL fragment 只传稳定 code，不泄漏 IdP 内部错误到浏览器 |
| clientSecret 加密存储 | AES-GCM at-rest |

### 输入校验

| 校验 | 位置 |
|------|------|
| ValidImage regex + `--` separator | docker CLI flag 注入防御 |
| validInstanceUUID | 所有 `taps-<uuid>` docker 命令前 |
| validBackupName regex | 备份文件名 |
| validSiteName 字符白名单 | 品牌名称 |
| normalizeEmail / normalizeUsername | 统一小写 + trim |
| LOWER() unique indexes | SQLite 唯一索引用 `lower()` 函数 |

### 路径安全（文件操作）

| 措施 | 说明 |
|------|------|
| fs.Resolve EvalSymlinks | 二次 symlink 解析 + containment check |
| containedIn 双根 | backup restore 目标必须在 instancesRoot 或 volumesRoot 下 |
| Zip/Copy symlink containment | EvalSymlinks → 在 mount 内则跟随，逃出则跳过 + log |
| O_NOFOLLOW | zip 解压 / backup 还原用 nofollow flag 打开文件 |
| isProtectedBackingFile | 拒绝对 `.img` / `.json` 卷 backing 文件的直接 fs 操作 |
| zip entry reject | 拒绝 symlink 条目 / leading `/` / `..` 段 |

### SSRF 防护

| 场景 | 措施 |
|------|------|
| Webhook URL | ClassifyHost 三分法 (public / private / DNS-failed) + DialContext 重检 |
| SSO Test | 同上 + SafeHTTPClient 防 DNS rebinding |

### 数据保护

| 措施 | 覆盖 |
|------|------|
| AES-GCM at-rest | captcha secret、SSO clientSecret |
| 独立密钥 | sso-state.key 独立于 jwt.secret |
| bcrypt | 用户密码 |
| crypto/rand | 所有随机数生成 |

### DoS 防御

| 措施 | 配置位置 |
|------|---------|
| per-IP 限速（login / changePw / apiKey） | 速率限制卡片 |
| oauth-start budget | 速率限制卡片 |
| PKCE store maxEntries | 速率限制卡片 |
| WS dispatch semaphore 8192 | daemon config |
| WS frame size cap | 请求大小上限 / daemon config |
| HTTP server timeouts | HTTP 超时卡片 / daemon config |
| request body cap | 请求大小上限卡片 |
| hib icon cache + rate limit | 速率限制卡片 |

### 事务一致性

以下多键 settings 写入全部包在 `db.Transaction` 内：
- SetCaptchaConfig、SetLimits、SetAuthTimings、SetRateLimit、SetHTTPTimeouts
- daemon.Delete（cascade InstancePermission / Task / NodeGroupMember）
- groups.Delete（cascade NodeGroupMember）
- User.Update / User.Delete（clause.Locking）

### 前端安全

| 措施 | 说明 |
|------|------|
| i18next escapeValue: true | 全局 HTML 转义 |
| CSP script-src 'self' | 限制可执行脚本源 |
| 921 i18n keys 对齐 | zh / en 完全一一对应 |
| 统一错误码 | 后端 apiErr(code, msg)，前端 formatApiError 自动查 i18n |
| partialize persist | zustand 只持久化 token + {id, username, role} |
| Terminal token re-read | 每次 WS 重连重读最新 token |
| waitFor timeout | captcha SDK 加载 5 秒超时 |
| ChunkErrorBoundary | getDerivedStateFromError 返回 null（不 throw） |

### 运维安全

| 措施 | 说明 |
|------|------|
| graceful shutdown | SIGTERM → srv.Shutdown(30s) → hib.Shutdown → vm.UnmountAll |
| systemd TimeoutStopSec=30s + KillSignal=SIGTERM | 配合 graceful shutdown |
| MountAll 同步 | daemon 启动时等所有 loopback mount 完成再接受请求 |

---

## 审计历史

截至 2026-04-26，共六轮人工/AI审计，累计修复 99 项。当前评级：**A**（0 Critical / 0 High / 0 Medium / 0 Low 开放漏洞）。
