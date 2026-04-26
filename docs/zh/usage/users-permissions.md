# 用户与权限

## 角色

| 角色 | 说明 |
|---|---|
| `admin` | 全权：所有用户 / 节点 / 实例 / 设置 |
| `user` | 普通用户；默认看不到任何实例，需要 admin 显式授权 |
| `guest` | 保留枚举，**当前代码未启用**（创建/更新用户时仅接受 `admin` / `user`，传 `guest` 会返回 400） |

**枚举严格校验**：admin 创建/更新用户时 `role` 字段必须是 `admin` 或 `user`，乱填会被 400 拒绝。

**用户名/邮箱大小写不敏感**：创建和登录时均自动 `ToLower` + `TrimSpace`，且 SQLite 使用 `LOWER()` 唯一索引。`Admin` 和 `admin` 被视为同一用户。

## 末位管理员保护

- **不能降级最后一个 admin**（HTTP 400 `cannot demote the last admin`）
- **不能删除最后一个 admin**（HTTP 400 `cannot delete the last admin`）
- 防止系统失去任何管理员后陷入"无人能登 admin 路由"的死锁

## 实例权限位

存在 `instance_permissions` 表，每行记录某用户对某实例的权限位掩码：

| 位 | 名称 | 含义 |
|---|---|---|
| 1 | `PermView` | 看实例 / 看监控 / 打开终端只读 |
| 2 | `PermControl` | 启/停/重启/输入 + 部署服务端 + 改实例配置（部分字段） |
| 4 | `PermFiles` | 文件管理 + 备份操作 + 文件上传/下载 |
| 8 | `PermTerminal` | 终端写入（向 stdin 发命令） |
| 16 | `PermManage` | 编辑实例完整配置（admin 等价；建议慎用） |

可叠加，例如 `PermView | PermFiles = 5`；`PermAll = 31` 给全权。

**Admin 角色绕过所有位检查**——admin = 全权。

## 授权流程

「**用户管理**」→ 选用户 →「**权限**」→ 列出该用户已有的实例权限：

1. 点「**新增权限**」
2. 选目标节点 + 实例 + 勾选权限位
3. 保存 → 用户立即生效（下一次请求中间件读 DB）

**撤销**：点权限行尾的删除按钮 → DB 行删除 → 用户立即失去该实例的权限。

## 用户级操作

| 接口 / 页面 | admin 必需 | 说明 |
|---|---|---|
| 创建用户 | ✅ | 设初始密码 + role |
| 修改用户密码 | ✅ | 改了会 bump `tokens_invalid_before` → 该用户所有 JWT 立即失效 |
| 修改用户 role | ✅ | 同上，立即吊销旧 token |
| 删除用户 | ✅ | 级联删除该用户的 API key；登录日志保留；**实例权限行（`InstancePermission`）当前不级联**——若担心残留请管理员手动 revoke |
| 查看登录日志 | ✅ | 全局；显示 IP / UA / 失败原因 |
| 查看审计日志 | ✅ | 全局；记录所有 POST/PUT/DELETE |
| 自助改密 | 任意已登录 | `/api/auth/me/password` |

## 自助改密

- 普通用户可在右上角菜单 →「**修改密码**」
- 首次登录时 `mustChangePassword=true`，所有写请求都被 `EnforcePasswordChange` 中间件拦截到改密页
- 改完后 `tokens_invalid_before` 被设置 → 当前 JWT 失效 → 必须重登

## API Key 与用户的关系

- 每把 API Key 归属一个用户，**继承用户的 role**
- 用户被删除时**级联清理**他名下所有 key
- 用户**改密不会自动撤销** key（按设计：改密针对人类登录，key 是机器凭据，独立生命周期）
- 想批量失效 key：用户自己进 API Key 页点「撤销我的全部 Key」，或 admin 逐行删除

详见 [API Key](api-keys.md)。

## 安全建议

- 给每个使用者**单独账号**，不要共享 admin
- admin 账号开**长密码**（≥ 12 字符 + 复杂度）
- 给 CI / 脚本用 **API Key**，不要把人类账号密码塞进 CI 环境变量
- 定期审计「**登录日志**」看异常 IP / UA
- 改密策略：admin 给敏感账号设 90 天过期周期（手工提醒）
