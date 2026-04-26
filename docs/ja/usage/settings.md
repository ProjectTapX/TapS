[English](../../usage/settings.md) | [中文](../zh/usage/settings.md) | **日本語**

# システム設定リファレンス

**システム設定**ページ（管理者専用）。すべての設定は SQLite の `settings` テーブル（key/value テキスト）に保存されます。

## カード順序と設定一覧

上から順に：

| # | カード | 主な設定 | デフォルト | 反映タイミング |
|---|--------|---------|---------|--------------|
| 1 | **サイトブランディング** | siteName | `TapS` | 即時 |
| | | favicon (PNG/ICO) | なし | 即時 |
| 2 | **パネル公開 URL** | publicUrl | 空 | 即時 |
| 3 | **パネルリッスンポート** | port | 24444 | **再起動が必要** |
| 4 | **信頼済みプロキシリスト** | proxies | `127.0.0.1, ::1` | **再起動が必要** |
| 5 | **CORS 許可オリジン** | origins | 空 | 即時 |
| 6 | **ログイン CAPTCHA** | provider / siteKey / secret / scoreThreshold | `none` / 空 / 暗号化済み / 0.5 | 即時 |
| 7 | **ログイン方式** | method | `password-only` | 即時 |
| 8 | **SSO プロバイダー (OIDC)** | プロバイダーリスト | — | 即時 |
| 9 | **サーバーダウンロードソース** | source | `fastmirror` | 即時 |
| 10 | **Minecraft Java 自動休止** | defaultEnabled / minutes / warmup / motd / kick / icon | true / 60 / 5 | 即時 |
| 11 | **Webhook 通知** | url / allowPrivate | 空 / false | 即時 |
| 12 | **ログ容量制限** | auditMaxRows / loginMaxRows | 1000000 | 即時 |
| 13 | **レート制限** | rateLimitPerMin / banDurationMinutes | 5 / 5 | リアルタイム |
| | | oauthStartCount / oauthStartWindowSec | 30 / 300 | リアルタイム |
| | | pkceStoreMaxEntries | 10000 | リアルタイム |
| | | terminalReadDeadlineSec / inputRatePerSec / inputBurst | 60 / 200 / 50 | 新規 WS 接続時 |
| | | iconCacheMaxAgeSec / iconRatePerMin | 300 / 10 | 即時 |
| 14 | **リクエストサイズ制限** | maxRequestBodyBytes / maxJsonBodyBytes / maxWsFrameBytes | 128 KiB / 16 MiB / 16 MiB | リアルタイム |
| 15 | **Content Security Policy (CSP)** | scriptSrcExtra / frameSrcExtra | Cloudflare + reCAPTCHA CDN | 即時 |
| 16 | **セッション有効期間** | jwtTtlMinutes / wsHeartbeatMinutes | 60 / 5 | 新規セッション |
| 17 | **HTTP タイムアウト（Slow-Loris 対策）** | readHeaderTimeoutSec / readTimeoutSec / writeTimeoutSec / idleTimeoutSec | 10 / 60 / 120 / 120 | **再起動が必要** |

---

## 詳細

### サイトブランディング

- **siteName**: ブラウザタイトルおよびログインページのヒーローエリアに表示される名前。使用可能文字：英数字、CJK 文字、一般的な記号。CJK 文字は重み 2 としてカウントされ、上限は 16 重み単位（ASCII 最大 16 文字、CJK 最大 8 文字）。
  - 参照：`panel/internal/api/settings.go validSiteName()`
- **favicon**: PNG / ICO ≤ 64 KiB をアップロード。**SVG は不可**（格納型 XSS 防止のため無効化）。サーバーは常に `http.DetectContentType` で実際の型を判定し、クライアントの Content-Type を無視します。
  - 参照：`panel/internal/api/settings.go SetBrandFavicon()`

### パネル公開 URL

パネルの外部アクセス URL（プロトコル含む）。例：`https://taps.example.com`。複数の機能がこの設定に依存します：

1. **SSO/OIDC コールバックアドレス**: `<publicUrl>/api/oauth/callback/<provider>`
2. **ターミナル WebSocket オリジン検証**: 未設定の場合、ターミナルセッションは拒否されます
3. **CORS 許可オリジンのフォールバック**: CORS ホワイトリストが空の場合、publicUrl が同一オリジン比較に使用されます

