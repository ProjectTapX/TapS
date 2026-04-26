# 项目结构

## 三个 Go 模块

```
packages/
├── shared/           # 通用，无外部依赖
├── panel/            # 控制面 + Web
└── daemon/           # 节点代理
```

`go.work` 把它们组合成 workspace。`shared` 不依赖另外两个；`panel` 和 `daemon` 都引用 `shared`，互不引用。

---

## packages/shared

跨进程共享的类型和工具。

| 子包 | 职责 |
|---|---|
| `protocol/` | Panel ↔ Daemon WS RPC 的消息结构（InstanceConfig、Hello、Welcome、所有 Action* 常量与请求/响应结构）|
| `ratelimit/` | 通用 IP-bucket 失败计数 + 指数退避，sync.Map 实现 |
| `tlscert/` | 自签 ECDSA 证书生成 + SHA-256 指纹工具 |

---

## packages/panel

```
cmd/panel/main.go              # 启动入口；config.Load → store.Open → registry.LoadAll → router → ListenAndServe
internal/
├── config/                    # 环境变量加载、JWT secret 自动生成
├── store/                     # gorm 打开 SQLite + AutoMigrate + 默认 admin seed
├── model/                     # User / Daemon / APIKey / InstancePermission / Setting / AuditLog / LoginLog ...
├── auth/
│   ├── jwt.go                 # HS256 签发 / 解析
│   ├── apikey.go              # tps_ 前缀 / hash 查询 / IP 白名单 / scope match
│   ├── password.go            # bcrypt
│   └── middleware.go          # ValidateRevocableJWT、Bearer 中间件、RequireRole/Scope
├── access/                    # 实例级权限 (PermView/Control/Files/Terminal) 查询助手
├── api/
│   ├── router.go              # 全部路由注册（参考 docs/api/endpoints.md）
│   ├── auth.go                # Login / Me / ChangePassword
│   ├── auth_timings.go        # JWT TTL 设置
│   ├── ratelimit_settings.go  # 登录/改密/api key 限频 bucket
│   ├── limits_settings.go     # 全局/JSON/WS body 上限 + BodyLimitMiddleware
│   ├── panel_port_settings.go # Panel 监听端口设置
│   ├── trusted_proxies_settings.go  # gin 反代信任列表
│   ├── queryauth.go           # ?token= JWT 校验（共享 ValidateRevocableJWT）
│   ├── user.go                # 用户 CRUD + 末位 admin 保护
│   ├── apikey.go              # API Key CRUD + revoke + revoke-all
│   ├── daemon.go              # 节点 CRUD + probe-fingerprint + refetch
│   ├── instance.go            # 实例 CRUD + 启停 + 输入 + 跨节点聚合
│   ├── files_proxy.go         # 文件 download/upload/upload-init 代理到 daemon
│   ├── fs.go                  # /fs/list /read /write /mkdir 等代理
│   ├── backup.go              # 备份 list/create/restore/delete + name/note 校验
│   ├── terminal.go            # WebSocket 终端 + 心跳吊销重检
│   ├── deploy.go              # 模板部署
│   ├── deploy_server.go       # serverdeploy（Vanilla/Paper/...）
│   ├── docker.go              # 镜像 list/pull/remove + 镜像别名 CRUD
│   ├── volumes.go             # 托管卷 CRUD
│   ├── monitor.go             # 节点级监控（admin only）
│   ├── audit.go               # 审计日志查询
│   ├── settings.go            # webhook/captcha/brand/log-limits/hibernation/deploy-source
│   ├── security_headers.go    # CSP 可配置白名单 + X-Frame-Options/nosniff/Referrer-Policy/条件 HSTS
│   ├── http_timeouts.go       # HTTP 超时设置 (ReadHeader/Read/Write/Idle)
│   ├── cors_settings.go       # CORS 允许源设置
│   ├── panel_public_url.go    # Panel 公开地址设置
│   ├── login_method.go        # 登录方式设置 (password-only/oidc+password/oidc-only)
│   ├── dto.go                 # 输出 DTO（脱敏 PasswordHash / Token）
│   ├── errors.go              # apiErr / apiErrWithParams / apiErrFromDB
│   ├── proxy_headers.go       # copySafeDaemonHeaders 白名单
│   ├── token_bucket.go        # 终端 WS per-connection 令牌桶
│   └── ...
├── daemonclient/
│   ├── client.go              # WS 连 daemon + 指纹 pin + HTTPClient 工厂
│   └── registry.go            # 全部 daemon 的连接管理 + 重连
├── scheduler/                 # 实例的 cron 任务
├── monitorhist/               # 节点监控历史采样
├── alerts/                    # webhook 派发
├── loglimit/                  # 审计/登录日志容量限制
├── captcha/                   # turnstile + recaptcha 验证
├── netutil/                   # SSRF 防护：ClassifyHost + SafeHTTPClient
├── secrets/                   # AES-GCM 加密 (captcha secret / SSO clientSecret)
└── serverdeploy/              # Paper/Vanilla 等服务端 jar 解析 provider
```

