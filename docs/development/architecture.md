**English** | [中文](../zh/development/architecture.md) | [日本語](../ja/development/architecture.md)

# Project Structure

## Three Go Modules

```
packages/
├── shared/           # Common utilities, no external dependencies
├── panel/            # Control plane + Web
└── daemon/           # Node agent
```

`go.work` combines them into a workspace. `shared` depends on neither of the other two; `panel` and `daemon` both reference `shared` but not each other.

---

## packages/shared

Cross-process shared types and utilities.

| Subpackage | Responsibility |
|---|---|
| `protocol/` | Panel ↔ Daemon WS RPC message structures (InstanceConfig, Hello, Welcome, all Action* constants and request/response structs) |
| `ratelimit/` | Generic IP-bucket failure counting + exponential backoff, sync.Map implementation |
| `tlscert/` | Self-signed ECDSA certificate generation + SHA-256 fingerprint utilities |

---

## packages/panel

```
cmd/panel/main.go              # Entry point: config.Load → store.Open → registry.LoadAll → router → ListenAndServe
internal/
├── config/                    # Environment variable loading, JWT secret auto-generation
├── store/                     # gorm opens SQLite + AutoMigrate + default admin seed
├── model/                     # User / Daemon / APIKey / InstancePermission / Setting / AuditLog / LoginLog ...
├── auth/
│   ├── jwt.go                 # HS256 signing / parsing
│   ├── apikey.go              # tps_ prefix / hash lookup / IP whitelist / scope matching
│   ├── password.go            # bcrypt
│   └── middleware.go          # ValidateRevocableJWT, Bearer middleware, RequireRole/Scope
├── access/                    # Instance-level permission (PermView/Control/Files/Terminal) query helpers
├── api/
│   ├── router.go              # All route registration (see docs/api/endpoints.md)
│   ├── auth.go                # Login / Me / ChangePassword
│   ├── auth_timings.go        # JWT TTL settings
│   ├── ratelimit_settings.go  # Login/changePw/apiKey rate limit buckets
│   ├── limits_settings.go     # Global/JSON/WS body limits + BodyLimitMiddleware
│   ├── panel_port_settings.go # Panel listen port settings
│   ├── trusted_proxies_settings.go  # gin reverse proxy trust list
│   ├── queryauth.go           # ?token= JWT validation (shared ValidateRevocableJWT)
│   ├── user.go                # User CRUD + last admin protection
│   ├── apikey.go              # API Key CRUD + revoke + revoke-all
│   ├── daemon.go              # Node CRUD + probe-fingerprint + refetch
│   ├── instance.go            # Instance CRUD + start/stop + input + cross-node aggregation
│   ├── files_proxy.go         # File download/upload/upload-init proxy to daemon
│   ├── fs.go                  # /fs/list /read /write /mkdir etc. proxy
│   ├── backup.go              # Backup list/create/restore/delete + name/note validation
│   ├── terminal.go            # WebSocket terminal + heartbeat revocation recheck
│   ├── deploy.go              # Template deployment
│   ├── deploy_server.go       # serverdeploy (Vanilla/Paper/...)
│   ├── docker.go              # Image list/pull/remove + image alias CRUD
│   ├── volumes.go             # Managed volume CRUD
│   ├── monitor.go             # Node-level monitoring (admin only)
│   ├── audit.go               # Audit log queries
│   ├── settings.go            # webhook/captcha/brand/log-limits/hibernation/deploy-source
│   ├── security_headers.go    # CSP configurable whitelist + X-Frame-Options/nosniff/Referrer-Policy/conditional HSTS
│   ├── http_timeouts.go       # HTTP timeout settings (ReadHeader/Read/Write/Idle)
│   ├── cors_settings.go       # CORS allowed origins settings
│   ├── panel_public_url.go    # Panel public URL settings
│   ├── login_method.go        # Login method settings (password-only/oidc+password/oidc-only)
│   ├── dto.go                 # Output DTOs (sanitize PasswordHash / Token)
│   ├── errors.go              # apiErr / apiErrWithParams / apiErrFromDB
│   ├── proxy_headers.go       # copySafeDaemonHeaders whitelist
│   ├── token_bucket.go        # Terminal WS per-connection token bucket
│   └── ...
├── daemonclient/
│   ├── client.go              # WS connection to daemon + fingerprint pin + HTTPClient factory
│   └── registry.go            # All daemon connection management + reconnection
├── scheduler/                 # Instance cron tasks
├── monitorhist/               # Node monitoring history sampling
├── alerts/                    # Webhook dispatch
├── loglimit/                  # Audit/login log capacity limiting
├── captcha/                   # Turnstile + reCAPTCHA verification
├── netutil/                   # SSRF protection: ClassifyHost + SafeHTTPClient
├── secrets/                   # AES-GCM encryption (captcha secret / SSO clientSecret)
└── serverdeploy/              # Paper/Vanilla etc. server jar parsing providers
```