- 参照：`panel/internal/api/panel_public_url.go`

### パネルリッスンポート

DB に書き込まれ、パネルプロセスの再起動後に反映されます。優先順位：DB > 環境変数 (`TAPS_ADDR`) > デフォルト 24444。

### 信頼済みプロキシリスト

パネルが nginx / Caddy / Cloudflare の背後にある場合にのみ必要です。設定しないと `c.ClientIP()` が常に `127.0.0.1` を返し、レート制限・監査ログ・API Key IP ホワイトリストがすべて正常に動作しません。変更後は**パネルの再起動が必要**です。
- 参照：`panel/internal/api/trusted_proxies_settings.go`

### CORS 許可オリジン

カンマ区切りのオリジンリスト（`scheme://host[:port]`）。リストに含まれるドメインのブラウザ JS のみがパネル API にクロスオリジンリクエストを送信できます。空の場合、パネル自身の publicUrl のみ許可されます（同一オリジン SPA は常に通過）。API Key によるサーバー間通信は影響を受けません。即時反映。
- 参照：`panel/internal/api/cors_settings.go`

### ログイン CAPTCHA

**ログインエンドポイント**にのみ適用されます。

| プロバイダー | 説明 |
|------------|------|
| `none` | 無効 |
| `turnstile` | Cloudflare Turnstile |
| `recaptcha` | Google reCAPTCHA Enterprise |

**主な動作**：
- **フェイルオープン**: キーレベルのエラー（シークレット誤り、サイトキー不一致）→ `ErrConfig` → ロックアウト防止のためログインを許可。ネットワークエラー / 5xx → フェイルクローズド、ログイン拒否
- **シークレットの暗号化保存**: CAPTCHA シークレットは `captcha.secretEnc` カラムに AES-GCM で暗号化して保存。管理者 GET は `hasSecret: true/false` を返し、**シークレット平文は返しません**
- **プロバイダー切替時にシークレットをリセット**: Turnstile から reCAPTCHA（またはその逆）に切り替えると、バックエンドはシークレット空の PUT を拒否。フロントエンドは siteKey + secret 入力を自動クリアします
- **scoreThreshold 0 も有効**: `*float64` ポインタ型。nil = 既存値を維持、0 = 閾値無効（すべての reCAPTCHA トークンが通過）、0.1-0.9 が通常の閾値

### ログイン方式

| 値 | 説明 |
|----|------|
| `password-only` | パスワードログインのみ（SSO プロバイダーを設定していても、ログインページに SSO ボタンは表示されない） |
| `oidc+password` | パスワード + SSO の両方に対応 |
| `oidc-only` | SSO のみ（パスワード入力は無効化。有効なプロバイダーが 1 つ以上、かつバインド済み管理者が 1 人以上必要） |

リカバリー（oidc-only で管理者がロックアウトされた場合）：
```bash
taps-panel reset-auth-method --to password-only --data-dir /var/lib/taps/panel
```

### SSO プロバイダー (OIDC)

[SSO / OIDC ドキュメント](sso-oidc.md)を参照してください。

### サーバーダウンロードソース

| 値 | 説明 |
|----|------|
| `fastmirror` | FastMirror ミラー（中国向け最適化） |
| `official` | Mojang / PaperMC 公式ソース（パネルから海外への直接アクセスが必要） |

### Minecraft Java 自動休止

[インスタンス管理 → 休止機能](instances.md)を参照してください。

### Webhook 通知

**Daemon ノード**（インスタンスではなく）の接続状態を監視します。Daemon がパネルから**60 秒以上連続して切断**された場合、`node.offline` を送信します。再接続時には `node.online` を送信します（以前に offline が送信された場合のみ）。

```json
{ "event": "node.offline", "timestamp": 1714000000, "payload": { "daemonId": 1, "name": "node-a", "address": "10.0.0.5:24445" } }
```

- **SSRF 防止**: ClassifyHost による 3 分類（public / private / DNS 失敗）+ DialContext での再検証。管理者は「プライベート/ループバックアドレスを許可」にチェックを入れると内部 Webhook を許可できます
- **allowPrivate**: Webhook の受信先が本当に内部の信頼済みネットワーク上にある場合にのみ有効にしてください