### 启动流程（Panel）

```
config.Load()                  # env + JWT secret
  ↓
store.Open(cfg)                # gorm.Open + AutoMigrate + seed admin
  ↓
daemonclient.NewRegistry(db)   # 连接每个 daemons 行
  ↓
scheduler.New / Start          # 启动 cron
monitorhist.New / Start        # 启动监控历史采集
loglimit.New / Start           # 启动日志清理
alerts.New                     # 注册 daemon offline/online hook（60s 去抖）
  ↓
api.NewRouter                  # 装配全部路由 + 中间件 + SecurityHeaders + CSP
  ↓
SetTrustedProxies(LoadTrustedProxies(db))
  ↓
LoadPanelPort(db) → addr       # DB > env > default
  ↓
LoadHTTPTimeouts(db) → srv.ReadHeaderTimeout/ReadTimeout/WriteTimeout/IdleTimeout
  ↓
signal.Notify(SIGTERM, SIGINT) → graceful shutdown goroutine
  ↓
http.ListenAndServe(addr, r)   # 或 ListenAndServeTLS
  ↓ (on signal)
srv.Shutdown(30s) → clean exit
```

---

## packages/daemon

```
cmd/daemon/main.go             # config.Load → Manager → backup → volumes.MountAll(同步) → hib → tlscert → signal.Notify → ListenAndServeTLS → graceful shutdown (hib.Shutdown → vm.UnmountAll)
internal/
├── config/                    # env + config.json (DataDir/config.json)；自动写 config.json.template
├── rpc/
│   └── server.go              # /healthz /cert /files/upload(/init) /files/download /backups/download
│                              # WS /ws：处理所有 ActionXxx RPC（instance.create/start/.../fs.list/.../docker.pull/...）
├── instance/                  # 进程/容器生命周期管理
│   ├── manager.go             # 实例集合 + AutoStart
│   ├── instance.go            # 单实例：startProcess / startDocker / stop / kill
│   ├── store.go               # 实例配置持久化（per-instance JSON）
│   └── bus.go                 # 事件总线（output / status）
├── docker/                    # docker CLI wrapper
├── fs/                        # mount-bounded 路径解析（防穿越）
├── backup/                    # zip 备份 + name 严格校验
├── volumes/                   # loopback img 托管卷（mkfs.ext4/xfs + mount + statfs）
├── hibernation/               # 自动休眠 SLP listener
├── deploy/                    # serverdeploy 后端：下载 jar 写到实例目录
├── minecraft/                 # SLP 协议（玩家列表 + 假 listener）
├── monitor/                   # 进程 / 容器资源采样
└── uploadsession/             # init/uploadId 协议状态机 + GC
```

### 启动流程（Daemon）

```
config.Load                    # env + config.json (覆盖) + 写 template
  ↓
instance.NewManager + Load     # 加载所有持久化的 InstanceConfig
fs.Mount("files", ...)
fs.Mount("data", ...)
backup.New
volumes.New + MountAll
hibernation.New / Start
deploy.New
  ↓
rpc.New(...)
  ↓
tlscert.LoadOrCreate           # cert.pem / key.pem 自动生成
  ↓
mgr.AutoStartAll              # 启动 autoStart=true 的实例
  ↓
http.ListenAndServeTLS(addr, ...)
```

---

## web/

前端 React 项目位于**仓库顶层 `web/` 目录**（不在 `packages/` 下）。

```
web/
├── package.json
├── vite.config.ts
├── tsconfig.json
└── src/
    ├── main.tsx                   # 入口
    ├── router.tsx                 # React Router 路由表
    ├── api/
    │   ├── client.ts              # axios + interceptor (Authorization 头 + X-Refreshed-Token)
    │   ├── resources.ts           # daemonsApi、instancesApi 等
    │   └── tasks.ts               # tasksApi、apiKeysApi、permsApi
    ├── stores/
    │   ├── auth.ts                # zustand 持久化的 token + user
    │   └── brand.ts
    ├── pages/
    │   ├── login/
    │   ├── dashboard/             # 首页：实例卡片 + 监控指标
    │   ├── instances/             # 实例列表 + 详情（terminal/files/backups/tasks/monitor/edit）
    │   ├── nodes/                 # 节点列表 + 添加（含 TLS 指纹 TOFU UI）
    │   ├── users/                 # 用户管理 + 实例权限授权
    │   ├── apikeys/               # API Key 管理（创建过期/撤销/全撤销）
    │   ├── settings/              # 系统设置（巨卡片页）
    │   ├── audit/                 # 审计日志 + 登录日志
    │   ├── logs/
    │   └── files/                 # 节点级文件浏览器
    ├── components/
    │   ├── FileExplorer.tsx       # 通用文件浏览器（实例 + 节点共用）
    │   ├── PageHeader.tsx
    │   ├── StatusBadge.tsx
    │   └── ...
    ├── i18n.ts                    # 中英双语
    └── ...
```

