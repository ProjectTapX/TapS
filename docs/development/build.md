**English** | [中文](../zh/development/build.md) | [日本語](../ja/development/build.md)

# Building from Source

## Toolchain

- **Go ≥ 1.25** (go.work uses workspaces)
- **Node.js ≥ 18** (frontend Vite + React + TypeScript)
- **npm** (or yarn / pnpm; examples below use npm)

## Project Structure

```
TapS/
├── go.work                # Go multi-module workspace
├── packages/
│   ├── shared/            # Shared: protocol, ratelimit, tlscert
│   │   └── go.mod
│   ├── panel/             # Control panel
│   │   ├── go.mod
│   │   ├── cmd/panel/main.go
│   │   └── internal/      # api / auth / config / model / store / ...
│   └── daemon/            # Daemon agent
│       ├── go.mod
│       ├── cmd/daemon/main.go
│       └── internal/      # rpc / config / instance / docker / ...
├── web/                   # Frontend
│   ├── package.json
│   ├── src/               # React code
│   ├── public/
│   └── vite.config.ts
├── scripts/build.sh       # One-click cross-compile
├── dist/                  # Build output
└── docs/                  # Documentation
```

## Local Build (Development Machine)

```bash
cd TapS

# Backend
go build ./packages/panel/...
go build ./packages/daemon/...    # daemon only compiles on Linux (uses syscall.Statfs)

# Run panel (dev mode)
cd packages/panel
TAPS_DATA_DIR=./_data \
TAPS_WEB_DIR=../../web/dist \
TAPS_ADDR=:24444 \
go run ./cmd/panel

# Open another shell for daemon
cd packages/daemon
TAPS_DAEMON_DATA=./_data \
TAPS_DAEMON_ADDR=:24445 \
go run ./cmd/daemon
```

## Frontend Development

```bash
cd web
npm install
npm run dev
# Vite dev server at http://localhost:5173
# Proxies /api to http://localhost:24444 via vite.config.ts
```

Frontend code changes hot-reload; backend changes require `go run` restart.

## One-Click Cross-Compile (Production Build)

```bash
./scripts/build.sh
# Output in dist/:
#   dist/panel-linux-amd64
#   dist/panel-linux-arm64
#   dist/daemon-linux-amd64
#   dist/daemon-linux-arm64
#   dist/web/  ← frontend dist copy
```

`build.sh` compiles 2 production targets + web by default:

| OS | ARCH | Description |
|----|------|-------------|
| linux | amd64 | Mainstream VPS / dedicated servers |
| linux | arm64 | Raspberry Pi / ARM cloud instances |

> **Only Linux is a production-supported platform.** macOS / Windows can be manually `go build`'d for local development, but the following features are unavailable or degraded:
>
> | Feature | macOS | Windows |
> |---------|-------|---------|
> | Managed volumes (disk quota) | Not available | Not available |
> | O_NOFOLLOW protection | Yes | Degraded (no protection) |
> | SIGTERM graceful instance stop | Yes | Degraded (hard kill) |
> | PTY terminal | Yes | Poor compatibility |

## Cross-Compile linux/amd64 Only

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath \
  go -C packages/panel build -ldflags '-s -w' -o ../../dist/panel-linux-amd64 ./cmd/panel

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath \
  go -C packages/daemon build -ldflags '-s -w' -o ../../dist/daemon-linux-amd64 ./cmd/daemon
```

## Frontend-Only Build

```bash
cd web
npm run build
# Output: web/dist/
# Package for deployment:
tar -czf ../dist/web.tar.gz -C dist .
```

## Deploy to Server

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

## Testing

```bash
# Unit tests (if available)
go test ./packages/panel/... ./packages/daemon/... ./packages/shared/...

# Compile-time checks (no tests)
go vet ./packages/panel/... ./packages/daemon/... ./packages/shared/...
```

## Docker Image

The repository includes `packages/{panel,daemon}/Dockerfile` and a root-level `docker-compose.yml`.

```bash
docker compose up --build -d
# Starts panel + daemon with persistent volumes for panel-data + daemon-data
# Access http://localhost:24444
```

## Debugging

```bash
# Backend: use dlv
dlv debug ./packages/panel/cmd/panel -- # args

# Frontend: browser DevTools
# Vite includes source maps by default
```

## Code Style / Tools

- Go: `gofmt -w .` + `goimports -w .`
- TypeScript: `npm run lint` (ESLint, TS strict mode)
- Run `go vet ./...` before committing — must have zero warnings