### ログ容量制限

`loglimit.Manager` が 60 秒ごとに audit_logs / login_logs の行数を確認し、超過分の古いレコードを削除します。

### レート制限

> カード名は「ログインレート制限」から「レート制限」に変更されました（2026-04-26）。対象範囲がより広いためです。

**認証レート制限**（3 つの独立バケットが閾値を共有）：
| 設定 | デフォルト | 範囲 | 説明 |
|------|---------|------|------|
| rateLimitPerMin | 5 | 1-100 | IP あたり 1 分間の失敗回数（login / changePw / apiKey はそれぞれ独立してカウント） |
| banDurationMinutes | 5 | 1-1440 | 閾値超過後の BAN 期間 |

**OAuth 開始レート制限**（PKCE ストアのフラッディング防止のための匿名エンドポイント）：
| 設定 | デフォルト | 範囲 |
|------|---------|------|
| oauthStartCount | 30 | 1-1000 |
| oauthStartWindowSec | 300 | 30-3600 |
| pkceStoreMaxEntries | 10000 | 100-1000000 |

**ターミナル WebSocket**（接続ごとのトークンバケット）：
| 設定 | デフォルト | 範囲 | 説明 |
|------|---------|------|------|
| terminalReadDeadlineSec | 60 | 10-600 | フレーム間（pong 含む）の最大アイドル時間 |
| terminalInputRatePerSec | 200 | 1-5000 | 1 秒あたりの入力フレーム許可数 |
| terminalInputBurst | 50 | 1-5000 | バースト予算（コマンド貼り付け用） |

**休止アイコン公開エンドポイント**：
| 設定 | デフォルト | 範囲 | 説明 |
|------|---------|------|------|
| iconCacheMaxAgeSec | 300 | 0-86400 | Cache-Control max-age |
| iconRatePerMin | 10 | 1-1000 | IP あたり 1 分間のリクエスト数 |

### リクエストサイズ制限

| 設定 | デフォルト | 範囲 | 説明 |
|------|---------|------|------|
| maxRequestBodyBytes | 128 KiB | 1 KiB - 4 MiB | グローバルリクエストボディ制限（Content-Length を先に確認し、超過時は 413 を返却） |
| maxJsonBodyBytes | 16 MiB | 1-128 MiB | fs/write など大きな JSON エンドポイント向け |
| maxWsFrameBytes | 16 MiB | 1-128 MiB | パネルターミナル WS フレーム制限 |

グローバル制限の対象外パス：`*/fs/write`, `*/files/upload*`, `*/brand/favicon`, `*/hibernation/icon`。

### Content Security Policy (CSP)

Content-Security-Policy は、ブラウザにスクリプトの読み込みと iframe の埋め込みを許可するドメインを指示します。`'self'` は常に含まれ、削除できません。

| 設定 | デフォルト | 説明 |
|------|---------|------|
| scriptSrcExtra | `https://challenges.cloudflare.com, https://www.recaptcha.net` | スクリプトの読み込みを許可する外部ドメイン |
| frameSrcExtra | `https://challenges.cloudflare.com, https://www.google.com, https://www.recaptcha.net` | iframe の埋め込みを許可する外部ドメイン |

生成される CSP ヘッダーの全文：
```
default-src 'self'; script-src 'self' <scriptSrcExtra...>; style-src 'self' 'unsafe-inline'; frame-src 'self' <frameSrcExtra...>; img-src 'self' data:; connect-src 'self' ws: wss:; font-src 'self'
```

- `style-src 'unsafe-inline'`: antd CSS-in-JS ランタイムの `<style>` タグ挿入に必要
- `connect-src ws: wss:`: ターミナル WebSocket 接続に必要

