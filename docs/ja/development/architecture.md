[English](../../development/architecture.md) | [中文](../../zh/development/architecture.md) | **日本語**

# プロジェクト構成

## 3つの Go モジュール

```
packages/
├── shared/           # 共通ユーティリティ、外部依存なし
├── panel/            # コントロールプレーン + Web
└── daemon/           # ノードエージェント
```

`go.work` でワークスペースとして統合されています。`shared` は他の2つに依存しません。`panel` と `daemon` はどちらも `shared` を参照しますが、互いには参照しません。

---

## packages/shared

プロセス間で共有される型とユーティリティ。

| サブパッケージ | 責務 |
|---|---|
| `protocol/` | Panel ↔ Daemon WS RPC メッセージ構造体（InstanceConfig, Hello, Welcome, 全 Action* 定数およびリクエスト/レスポンス構造体） |
| `ratelimit/` | 汎用 IP バケット失敗カウント + 指数バックオフ、sync.Map 実装 |
| `tlscert/` | 自己署名 ECDSA 証明書生成 + SHA-256 フィンガープリントユーティリティ |

---

## packages/panel

```
cmd/panel/main.go              # エントリポイント: config.Load → store.Open → registry.LoadAll → router → ListenAndServe
internal/
├── config/                    # 環境変数の読み込み、JWT シークレットの自動生成
├── store/                     # gorm で SQLite を開き AutoMigrate + デフォルト管理者シード
├── model/                     # User / Daemon / APIKey / InstancePermission / Setting / AuditLog / LoginLog ...
├── auth/
│   ├── jwt.go                 # HS256 署名 / 解析
│   ├── apikey.go              # tps_ プレフィックス / ハッシュ検索 / IP ホワイトリスト / スコープマッチング
│   ├── password.go            # bcrypt
│   └── middleware.go          # ValidateRevocableJWT, Bearer ミドルウェア, RequireRole/Scope
├── access/                    # インスタンスレベルの権限（PermView/Control/Files/Terminal）クエリヘルパー
├── api/
│   ├── router.go              # 全ルート登録（docs/api/endpoints.md 参照）
│   ├── auth.go                # Login / Me / ChangePassword
│   ├── auth_timings.go        # JWT TTL 設定
│   ├── ratelimit_settings.go  # Login/changePw/apiKey レート制限バケット
│   ├── limits_settings.go     # グローバル/JSON/WS ボディ制限 + BodyLimitMiddleware
│   ├── panel_port_settings.go # Panel リスンポート設定
│   ├── trusted_proxies_settings.go  # gin リバースプロキシ信頼リスト
│   ├── queryauth.go           # ?token= JWT 検証（共有 ValidateRevocableJWT）
│   ├── user.go                # ユーザー CRUD + 最後の管理者保護
│   ├── apikey.go              # API キー CRUD + 失効 + 一括失効
│   ├── daemon.go              # ノード CRUD + フィンガープリント取得 + 再取得
│   ├── instance.go            # インスタンス CRUD + start/stop + input + クロスノード集約
│   ├── files_proxy.go         # ファイルダウンロード/アップロード/upload-init の Daemon へのプロキシ
│   ├── fs.go                  # /fs/list /read /write /mkdir 等のプロキシ
│   ├── backup.go              # バックアップ一覧/作成/復元/削除 + 名前/メモのバリデーション
│   ├── terminal.go            # WebSocket ターミナル + ハートビート失効再チェック
│   ├── deploy.go              # テンプレートデプロイ
│   ├── deploy_server.go       # serverdeploy（Vanilla/Paper/...）
│   ├── docker.go              # イメージ一覧/pull/削除 + イメージエイリアス CRUD
│   ├── volumes.go             # マネージドボリューム CRUD
│   ├── monitor.go             # ノードレベル監視（管理者のみ）
│   ├── audit.go               # 監査ログクエリ
│   ├── settings.go            # webhook/captcha/ブランド/ログ制限/ハイバネーション/デプロイソース
│   ├── security_headers.go    # CSP 設定可能ホワイトリスト + X-Frame-Options/nosniff/Referrer-Policy/条件付き HSTS
│   ├── http_timeouts.go       # HTTP タイムアウト設定（ReadHeader/Read/Write/Idle）
│   ├── cors_settings.go       # CORS 許可オリジン設定
│   ├── panel_public_url.go    # Panel パブリック URL 設定
│   ├── login_method.go        # ログイン方式設定（パスワードのみ/oidc+パスワード/oidcのみ）
│   ├── dto.go                 # 出力 DTO（PasswordHash / Token のサニタイズ）
│   ├── errors.go              # apiErr / apiErrWithParams / apiErrFromDB
│   ├── proxy_headers.go       # copySafeDaemonHeaders ホワイトリスト
│   ├── token_bucket.go        # ターミナル WS 接続ごとのトークンバケット
│   └── ...
├── daemonclient/
│   ├── client.go              # Daemon への WS 接続 + フィンガープリントピン + HTTPClient ファクトリ
│   └── registry.go            # 全 Daemon 接続管理 + 再接続
├── scheduler/                 # インスタンス cron タスク
├── monitorhist/               # ノード監視履歴サンプリング
├── alerts/                    # Webhook ディスパッチ
├── loglimit/                  # 監査/ログインログ容量制限
├── captcha/                   # Turnstile + reCAPTCHA 検証
├── netutil/                   # SSRF 防御: ClassifyHost + SafeHTTPClient
├── secrets/                   # AES-GCM 暗号化（captcha シークレット / SSO clientSecret）
└── serverdeploy/              # Paper/Vanilla 等のサーバー jar 解析プロバイダ
```

