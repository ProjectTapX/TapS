# 单点登录（SSO / OIDC）

TapS Panel 支持标准 **OpenID Connect (OIDC)** 登录。可对接 Google、Microsoft Entra ID、Keycloak、Casdoor、Logto，或任何符合 OIDC 规范、提供 `.well-known/openid-configuration` 的 IdP。

> 本文涵盖：管理员配置 IdP、用户绑定/解绑、登录方式切换、被锁死时的恢复方法。

---

## 1. 前置条件

1. **Panel 必须有外部可达地址**。OIDC 回调由 IdP 重定向回浏览器，浏览器再回到 Panel —— Panel 内网/`localhost` IdP 访问不到自己也没关系，但**浏览器必须能访问** Panel。
2. 在 IdP 处提前注册一个 OIDC Client，准备好：
   - **Issuer URL**（如 `https://accounts.google.com`、`https://login.microsoftonline.com/{tenantId}/v2.0`、自建 `https://sso.example.com/realms/master`）
   - **Client ID** + **Client Secret**

---

## 2. 配置步骤（管理员）

### 2.1 设置 Panel 公开地址

进入 **系统设置 → SSO → "Panel 公开地址"**，填入对外 URL（含协议），例如 `https://taps.example.com`。**未填写时所有 SSO 流程都会失败。**

回调 URL 自动等于：`<公开地址>/api/oauth/callback/<提供商 slug>`。

### 2.2 添加提供商

**系统设置 → SSO → 添加提供商**，选模板（Google / Microsoft Entra / Keycloak / Logto / Custom）。需要填的字段：

| 字段 | 说明 |
|---|---|
| 名称（slug） | URL 安全标识，**保存后不可改**。决定回调 URL 路径 |
| 显示名 | 登录页按钮上的文案 |
| Issuer | OIDC 发行方 URL，自动通过 `.well-known/openid-configuration` 发现端点 |
| Client ID / Secret | IdP 注册时拿到的凭据；Secret AES-256-GCM 加密落库 |
| Scopes | 默认 `openid profile email`，按需追加 |
| 允许的邮箱域名 | 留空 = 不限；多个用逗号分隔 |
| 自动创建账号 | 开 = IdP 邮箱与本地无匹配时新建本地账号；关 = 拒绝登录 |
| 默认角色 | 仅在"自动创建"开启时生效 |

填完点 **测试 Issuer** 验证可达，再保存。

### 2.3 在 IdP 端登记回调 URL

**模态框底部会显示**该提供商的回调 URL，把它复制到 IdP 控制台的"重定向 URI / Redirect URI"白名单里。

### 2.4 切换登录方式

**系统设置 → SSO → 登录方式**，三选一：

- `password-only` — 默认；登录页只有用户名/密码
- `oidc+password` — 同时支持，**推荐**
- `oidc-only` — 只允许 SSO；密码登录被拒绝

> **防锁死**：切换到 `oidc-only` 前必须满足"已有至少一个管理员绑定到一个启用的 SSO 提供商"，否则后端拒绝。原因显然——IdP 一旦坏掉，没人能登录。

---

## 3. 用户绑定 / 解绑

普通用户在 **右上角头像 → 账号设置** 看到自己已绑定的 SSO 列表。

- **绑定**：点击"绑定 X"，会按 SSO 登录流程跳转到 IdP，IdP 通过后回到 Panel 自动绑定到当前账号。
- **解绑**：在列表行点击"解绑"。`oidc-only` 模式下解绑最后一个绑定会被拒绝（避免把自己锁死）。
- **自动绑定**：用户首次通过 SSO 登录时，按以下顺序匹配：
  1. `(provider, subject)` 已存在 → 找到原账号
  2. IdP 返回邮箱与某本地账号 `email` 匹配 → 绑定到该账号
  3. 都不匹配 → 看"自动创建"是否开启

管理员可通过用户管理页操作任意用户的绑定（解绑被锁定的同事很有用）。

---

## 4. 锁死后如何恢复

如果不幸切到了 `oidc-only` 又把 IdP 玩坏了，登录页谁也进不去。在 Panel 主机的 shell 里执行：

```bash
# 切回密码登录（保留所有 SSO 配置，下次能直接切回 oidc+password）
./taps-panel reset-auth-method --to password-only
```

不需要重启 Panel 进程；下一次登录请求会读到新值。

---

## 5. 安全说明

- **State + PKCE + Nonce** 全部启用。State 通过 HMAC-SHA256 签名（provider + nonce + expiry，5 分钟 TTL）。**PKCE verifier 存在 Panel 进程内存**（不在 URL 中），10 分钟 TTL + maxEntries 上限防 DoS。
- **Client Secret** 用 AES-256-GCM 加密落库，密钥文件 `<dataDir>/secret-encryption.key`（首次启动自动生成；权限 0600）。**该文件丢失=所有 secret 失效**，请纳入备份。
- **Email 大小写规范化**：IdP 返回的 email 在入口即 `ToLower`，防止大小写变形绕过 admin auto-bind 拒绝。
- **回调错误码映射**：所有回调失败使用 `CallbackError{Code, Err}` typed wrapper，URL fragment 只传稳定 code（如 `sso.token_exchange_failed`），不泄漏 IdP 内部错误到浏览器。审计日志保留完整内部 error。
- **Token 透传**：Panel 在 OIDC 成功后签发自己的 TapS JWT，并通过 URL hash fragment（`#oauth-token=...`）传给 SPA，IdP 的 access/refresh token **不会**进入浏览器。

---

## 6. 常见问题

**Q：回调一直报 `redirect_uri_mismatch`？**
A：检查 IdP 控制台的"重定向 URI"是否**完全匹配**（含协议、端口、`/api/oauth/callback/<slug>`）。slug 不能改，所以一开始要选好。

**Q：能用同一个本地账号绑多个 IdP 吗？**
A：可以。账号设置页可以把多个 IdP 都绑到同一账号。

**Q：用户在 IdP 改了邮箱，会不会自动跟随？**
A：登录时会把最新邮箱写回 `sso_identities.email`，但**不会**改本地 `users.email`（避免与用户管理页冲突）。

**Q：删除一个 SSO 提供商，所有绑定也没了？**
A：是。删除会级联清理 `sso_identities` 中该 `provider_id` 的所有行；本地账号本身保留。
