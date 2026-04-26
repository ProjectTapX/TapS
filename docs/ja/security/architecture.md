[English](../../security/architecture.md) | [中文](../../zh/security/architecture.md) | **日本語**

# セキュリティアーキテクチャ

## 全体モデル

```
Browser ──HTTPS──▶ [nginx/Caddy] ──HTTP──▶ Panel (:24444)
                                           │  wss + TLS fingerprint pin
                                           ▼
                                        Daemon (:24445, self-signed TLS)
                                           │
                                        Docker Engine
```

Panel がすべての認証・認可の判断の中心です。Daemon は Panel の共有トークンのみを信頼します。ブラウザは JWT で Panel に認証し、Panel は TLS + 共有トークンで Daemon に認証します。

---

## 防御レイヤー

### HTTP セキュリティヘッダー

すべてのレスポンスに自動的に含まれます（`panel/internal/api/security_headers.go` 参照）:

| ヘッダー | 値 | 目的 |
|--------|-------|---------|
| Content-Security-Policy | `default-src 'self'; script-src 'self' + 設定可能ホワイトリスト; ...` | XSS 外部スクリプト注入の防止 |
| X-Frame-Options | `SAMEORIGIN` | クリックジャッキングの防止 |
| X-Content-Type-Options | `nosniff` | MIME スニッフィング攻撃の防止 |
| Referrer-Policy | `strict-origin-when-cross-origin` | Referer 漏洩の防止 |
| Strict-Transport-Security | `max-age=31536000; includeSubDomains`（HTTPS のみ） | HTTPS の強制 |

CSP の script-src / frame-src ホワイトリストは管理パネルでホットコンフィグ可能です（即座に反映、再起動不要）。

### 認証

| 対策 | 説明 |
|---------|-------------|
| JWT HS256 | ランダムシークレット（`jwt.secret` ファイル、初回起動時に生成） |
| bcrypt cost 10 | パスワードハッシュ |
| ダミーハッシュによるタイミング均一化 | 存在しないユーザーでも1回の bcrypt 比較を実行し、ユーザー名列挙のためのタイミング攻撃を防止 |
| スライディング更新 | JWT の残り時間が TTL/2 未満の場合、`X-Refreshed-Token` レスポンスヘッダーで新しいトークンを自動発行 |
| トークン失効 | `TokensInvalidBefore` フィールド。パスワード変更 / 管理者降格時に現在の iat-1s に設定 |
| MustChangePassword | 初回ログイン時のパスワード変更強制 |
| `alg: none` 拒否 | jwt-go の ParseToken が none アルゴリズムを明示的に拒否 |

### 認可

| レイヤー | 実装 |
|-------|---------------|
| ロール | admin / user、`auth.RequireRole()` ミドルウェア |
| インスタンスごとの権限 | PermView / PermControl / PermTerminal / PermFiles |
| API キースコープ | `RequireScope()` ミドルウェア、カンマ区切りのスコープタグ |

### SSO / OIDC

| 対策 | 説明 |
|---------|-------------|
| PKCE サーバー側ストア | Verifier は URL に含めず、Panel プロセスメモリに保存（10分 TTL） |
| HMAC state | provider + nonce + expiry + HMAC-SHA256 署名 |
| Nonce バインディング | id_token.nonce が state 内の nonce と一致する必要あり |
| Email ToLower | 入力時に小文字化し、管理者自動バインドガードのケースバリエーションバイパスを防止 |
| 管理者自動バインド拒否 | 既存の管理者メールアドレスを持つローカルアカウントは IdP 自動バインドを拒否 |
| メールドメインホワイトリスト | プロバイダごとに設定可能な許可ドメインリスト |
| CallbackError 型コード | URL フラグメントは安定したコードのみを渡し、IdP 内部エラーをブラウザに漏洩しない |
| clientSecret 暗号化保存 | AES-GCM による保存時暗号化 |

### 入力バリデーション

| バリデーション | 場所 |
|------------|----------|
| ValidImage 正規表現 + `--` セパレータ | Docker CLI フラグインジェクションの防止 |
| validInstanceUUID | すべての `taps-<uuid>` docker コマンド実行前 |
| validBackupName 正規表現 | バックアップファイル名 |
| validSiteName 文字ホワイトリスト | ブランド名 |
| normalizeEmail / normalizeUsername | 統一的な小文字化 + トリム |
| LOWER() ユニークインデックス | SQLite ユニークインデックスが `lower()` 関数を使用 |