### Startup Flow (Panel)

```
config.Load()                  # env + JWT secret
  ↓
store.Open(cfg)                # gorm.Open + AutoMigrate + seed admin
  ↓
daemonclient.NewRegistry(db)   # connect to each daemons row
  ↓
scheduler.New / Start          # start cron
monitorhist.New / Start        # start monitoring history collection
loglimit.New / Start           # start log cleanup
alerts.New                     # register daemon offline/online hooks (60s debounce)
  ↓
api.NewRouter                  # assemble all routes + middleware + SecurityHeaders + CSP
  ↓
SetTrustedProxies(LoadTrustedProxies(db))
  ↓
LoadPanelPort(db) → addr       # DB > env > default
  ↓
LoadHTTPTimeouts(db) → srv.ReadHeaderTimeout/ReadTimeout/WriteTimeout/IdleTimeout
  ↓
signal.Notify(SIGTERM, SIGINT) → graceful shutdown goroutine
  ↓
http.ListenAndServe(addr, r)   # or ListenAndServeTLS
  ↓ (on signal)
srv.Shutdown(30s) → clean exit
```

---

## packages/daemon

```
cmd/daemon/main.go             # config.Load → Manager → backup → volumes.MountAll(sync) → hib → tlscert → signal.Notify → ListenAndServeTLS → graceful shutdown (hib.Shutdown → vm.UnmountAll)
internal/
├── config/                    # env + config.json (DataDir/config.json); auto-writes config.json.template
├── rpc/
│   └── server.go              # /healthz /cert /files/upload(/init) /files/download /backups/download
│                              # WS /ws: handles all ActionXxx RPCs (instance.create/start/.../fs.list/.../docker.pull/...)
├── instance/                  # Process/container lifecycle management
│   ├── manager.go             # Instance collection + AutoStart
│   ├── instance.go            # Single instance: startProcess / startDocker / stop / kill
│   ├── store.go               # Instance config persistence (per-instance JSON)
│   └── bus.go                 # Event bus (output / status)
├── docker/                    # Docker CLI wrapper
├── fs/                        # Mount-bounded path resolution (traversal prevention)
├── backup/                    # Zip backup + strict name validation
├── volumes/                   # Loopback img managed volumes (mkfs.ext4/xfs + mount + statfs)
├── hibernation/               # Auto-hibernation SLP listener
├── deploy/                    # serverdeploy backend: download jar to instance directory
├── minecraft/                 # SLP protocol (player list + fake listener)
├── monitor/                   # Process / container resource sampling
└── uploadsession/             # init/uploadId protocol state machine + GC
```

### Startup Flow (Daemon)

```
config.Load                    # env + config.json (override) + write template
  ↓
instance.NewManager + Load     # load all persisted InstanceConfigs
fs.Mount("files", ...)
fs.Mount("data", ...)
backup.New
volumes.New + MountAll
hibernation.New / Start
deploy.New
  ↓
rpc.New(...)
  ↓
tlscert.LoadOrCreate           # cert.pem / key.pem auto-generated
  ↓
mgr.AutoStartAll              # start instances with autoStart=true
  ↓
http.ListenAndServeTLS(addr, ...)
```

---

## web/

The frontend React project lives at the **repository root `web/` directory** (not under `packages/`).

