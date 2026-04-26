[English](../../api/overview.md) | [中文](../zh/api/overview.md) | **日本語**

# API 概要

パネルは RESTful および WebSocket インターフェースを提供しており、すべて `/api/` プレフィックスが付きます。

**ベース URL（本番環境の例）**: `https://taps.example.com`
**デフォルトポート**: 24444（HTTP、変更可能 / nginx 使用可）

## 認証

3 種類の認証情報から選択できます:

### 1. JWT Bearer トークン

ログイン後に取得し、`Authorization` ヘッダーに設定します:

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

- HS256、シークレットは `data/jwt.secret` に保存（初回起動時に自動生成）
- デフォルト有効期間 1 時間（システム設定で 5〜1440 分に変更可能）
- スライディング更新: 残り時間が TTL/2 未満の場合、レスポンスヘッダー `X-Refreshed-Token` に新しい JWT が含まれます
- パスワード変更 / ロール変更 / ユーザー削除後、旧 JWT は**即座に無効化**されます（HTTP 401 `auth.token_revoked`）
- `alg: none` 攻撃は明示的に拒否されます

### 2. クエリパラメータによる JWT

ブラウザでヘッダーを設定できないシナリオ（`<a href>` ダウンロード、フォームアップロード、WebSocket）でのみ使用します:

```
GET /api/daemons/1/files/download?token=<jwt>&path=/data/x.txt
```

Bearer と同様に動作し、`tokens_invalid_before` の失効チェックも含まれます。

### 3. API キー

`tps_` プレフィックス付きの固定認証情報で、Bearer ヘッダーを使用します:

```http
Authorization: Bearer tps_3fe3c349dd703a4c...
```

- 永続または有効期限付き。失効可能
- IP ホワイトリスト + スコープに対応
- 詳細は [API キー](../usage/api-keys.md) を参照

## エラー形式

すべてのエラーは統一された JSON 形式で返され、**安定したエラーコード**（`domain.snake_case` 形式）を含みます:

```json
{ "error": "auth.invalid_credentials", "message": "invalid credentials" }
```

一部のエラーにはパラメータが含まれます:

```json
{ "error": "auth.rate_limited", "message": "...", "params": { "retryAfter": 298 } }
```

```json
{ "error": "common.request_too_large", "message": "...", "params": { "maxBytes": 131072 } }
```

エラーコードはフロントエンドの i18n ルックアップに直接使用できます: `t('errors.' + error)`

### 一般的なステータスコード

| コード | 意味 |
|--------|------|
| 200 | 成功 |
| 400 | 不正なリクエスト / パラメータバリデーション失敗 |
| 401 | 認証情報が未設定 / 無効 / 失効 / 期限切れ |
| 403 | 認証済みだが権限不足（ロール / スコープ / インスタンス権限） |
| 404 | リソースが見つからない |
| 405 | メソッドが許可されていない（JSON ボディ `common.method_not_allowed`） |
| 409 | 競合（ユーザー名/メールアドレスの重複、アップロードパスが使用中など） |
| 410 | アップロードセッションの有効期限切れ |
| 413 | リクエストボディが大きすぎる |
| 429 | レート制限超過。レスポンスヘッダーに `Retry-After: <seconds>` が含まれます |
| 502 | デーモンに接続不可 / デーモンのアップストリームエラー |

## レート制限

| バケット | デフォルト閾値 | デフォルトBAN時間 | 設定場所 |
|----------|---------------|-------------------|----------|
| ログイン失敗 | 5回/分/IP | 5 分 | システム設定 → レート制限 |
| パスワード変更失敗 | 同上 | 同上 | 同上 |
| API キー失敗 | 同上 | 同上 | 同上 |
| OAuth 開始 | 30回/5分/IP | 5 分 | 同上 |
| デーモントークン失敗 | 10回/分/IP | 10 分 | デーモン設定 |

各失敗ごとに追加のスリープが発生します（指数バックオフ、最大 3 秒）。認証成功時にその IP の失敗カウントはクリアされます。

## リクエストボディサイズ

| エンドポイント | 制限 | 設定 |
|---------------|------|------|
| グローバル（免除対象を除く） | 128 KiB | システム設定 → リクエストサイズ制限 |
| `POST /daemons/:id/fs/write` | 16 MiB | 同上、maxJsonBodyBytes |
| `POST /daemons/:id/files/upload` 単一チャンク | 1 GiB | デーモンのハード制限 |
| `POST /settings/brand/favicon` | 64 KiB | ハードコーディング |
| `POST /settings/hibernation/icon` | 32 KiB | ハードコーディング |

WebSocket 単一フレーム ≤ 16 MiB（パネルシステム設定 / デーモン設定で個別に制御）。

## CORS

- 許可オリジン: システム設定 → CORS 許可オリジンで設定されたドメイン + パネル自身の publicUrl
- 許可ヘッダー: `Origin, Content-Type, Authorization`
- 許可メソッド: `GET, POST, PUT, DELETE, OPTIONS`
- **公開レスポンスヘッダー**: `X-Refreshed-Token, Retry-After, Content-Disposition`
- 開発環境: `TAPS_CORS_DEV=1` で一時的にワイルドカードを許可

## セキュリティヘッダー

すべてのレスポンスに自動的に含まれます:

| ヘッダー | 値 |
|----------|-----|
| Content-Security-Policy | `default-src 'self'; script-src 'self' + configurable whitelist; ...` |
| X-Frame-Options | `SAMEORIGIN` |
| X-Content-Type-Options | `nosniff` |
| Referrer-Policy | `strict-origin-when-cross-origin` |
| Strict-Transport-Security | HTTPS 接続時のみ送信 |

CSP の script-src / frame-src はシステム設定 → Content Security Policy (CSP) でホット設定可能です。

## TLS

- **パネル**: デフォルトは HTTP。HTTPS を使用するには `TAPS_TLS_CERT` + `TAPS_TLS_KEY` を指定。nginx リバースプロキシを推奨
- **デーモン**: HTTPS 必須（自己署名 99 年 ECDSA 証明書、パネルが SHA-256 フィンガープリントをピン留め）

## WebSocket エンドポイント

| パス | 用途 | 認証 |
|------|------|------|
| `GET /api/ws/instance/:id/:uuid/terminal` | リアルタイムターミナル | `?token=<jwt>` + PermView（読み取り専用）/ PermTerminal（読み書き） |

- オリジンチェック: パネルの公開 URL と一致する必要があります（未設定の場合は拒否）
- 読み取りタイムアウト + pong ハンドラー: 設定可能（デフォルト 60 秒）
- 入力トークンバケット: 設定可能（デフォルト 200/秒、バースト 50）

## パスパラメータ規約

- `:id` = ノード ID（uint）
- `:uuid` = インスタンス UUID（8-4-4-4-12 の 16 進数）
- `:taskId` = スケジュールタスク ID（uint）
- `:ref` = Docker イメージリファレンス（repository:tag、URL エンコード）

## 時刻形式

すべてのタイムスタンプは RFC 3339 形式です: `2026-04-23T18:55:07.020890690-04:00`