### 起動フロー（Panel）

```
config.Load()                  # env + JWT シークレット
  ↓
store.Open(cfg)                # gorm.Open + AutoMigrate + 管理者シード
  ↓
daemonclient.NewRegistry(db)   # 各 daemons 行に接続
  ↓
scheduler.New / Start          # cron 開始
monitorhist.New / Start        # 監視履歴収集開始
loglimit.New / Start           # ログクリーンアップ開始
alerts.New                     # Daemon オフライン/オンラインフック登録（60秒デバウンス）
  ↓
api.NewRouter                  # 全ルート + ミドルウェア + SecurityHeaders + CSP の組み立て
  ↓
SetTrustedProxies(LoadTrustedProxies(db))
  ↓
LoadPanelPort(db) → addr       # DB > env > デフォルト
  ↓
LoadHTTPTimeouts(db) → srv.ReadHeaderTimeout/ReadTimeout/WriteTimeout/IdleTimeout
  ↓
signal.Notify(SIGTERM, SIGINT) → グレースフルシャットダウン goroutine
  ↓
http.ListenAndServe(addr, r)   # または ListenAndServeTLS
  ↓ (シグナル受信時)
srv.Shutdown(30s) → クリーン終了
```

---

## packages/daemon

```
cmd/daemon/main.go             # config.Load → Manager → backup → volumes.MountAll(sync) → hib → tlscert → signal.Notify → ListenAndServeTLS → グレースフルシャットダウン (hib.Shutdown → vm.UnmountAll)
internal/
├── config/                    # env + config.json (DataDir/config.json); config.json.template を自動書き込み
├── rpc/
│   └── server.go              # /healthz /cert /files/upload(/init) /files/download /backups/download
│                              # WS /ws: 全 ActionXxx RPC を処理 (instance.create/start/.../fs.list/.../docker.pull/...)
├── instance/                  # プロセス/コンテナのライフサイクル管理
│   ├── manager.go             # インスタンスコレクション + AutoStart
│   ├── instance.go            # 単一インスタンス: startProcess / startDocker / stop / kill
│   ├── store.go               # インスタンス設定の永続化（インスタンスごとの JSON）
│   └── bus.go                 # イベントバス（output / status）
├── docker/                    # Docker CLI ラッパー
├── fs/                        # マウント境界パス解決（トラバーサル防止）
├── backup/                    # Zip バックアップ + 厳密な名前バリデーション
├── volumes/                   # ループバック img マネージドボリューム（mkfs.ext4/xfs + mount + statfs）
├── hibernation/               # 自動ハイバネーション SLP リスナー
├── deploy/                    # serverdeploy バックエンド: jar をインスタンスディレクトリにダウンロード
├── minecraft/                 # SLP プロトコル（プレイヤーリスト + フェイクリスナー）
├── monitor/                   # プロセス / コンテナのリソースサンプリング
└── uploadsession/             # init/uploadId プロトコルステートマシン + GC
```

### 起動フロー（Daemon）

```
config.Load                    # env + config.json (オーバーライド) + テンプレート書き込み
  ↓
instance.NewManager + Load     # 永続化された全 InstanceConfig を読み込み
fs.Mount("files", ...)
fs.Mount("data", ...)
backup.New
volumes.New + MountAll
hibernation.New / Start
deploy.New
  ↓
rpc.New(...)
  ↓
tlscert.LoadOrCreate           # cert.pem / key.pem 自動生成
  ↓
mgr.AutoStartAll              # autoStart=true のインスタンスを起動
  ↓
http.ListenAndServeTLS(addr, ...)
```

---

## web/

フロントエンドの React プロジェクトは **リポジトリルートの `web/` ディレクトリ** に配置されています（`packages/` の下ではありません）。