```
web/
├── package.json
├── vite.config.ts
├── tsconfig.json
└── src/
    ├── main.tsx                   # Entry point
    ├── router.tsx                 # React Router route table
    ├── api/
    │   ├── client.ts              # axios + interceptor (Authorization header + X-Refreshed-Token)
    │   ├── resources.ts           # daemonsApi, instancesApi, etc.
    │   └── tasks.ts               # tasksApi, apiKeysApi, permsApi
    ├── stores/
    │   ├── auth.ts                # zustand persisted token + user
    │   └── brand.ts
    ├── pages/
    │   ├── login/
    │   ├── dashboard/             # Home: instance cards + monitoring metrics
    │   ├── instances/             # Instance list + detail (terminal/files/backups/tasks/monitor/edit)
    │   ├── nodes/                 # Node list + add (with TLS fingerprint TOFU UI)
    │   ├── users/                 # User management + instance permission grants
    │   ├── apikeys/               # API Key management (create with expiry/revoke/revoke-all)
    │   ├── settings/              # System settings (large card page)
    │   ├── audit/                 # Audit log + login log
    │   ├── logs/
    │   └── files/                 # Node-level file browser
    ├── components/
    │   ├── FileExplorer.tsx       # Shared file browser (instance + node)
    │   ├── PageHeader.tsx
    │   ├── StatusBadge.tsx
    │   └── ...
    ├── i18n.ts                    # Chinese/English/Japanese trilingual
    └── ...
```

### Key Conventions

- All API calls go through `@/api/client.ts`'s axios instance (auto Authorization header + X-Refreshed-Token sliding renewal handling + 401 auto-logout)
- Global state uses zustand persist to localStorage (keys `taps-auth`, `taps-prefs`, `taps-brand`)
- UI uses Ant Design 5 + custom CSS variables
- Icon set: `@ant-design/icons`

---

## Data Flow: User Starts an Instance in the UI

```
User clicks "Start" button
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
Docker daemon creates container
  ↓
container stdout → instance.bus → daemon WS event "instance.output"
  ↓ wss
panel daemonclient → router → terminal WS subscriber → browser
```

---

## Data Flow: User Uploads a File

```
Browser selects file → web/src/components/FileExplorer.tsx
  ↓ 1. POST /api/daemons/1/files/upload/init { path, totalBytes, totalChunks }
panel: files_proxy.UploadInit → forwards to daemon /files/upload/init
daemon: uploadsession.Init → quota check → returns uploadId
  ↓ 2. Loop: POST /api/daemons/1/files/upload?uploadId=&seq=&total=&final= (multipart 1 MiB)
panel: files_proxy.Upload → forwards to daemon /files/upload
daemon: validates uploadId / accumulator → writes .partial → on final, renames to final file
```

---

## Adding a New RPC Action

Example: make daemon support `instance.dump-state`.

1. **shared/protocol/message.go**
   - Define `ActionInstanceDumpState = "instance.dumpState"`
   - Define request/response structs (e.g., reuse `InstanceTarget`, new `InstanceDumpStateResp`)

2. **daemon/internal/rpc/server.go**
   - Add a case in the `dispatch` switch
   - Implement handler calling `s.mgr.Dump(uuid)`

3. **daemon/internal/instance/instance.go**
   - Add `Dump()` method

4. **panel/internal/api/instance.go**
   - Add `func (h *InstanceHandler) DumpState(c *gin.Context)` with `cli.Call(ctx, protocol.ActionInstanceDumpState, ...)`

5. **panel/internal/api/router.go**
   - Register route: `di.GET("/:uuid/state", auth.RequireScope("instance.read"), instH.DumpState)`

6. **web/src/api/resources.ts**
   - Add `dumpState: (id, uuid) => api.get(...)`

7. **web/src/pages/...**
   - UI integration

8. **docs/api/endpoints.md**
   - Add a row to the endpoint table

---

## Quick Reference

| Looking for | Look here |
|---|---|
| All routes + middleware | `panel/internal/api/router.go` |
| WS RPC dispatch | `daemon/internal/rpc/server.go` |
| Auth strategy | `panel/internal/auth/middleware.go` |
| Rate limiting implementation | `shared/ratelimit/ratelimit.go` |
| Quota checking | `daemon/internal/uploadsession/uploadsession.go` |
| Instance start/stop | `daemon/internal/instance/instance.go` |
| Auto-hibernation | `daemon/internal/hibernation/...` |
| TLS / TOFU | `shared/tlscert/tlscert.go` + `panel/internal/api/daemon.go ProbeFingerprint` |