### 关键约定

- 所有 API 调用走 `@/api/client.ts` 的 axios 实例（自动加 Authorization 头 + 处理 X-Refreshed-Token 滑动续期 + 401 自动 logout）
- 全局状态用 zustand persist 到 localStorage（key `taps-auth`、`taps-prefs`、`taps-brand`）
- UI 用 ant design 5 + 自定义 CSS variables
- 两套 Material Hello-icon 集合：`@ant-design/icons` 主用

---

## 数据流：用户在 UI 启动一个实例

```
用户点 "启动" 按钮
  ↓
web/src/pages/instances/.../detail.tsx
  axios.post('/api/daemons/1/instances/<uuid>/start')
  ↓
panel: router.go → instance.go (InstanceHandler.Start)
  ↓
panel: daemonclient.Client.Call(ctx, "instance.start", InstanceTarget{UUID})
  ↓ wss
daemon: rpc/server.go dispatch ActionInstanceStart
  ↓
daemon: instance/manager.go Start(uuid)
  ↓
daemon: instance.go startDocker → exec.Command("docker", "run", ...)
  ↓
docker daemon 创建容器
  ↓
container stdout → instance.bus → daemon WS event "instance.output"
  ↓ wss
panel daemonclient → router → terminal WS subscriber → 浏览器
```

---

## 数据流：用户上传文件

```
浏览器选文件 → web/src/components/FileExplorer.tsx
  ↓ 1. POST /api/daemons/1/files/upload/init { path, totalBytes, totalChunks }
panel: files_proxy.UploadInit → 转到 daemon /files/upload/init
daemon: uploadsession.Init → 配额检查 → 返回 uploadId
  ↓ 2. 循环：POST /api/daemons/1/files/upload?uploadId=&seq=&total=&final= (multipart 1 MiB)
panel: files_proxy.Upload → 转 daemon /files/upload
daemon: 校验 uploadId / accumulator → 写 .partial → final 时 rename 成最终文件
```

---

## 添加一个新的 RPC Action

例：让 daemon 支持 `instance.dump-state`。

1. **shared/protocol/message.go**
   - 定义 `ActionInstanceDumpState = "instance.dumpState"`
   - 定义请求 / 响应结构（如 `InstanceTarget` 复用、`InstanceDumpStateResp`）

2. **daemon/internal/rpc/server.go**
   - 在 `dispatch` switch case 加一条
   - 实现 handler 调 `s.mgr.Dump(uuid)`

3. **daemon/internal/instance/instance.go**
   - 加 `Dump()` 方法

4. **panel/internal/api/instance.go**
   - 加 `func (h *InstanceHandler) DumpState(c *gin.Context)`，里面 `cli.Call(ctx, protocol.ActionInstanceDumpState, ...)`

5. **panel/internal/api/router.go**
   - 注册路由：`di.GET("/:uuid/state", auth.RequireScope("instance.read"), instH.DumpState)`

6. **web/src/api/resources.ts**
   - 加 `dumpState: (id, uuid) => api.get(...)`

7. **web/src/pages/...**
   - UI 调用

8. **docs/api/endpoints.md**
   - 在端点表里加一行

---

## 关键文件速查

| 关心的事 | 看这里 |
|---|---|
| 所有路由 + 中间件 | `panel/internal/api/router.go` |
| WS RPC dispatch | `daemon/internal/rpc/server.go` |
| 鉴权策略 | `panel/internal/auth/middleware.go` |
| 限频实现 | `shared/ratelimit/ratelimit.go` |
| 配额检查 | `daemon/internal/uploadsession/uploadsession.go` |
| 实例启停 | `daemon/internal/instance/instance.go` |
| 自动休眠 | `daemon/internal/hibernation/...` |
| TLS / TOFU | `shared/tlscert/tlscert.go` + `panel/internal/api/daemon.go ProbeFingerprint` |