### パスセキュリティ（ファイル操作）

| 対策 | 説明 |
|---------|-------------|
| fs.Resolve EvalSymlinks | 二重シンボリックリンク解決 + 包含チェック |
| containedIn dual-root | バックアップ復元先は instancesRoot または volumesRoot 配下である必要あり |
| Zip/Copy シンボリックリンク包含 | EvalSymlinks → マウント内であれば追跡、外部に逸脱した場合はスキップ + ログ |
| O_NOFOLLOW | Zip 展開 / バックアップ復元でファイルオープン時に nofollow フラグを使用 |
| isProtectedBackingFile | `.img` / `.json` ボリュームバッキングファイルへの直接 fs 操作を拒否 |
| Zip エントリ拒否 | シンボリックリンクエントリ / 先頭の `/` / `..` セグメントを拒否 |

### SSRF 防御

| シナリオ | 対策 |
|----------|----------|
| Webhook URL | ClassifyHost 三分類（public / private / DNS-failed）+ DialContext 再チェック |
| SSO テスト | 同上 + DNS リバインディング対策の SafeHTTPClient |

### データ保護

| 対策 | 対象 |
|---------|----------|
| AES-GCM 保存時暗号化 | Captcha シークレット、SSO clientSecret |
| 独立キー | sso-state.key は jwt.secret から独立 |
| bcrypt | ユーザーパスワード |
| crypto/rand | すべての乱数生成 |

### DoS 防御

| 対策 | 設定 |
|---------|--------------|
| IP ごとのレート制限（login / changePw / apiKey） | レート制限カード |
| OAuth-start バジェット | レート制限カード |
| PKCE ストア maxEntries | レート制限カード |
| WS ディスパッチセマフォ 8192 | Daemon 設定 |
| WS フレームサイズ上限 | リクエストサイズ制限 / Daemon 設定 |
| HTTP サーバータイムアウト | HTTP タイムアウトカード / Daemon 設定 |
| リクエストボディ上限 | リクエストサイズ制限カード |
| ハイバネーションアイコンキャッシュ + レート制限 | レート制限カード |

### トランザクション一貫性

以下の複数キー設定の書き込みはすべて `db.Transaction` でラップされています:
- SetCaptchaConfig, SetLimits, SetAuthTimings, SetRateLimit, SetHTTPTimeouts
- daemon.Delete（InstancePermission / Task / NodeGroupMember をカスケード削除）
- groups.Delete（NodeGroupMember をカスケード削除）
- User.Update / User.Delete（clause.Locking）

### フロントエンドセキュリティ

| 対策 | 説明 |
|---------|-------------|
| i18next escapeValue: true | グローバル HTML エスケープ |
| CSP script-src 'self' | 実行可能なスクリプトソースを制限 |
| 926 i18n キー整合 | zh / en / ja 完全一致 |
| 統一エラーコード | バックエンド apiErr(code, msg)、フロントエンド formatApiError が自動的に i18n を検索 |
| Partialize persist | zustand は token + {id, username, role} のみを永続化 |
| ターミナルトークン再読み込み | WS 再接続ごとに最新トークンを再読み込み |
| waitFor タイムアウト | Captcha SDK 読み込みの5秒タイムアウト |
| ChunkErrorBoundary | getDerivedStateFromError が null を返す（throw しない） |

### 運用セキュリティ

| 対策 | 説明 |
|---------|-------------|
| グレースフルシャットダウン | SIGTERM → srv.Shutdown(30s) → hib.Shutdown → vm.UnmountAll |
| systemd TimeoutStopSec=30s + KillSignal=SIGTERM | グレースフルシャットダウンと連携 |
| MountAll 同期 | Daemon はリクエスト受付前にすべてのループバックマウントの完了を待機 |

---

## 監査履歴

2026年4月26日時点で、6回の手動/AI 監査を実施し、累計99件の修正を行いました。現在の評価: **A**（Critical 0件 / High 0件 / Medium 0件 / Low 0件の未解決脆弱性）。