```
web/
├── package.json
├── vite.config.ts
├── tsconfig.json
└── src/
    ├── main.tsx                   # エントリポイント
    ├── router.tsx                 # React Router ルートテーブル
    ├── api/
    │   ├── client.ts              # axios + インターセプター（Authorization ヘッダー + X-Refreshed-Token）
    │   ├── resources.ts           # daemonsApi, instancesApi 等
    │   └── tasks.ts               # tasksApi, apiKeysApi, permsApi
    ├── stores/
    │   ├── auth.ts                # zustand 永続化トークン + ユーザー
    │   └── brand.ts
    ├── pages/
    │   ├── login/
    │   ├── dashboard/             # ホーム: インスタンスカード + 監視メトリクス
    │   ├── instances/             # インスタンス一覧 + 詳細（ターミナル/ファイル/バックアップ/タスク/監視/編集）
    │   ├── nodes/                 # ノード一覧 + 追加（TLS フィンガープリント TOFU UI 付き）
    │   ├── users/                 # ユーザー管理 + インスタンス権限付与
    │   ├── apikeys/               # API キー管理（有効期限付き作成/失効/一括失効）
    │   ├── settings/              # システム設定（大型カードページ）
    │   ├── audit/                 # 監査ログ + ログインログ
    │   ├── logs/
    │   └── files/                 # ノードレベルファイルブラウザ
    ├── components/
    │   ├── FileExplorer.tsx       # 共有ファイルブラウザ（インスタンス + ノード）
    │   ├── PageHeader.tsx
    │   ├── StatusBadge.tsx
    │   └── ...
    ├── i18n.ts                    # 中国語/英語/日本語 3言語対応
    └── ...
```

### 主要な規約

- すべての API 呼び出しは `@/api/client.ts` の axios インスタンスを経由（自動 Authorization ヘッダー + X-Refreshed-Token スライディング更新処理 + 401 自動ログアウト）
- グローバル状態は zustand persist で localStorage に保存（キー: `taps-auth`、`taps-prefs`、`taps-brand`）
- UI は Ant Design 5 + カスタム CSS 変数を使用
- アイコンセット: `@ant-design/icons`

---

## データフロー: ユーザーが UI でインスタンスを起動

```
ユーザーが「起動」ボタンをクリック
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
Docker daemon がコンテナを作成
  ↓
コンテナ stdout → instance.bus → daemon WS イベント "instance.output"
  ↓ wss
panel daemonclient → router → terminal WS サブスクライバー → ブラウザ
```

---

## データフロー: ユーザーがファイルをアップロード

```
ブラウザでファイルを選択 → web/src/components/FileExplorer.tsx
  ↓ 1. POST /api/daemons/1/files/upload/init { path, totalBytes, totalChunks }
panel: files_proxy.UploadInit → Daemon の /files/upload/init に転送
daemon: uploadsession.Init → クォータチェック → uploadId を返却
  ↓ 2. ループ: POST /api/daemons/1/files/upload?uploadId=&seq=&total=&final= (multipart 1 MiB)
panel: files_proxy.Upload → Daemon の /files/upload に転送
daemon: uploadId / アキュムレータを検証 → .partial に書き込み → 最終時にファイル名をリネーム
```

---

## 新しい RPC アクションの追加

例: Daemon に `instance.dump-state` をサポートさせる。

1. **shared/protocol/message.go**
   - `ActionInstanceDumpState = "instance.dumpState"` を定義
   - リクエスト/レスポンス構造体を定義（例: `InstanceTarget` を再利用、新規 `InstanceDumpStateResp`）

2. **daemon/internal/rpc/server.go**
   - `dispatch` switch にケースを追加
   - `s.mgr.Dump(uuid)` を呼び出すハンドラを実装

3. **daemon/internal/instance/instance.go**
   - `Dump()` メソッドを追加

4. **panel/internal/api/instance.go**
   - `func (h *InstanceHandler) DumpState(c *gin.Context)` を追加し、`cli.Call(ctx, protocol.ActionInstanceDumpState, ...)` を呼び出す

5. **panel/internal/api/router.go**
   - ルートを登録: `di.GET("/:uuid/state", auth.RequireScope("instance.read"), instH.DumpState)`

6. **web/src/api/resources.ts**
   - `dumpState: (id, uuid) => api.get(...)` を追加

7. **web/src/pages/...**
   - UI 統合

8. **docs/api/endpoints.md**
   - エンドポイントテーブルに行を追加

---

## クイックリファレンス

| 探しているもの | 参照先 |
|---|---|
| 全ルート + ミドルウェア | `panel/internal/api/router.go` |
| WS RPC ディスパッチ | `daemon/internal/rpc/server.go` |
| 認証戦略 | `panel/internal/auth/middleware.go` |
| レート制限の実装 | `shared/ratelimit/ratelimit.go` |
| クォータチェック | `daemon/internal/uploadsession/uploadsession.go` |
| インスタンスの起動/停止 | `daemon/internal/instance/instance.go` |
| 自動ハイバネーション | `daemon/internal/hibernation/...` |
| TLS / TOFU | `shared/tlscert/tlscert.go` + `panel/internal/api/daemon.go ProbeFingerprint` |