**その他のセキュリティヘッダー**（自動付与、設定変更不可）：
- `X-Frame-Options: SAMEORIGIN`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Strict-Transport-Security`: パネルが独自の TLS 証明書を持つ場合、またはリクエストに `X-Forwarded-Proto: https` が含まれる場合（nginx リバースプロキシ）にのみ送信

参照：`panel/internal/api/security_headers.go`

### セッション有効期間

| 設定 | デフォルト | 範囲 | 説明 |
|------|---------|------|------|
| jwtTtlMinutes | 60 | 5-1440 | JWT の有効期間。残り時間が TTL/2 未満になると自動更新 |
| wsHeartbeatMinutes | 5 | 1-60 | ターミナル WS が TokensInvalidBefore を再検証する間隔 |

### HTTP タイムアウト（Slow-Loris 対策）

`http.Server` の 4 つのタイムアウトパラメータ。WebSocket 接続は Hijack 後これらのタイムアウトの対象外になります。変更後は**パネルの再起動が必要**です。

| 設定 | デフォルト | 範囲 | 説明 |
|------|---------|------|------|
| readHeaderTimeoutSec | 10 | 1-3600 | 接続からヘッダー読み取り完了までの合計時間 |
| readTimeoutSec | 60 | 1-3600 | ボディを含む読み取り合計時間 |
| writeTimeoutSec | 120 | 1-3600 | ヘッダー読み取り完了からレスポンス書き込み完了まで |
| idleTimeoutSec | 120 | 1-3600 | Keep-alive のアイドル保持時間 |

---

## UI に含まれない設定

環境変数または Daemon の `config.json` で設定：

### パネル環境変数

| 変数 | 説明 | デフォルト |
|------|------|---------|
| `TAPS_DATA_DIR` | パネルデータディレクトリ | `./data` |
| `TAPS_WEB_DIR` | Web 静的ファイルディレクトリ | `./web` |
| `TAPS_ADDR` | リッスン host:port（DB のポート設定で上書き） | `:24444` |
| `TAPS_ADMIN_USER` / `TAPS_ADMIN_PASS` | 初回起動時のシードのみ | `admin` / `admin` |
| `TAPS_TLS_CERT` / `TAPS_TLS_KEY` | HTTPS を有効化（nginx を使用しない場合） | — |
| `TAPS_CORS_DEV` | `=1` で CORS ワイルドカードを開放（開発用） | — |

### Daemon 環境変数 / config.json

すべての環境変数は `<DataDir>/config.json` で上書き可能（優先順位：JSON > 環境変数 > デフォルト）。

| 変数 / JSON キー | 説明 | デフォルト | 範囲 |
|-----------------|------|---------|------|
| `TAPS_DAEMON_DATA` | Daemon データディレクトリ | `./data` | — |
| `TAPS_DAEMON_ADDR` / `addr` | リッスン host:port | `:24445` | — |
| `TAPS_REQUIRE_DOCKER` / `requireDocker` | Docker 以外のインスタンスを拒否 | `true` | bool |
| `TAPS_DAEMON_RL_THRESHOLD` / `rateLimitThreshold` | トークン検証失敗の閾値 | 10 | 1-1000 |
| `TAPS_DAEMON_RL_BAN_MINUTES` / `rateLimitBanMinutes` | BAN 期間 | 10 | 1-1440 |
| `TAPS_DAEMON_MAX_WS_FRAME_BYTES` / `maxWsFrameBytes` | WS フレーム制限 | 16 MiB | 1-128 MiB |
| `TAPS_DAEMON_WS_DISPATCH_CONCURRENCY` / `wsDispatchConcurrency` | セッションごとのディスパッチ同時実行数制限 | 8192 | 1-65536 |
| `TAPS_DAEMON_HTTP_READ_HEADER_TIMEOUT_SEC` / `httpReadHeaderTimeoutSec` | HTTP ヘッダー読み取りタイムアウト | 10 | 1-3600 |
| `TAPS_DAEMON_HTTP_READ_TIMEOUT_SEC` / `httpReadTimeoutSec` | HTTP ボディ読み取りタイムアウト | 60 | 1-3600 |
| `TAPS_DAEMON_HTTP_WRITE_TIMEOUT_SEC` / `httpWriteTimeoutSec` | HTTP 書き込みタイムアウト | 120 | 1-3600 |
| `TAPS_DAEMON_HTTP_IDLE_TIMEOUT_SEC` / `httpIdleTimeoutSec` | HTTP アイドルタイムアウト | 120 | 1-3600 |

Daemon は起動時にデータディレクトリへ `config.json.template` を自動書き出しします。サポートされるすべてのフィールドとデフォルト値が含まれており、管理者がコピーして編集できます。
