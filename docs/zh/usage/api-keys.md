# API Key

API Key 是给机器（CI、脚本、外部集成）用的长期凭据，与人类账号的 JWT 走同一个鉴权中间件，但独立生命周期。

## 形态

- 前缀固定 `tps_`，后接 48 位 hex（24 字节随机）
- 总长度 52 字符
- 例：`tps_3fe3c349dd703a4c8b...`

## 签发

「**API Key**」页 → 「**新建 Key**」：

| 字段 | 说明 |
|---|---|
| 名称 | 仅展示用，例 `ci-deploy` / `monitoring-prober` |
| IP 白名单 | 逗号分隔的 IP 或 CIDR；空 = 任何 IP |
| Scope | 逗号分隔的权限标签，空 = 全权（继承用户 role）；详见下表 |
| 过期时间 | `永不` / 30/90/365 天 / 自定义日期 |

点「**确定**」→ 弹出**仅显示一次**的明文 key，点复制后自行妥善保存。关掉就**再也看不到**（DB 只存 SHA-256）。

## Scope（路由权限标签）

可选值（CSV）：

| Scope | 允许调用的接口组 |
|---|---|
| `instance.read` | 看实例 / 监控 / 玩家列表 / 节点 public 元数据 |
| `instance.control` | 启停 / 输入 / 创建 / 更新 / 部署模板 / serverdeploy |
| `files` | 文件管理 + 备份 |
| `tasks` | 计划任务 |
| `admin` | 用户、节点、设置、审计、API Key 管理 |

留空 = 不限 scope（**全权**，仅受角色限制）。

例：监控脚本只需要读 → `instance.read`；CI 自动重启脚本 → `instance.read,instance.control`。

## 使用

```bash
curl -H "Authorization: Bearer tps_3fe3c349dd703a4c..." \
     https://taps.example.com/api/instances
```

服务端会：
1. 识别 `tps_` 前缀走 API Key 路径
2. SHA-256 比对 → 找到行
3. 校验 IP 白名单（CIDR / 精确 IP）
4. 校验 `revoked_at` 为 NULL
5. 校验 `expires_at` > now（NULL 视为永不过期）
6. 校验请求 scope 命中
7. 失败累计 5 次/分钟 → 封该 IP 5 分钟（429 + Retry-After）

## 撤销 vs 删除

每行 key 在列表里有两个按钮：

- **撤销**（默认色）：把 `revoked_at` 设为现在；行保留以便审计；凭据立即失效
- **删除**（红色）：物理删除该行；不可恢复

「**撤销我的全部 Key**」按钮：把当前用户名下**所有未撤销**的 key 一次性 set `revoked_at = now`，常用于"怀疑 key 泄露"的应急。

## 过期与轮换

- 创建时可选 30 / 90 / 365 天 / 自定义日期 / 永不
- 过期后调用立即返回 `401 invalid api key: api key expired`
- **过期不会自动删除行**——保留作审计；admin / 用户可手动 delete 清理
- 推荐 CI 凭据走 90 天周期，到期前换一把新 key 部署，旧 key 自然过期

## 与 JWT 的差异

| 维度 | JWT | API Key |
|---|---|---|
| 颁发 | 登录时签发 | 用户主动创建 |
| 撤销机制 | bump `tokens_invalid_before`（全 token 一刀切）| 每 key `revoked_at` 字段（精细） |
| 默认有效期 | 1 小时（设置可改） | 永久（创建时可选过期） |
| 滑动续期 | ✅（请求剩余 < 半 TTL 自动新签） | ❌（key 是固定凭据） |
| Scope | ❌（沿用用户 role） | ✅（CSV 限制路由组） |
| IP 白名单 | ❌ | ✅ |
| 适合场景 | 浏览器交互 | CI / 脚本 / 监控 |

## 安全建议

- 给每条 CI 任务**独立 key**，不共享
- 配 IP 白名单（CI runner 出口 IP）
- 配最小 scope（只读监控就只给 `instance.read`）
- 定期轮换（90 天）
- 怀疑泄露立即按「**撤销我的全部 Key**」+ 改密
- **不要**把 key 写到代码里、commit 到 git；用 GitHub Actions Secret / GitLab CI Variable 等
