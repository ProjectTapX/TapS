[English](../../development/build.md) | [中文](../../zh/development/build.md) | **日本語**

# ソースからのビルド

## ツールチェーン

- **Go ≥ 1.25**（go.work でワークスペースを使用）
- **Node.js ≥ 18**（フロントエンド Vite + React + TypeScript）
- **npm**（または yarn / pnpm。以下の例では npm を使用）

## プロジェクト構成

```
TapS/
├── go.work                # Go マルチモジュールワークスペース
├── packages/
│   ├── shared/            # 共有: protocol, ratelimit, tlscert
│   │   └── go.mod
│   ├── panel/             # コントロールパネル
│   │   ├── go.mod
│   │   ├── cmd/panel/main.go
│   │   └── internal/      # api / auth / config / model / store / ...
│   └── daemon/            # Daemon エージェント
│       ├── go.mod
│       ├── cmd/daemon/main.go
│       └── internal/      # rpc / config / instance / docker / ...
├── web/                   # フロントエンド
│   ├── package.json
│   ├── src/               # React コード
│   ├── public/
│   └── vite.config.ts
├── scripts/build.sh       # ワンクリッククロスコンパイル
├── dist/                  # ビルド出力
└── docs/                  # ドキュメント
```

## ローカルビルド（開発マシン）

```bash
cd TapS

# バックエンド
go build ./packages/panel/...
go build ./packages/daemon/...    # Daemon は Linux でのみコンパイル可能（syscall.Statfs を使用）

# Panel の実行（開発モード）
cd packages/panel
TAPS_DATA_DIR=./_data \
TAPS_WEB_DIR=../../web/dist \
TAPS_ADDR=:24444 \
go run ./cmd/panel

# 別のシェルで Daemon を起動
cd packages/daemon
TAPS_DAEMON_DATA=./_data \
TAPS_DAEMON_ADDR=:24445 \
go run ./cmd/daemon
```

## フロントエンド開発

```bash
cd web
npm install
npm run dev
# Vite 開発サーバーが http://localhost:5173 で起動
# vite.config.ts により /api が http://localhost:24444 にプロキシされる
```

フロントエンドのコード変更はホットリロードされます。バックエンドの変更には `go run` の再起動が必要です。

## ワンクリッククロスコンパイル（本番ビルド）

```bash
./scripts/build.sh
# dist/ への出力:
#   dist/panel-linux-amd64
#   dist/panel-linux-arm64
#   dist/daemon-linux-amd64
#   dist/daemon-linux-arm64
#   dist/web/  ← フロントエンド dist のコピー
```

`build.sh` はデフォルトで2つの本番ターゲット + Web をコンパイルします:

| OS | ARCH | 説明 |
|----|------|------|
| linux | amd64 | 主流の VPS / 専用サーバー |
| linux | arm64 | Raspberry Pi / ARM クラウドインスタンス |

> **Linux のみが本番サポートプラットフォームです。** macOS / Windows はローカル開発用に手動で `go build` できますが、以下の機能が利用不可または制限されます:
>
> | 機能 | macOS | Windows |
> |------|-------|---------|
> | マネージドボリューム（ディスククォータ） | 利用不可 | 利用不可 |
> | O_NOFOLLOW 保護 | 対応 | 制限あり（保護なし） |
> | SIGTERM グレースフルインスタンス停止 | 対応 | 制限あり（強制終了） |
> | PTY ターミナル | 対応 | 互換性が低い |

## linux/amd64 のみのクロスコンパイル

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath \
  go -C packages/panel build -ldflags '-s -w' -o ../../dist/panel-linux-amd64 ./cmd/panel

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath \
  go -C packages/daemon build -ldflags '-s -w' -o ../../dist/daemon-linux-amd64 ./cmd/daemon
```

## フロントエンドのみのビルド

```bash
cd web
npm run build
# 出力: web/dist/
# デプロイ用パッケージ:
tar -czf ../dist/web.tar.gz -C dist .
```

## サーバーへのデプロイ

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

## テスト

```bash
# ユニットテスト（利用可能な場合）
go test ./packages/panel/... ./packages/daemon/... ./packages/shared/...

# コンパイル時チェック（テストなし）
go vet ./packages/panel/... ./packages/daemon/... ./packages/shared/...
```

## Docker イメージ

リポジトリには `packages/{panel,daemon}/Dockerfile` とルートレベルの `docker-compose.yml` が含まれています。

```bash
docker compose up --build -d
# panel-data + daemon-data の永続ボリュームで panel + daemon を起動
# http://localhost:24444 にアクセス
```

## デバッグ

```bash
# バックエンド: dlv を使用
dlv debug ./packages/panel/cmd/panel -- # args

# フロントエンド: ブラウザ DevTools
# Vite はデフォルトでソースマップを含む
```

## コードスタイル / ツール

- Go: `gofmt -w .` + `goimports -w .`
- TypeScript: `npm run lint`（ESLint、TS strict モード）
- コミット前に `go vet ./...` を実行 — 警告ゼロであること
