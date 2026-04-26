# 从源码构建

## 工具链

- **Go ≥ 1.25**（go.work 用了 workspaces）
- **Node.js ≥ 18**（前端 Vite + React + TypeScript）
- **npm**（或 yarn / pnpm，下面以 npm 为例）

## 项目结构

```
TapS/
├── go.work                # Go 多模块工作区
├── packages/
│   ├── shared/            # 共享：protocol、ratelimit、tlscert
│   │   └── go.mod
│   ├── panel/             # 控制面板
│   │   ├── go.mod
│   │   ├── cmd/panel/main.go
│   │   └── internal/      # api / auth / config / model / store / ...
│   └── daemon/            # 守护进程
│       ├── go.mod
│       ├── cmd/daemon/main.go
│       └── internal/      # rpc / config / instance / docker / ...
├── web/                   # 前端
│   ├── package.json
│   ├── src/               # React 代码
│   ├── public/
│   └── vite.config.ts
├── scripts/build.sh       # 一键交叉编译
├── dist/                  # 构建产物输出
└── docs/                  # 本文档
```

## 本地构建（开发机）

```bash
cd TapS

# 后端
go build ./packages/panel/...
go build ./packages/daemon/...    # daemon 只能在 Linux 编译（用了 syscall.Statfs）

# 跑 panel（开发模式）
cd packages/panel
TAPS_DATA_DIR=./_data \
TAPS_WEB_DIR=../../web/dist \
TAPS_ADDR=:24444 \
go run ./cmd/panel

# 另开一个 shell 跑 daemon
cd packages/daemon
TAPS_DAEMON_DATA=./_data \
TAPS_DAEMON_ADDR=:24445 \
go run ./cmd/daemon
```

## 前端开发

```bash
cd web
npm install
npm run dev
# Vite dev server 在 http://localhost:5173
# 访问后通过 vite.config.ts 的 proxy 把 /api 转给 http://localhost:24444
```

修改前端代码热更新；改完后端要 `go run` 重启。

## 一键交叉编译（生产构建）

```bash
./scripts/build.sh
# 输出全在 dist/：
#   dist/panel-linux-amd64
#   dist/panel-linux-arm64
#   dist/daemon-linux-amd64
#   dist/daemon-linux-arm64
#   dist/web/  ← 前端 dist 拷贝
```

`build.sh` 默认编译 2 个生产目标 + Web：

| OS | ARCH | 说明 |
|----|------|------|
| linux | amd64 | 主流 VPS / 独服 |
| linux | arm64 | 树莓派 / ARM 云主机 |

> **仅 Linux 为生产支持平台。** macOS / Windows 可以手动 `go build` 用于本地开发联调，但以下功能不可用或降级：
>
> | 功能 | macOS | Windows |
> |------|-------|---------|
> | 托管卷（磁盘配额） | ❌ 不可用 | ❌ 不可用 |
> | O_NOFOLLOW 防护 | ✅ | ❌ 降级为无防护 |
> | SIGTERM 优雅停止实例 | ✅ | ❌ 降级为硬杀 |
> | PTY 终端 | ✅ | ⚠️ 兼容性差 |

## 单独交叉编译 linux/amd64

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath \
  go -C packages/panel build -ldflags '-s -w' -o ../../dist/panel-linux-amd64 ./cmd/panel

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath \
  go -C packages/daemon build -ldflags '-s -w' -o ../../dist/daemon-linux-amd64 ./cmd/daemon
```

## 前端单独构建

```bash
cd web
npm run build
# 输出 web/dist/
# 打包给部署：
tar -czf ../dist/web.tar.gz -C dist .
```

## 部署到服务器

```bash
scp dist/panel-linux-amd64 dist/daemon-linux-amd64 dist/web.tar.gz user@host:/tmp/
ssh user@host bash -c '
  systemctl stop taps-panel taps-daemon
  mv /tmp/panel-linux-amd64  /opt/taps/panel
  mv /tmp/daemon-linux-amd64 /opt/taps/daemon
  chmod +x /opt/taps/panel /opt/taps/daemon
  rm -rf /opt/taps/web && mkdir -p /opt/taps/web
  tar -xzf /tmp/web.tar.gz -C /opt/taps/web && rm /tmp/web.tar.gz
  systemctl start taps-daemon taps-panel
'
```

## 测试

```bash
# 单元测试（如果有）
go test ./packages/panel/... ./packages/daemon/... ./packages/shared/...

# 编译时检查（不跑测试）
go vet ./packages/panel/... ./packages/daemon/... ./packages/shared/...
```

## Docker 镜像

仓库里有 `packages/{panel,daemon}/Dockerfile` 和根目录 `docker-compose.yml`。

```bash
docker compose up --build -d
# 起一个 panel + daemon，volumes 持久化 panel-data + daemon-data
# 访问 http://localhost:24444
```

## 调试

```bash
# 后端：用 dlv
dlv debug ./packages/panel/cmd/panel -- # args

# 前端：浏览器 DevTools
# Vite 默认带 source map
```

## 代码风格 / 工具

- Go：`gofmt -w .` + `goimports -w .`
- TypeScript：`npm run lint`（ESLint，TS 严格模式）
- 提交前 `go vet ./...` 必须无 warning
