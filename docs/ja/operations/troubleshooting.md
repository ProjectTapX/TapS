[English](../../operations/troubleshooting.md) | [中文](../../zh/operations/troubleshooting.md) | **日本語**

# トラブルシューティング

## Panel が起動しない

```bash
systemctl status taps-panel
journalctl -u taps-panel -n 50 --no-pager
```

| 症状 | 原因 | 対処法 |
|---|---|---|
| `bind: address already in use` | ポートが使用中 | `ss -lntp \| grep 24444` でプロセスを特定し、Panel のポートを変更（システム設定または環境変数） |
| `database is locked` | 別の Panel プロセスが稼働中 / SQLite ファイルロックの残留 | `ps aux \| grep panel` で残留プロセスを kill。`panel.db-shm` `panel.db-wal` を削除 |
| `jwt.secret: permission denied` | ファイル権限が不正 | `chown root:root /var/lib/taps/panel/jwt.secret && chmod 600` |
| `panel listening on :2444`（桁が違う） | DB で `system.panelPort` が誤って変更された | `sqlite3 panel.db "UPDATE settings SET value='24444' WHERE key='system.panelPort'"` |

## Daemon が起動しない

| 症状 | 原因 | 対処法 |
|---|---|---|
| `tls cert: ...` | cert/key ファイルが破損 | `cert.pem` `key.pem` を削除して再起動。その後 Panel でフィンガープリントを再取得 |
| `docker daemon not running` | Docker が起動していない | `systemctl start docker` |
| `bind: permission denied` | root 以外で 1024 未満のポートを使用 | systemd ユニットで `User=root` を指定 |

## Panel でノードが「オフライン」と表示される

```bash
# Panel ホスト上で実行
nc -zv <daemon-host> 24445       # ポートに到達可能か？
journalctl -u taps-panel -n 50 | grep daemon
```

| エラー | 原因 | 対処法 |
|---|---|---|
| `tls handshake: tls: failed to verify certificate` | TOFU で保存されたフィンガープリント ≠ Daemon の実際のフィンガープリント | Panel UI: ノードを編集 → フィンガープリントを取得 → 承認 → 保存 |
| `dial: ... connection refused` | Daemon が稼働していない / ファイアウォールがブロック | Daemon ホスト上で: `systemctl status taps-daemon` |
| `not pinned` | ノード行に certFingerprint がない | UI: 編集 → フィンガープリントを取得 → 承認 |

## インスタンスが起動しない

```bash
# Daemon ホスト上で実行
docker ps -a | grep taps-
docker logs taps-<uuid>
```

よくある原因:
- `OCI runtime create failed: ... mounts: ... no such file or directory` → 作業ディレクトリが削除された。Panel UI で再デプロイするかディレクトリを修復
- `Address already in use` → インスタンスのホストポートが別のプロセスに使用されている。インスタンス設定でポートを変更
- `pull access denied` → イメージ名が間違っている / レジストリに到達できない
- `EULA must be accepted` → 初回のみ。Daemon は itzg 環境で EULA=TRUE を自動書き込み。カスタム Docker インスタンスでは手動設定が必要

## ターミナルに接続できない

| 症状 | 調査方法 |
|---|---|
| WebSocket 接続が即座に 401 を返す | `?token=` が期限切れ（失効 / パスワード変更済み）→ 再ログイン |
| フロントエンドのターミナルでスピナーが表示され続ける | nginx が `Upgrade` ヘッダーを転送していない。[nginx 設定](../deployment/nginx-https.md)を確認 |
| ターミナルが5分後に切断される | 管理者によりトークンが失効された。再ログインが必要 |

## アップロードの失敗

| エラーコード | 原因 |
|---|---|
| 413 `request_too_large` | `init` エンドポイントがグローバル 128 KiB 制限でブロック？ nginx の `client_max_body_size` が 1100M 以上か確認 |
| 507 `quota_exceeded` | ファイル + 使用量がボリュームの残り容量を超過。ボリュームを拡張するかクリーンアップ |
| 410 unknown or expired uploadId | 単一チャンクのアップロードが最終処理なしに1時間を超過。クライアントはアップロード全体を再試行する必要あり |
| 400 missing uploadId | クライアントが先に `/upload/init` を呼び出していない。フロントエンドのバージョンが古い可能性、ページを更新 |
| 400 path does not match init declaration | アップロードセッションのパスとチャンクのパスフィールドが一致しない |

## レート制限 429

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 298
{"error":"rate_limited","retryAfter":298}
```

- ログイン / パスワード変更 / API キーの失敗が累積して閾値に到達（デフォルト 5回/分）
- Retry-After の秒数だけ待機
- 緊急時: `systemctl restart taps-panel` で全てのインメモリカウンターがクリアされる

## リバースプロキシ配下ですべてのクライアント IP が 127.0.0.1 と表示される

考えられる原因（トラブルシューティングの優先順）:

1. **nginx が実 IP ヘッダーを転送していない**: nginx サイト設定に以下の3行があるか確認:
   ```nginx
   proxy_set_header X-Real-IP         $remote_addr;
   proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
   proxy_set_header X-Forwarded-Proto $scheme;
   ```
   これらのいずれかが欠けていると、Panel が実際のクライアント IP を取得できません。

2. **Panel の信頼済みプロキシリストが未設定**: nginx が `X-Forwarded-For` を送信しても、gin はデフォルトでそのヘッダーを信頼しません。「システム設定」→「信頼済みプロキシリスト」→ nginx ホストの IP を追加（localhost のデフォルト `127.0.0.1, ::1` は同一マシン上の nginx をカバー済み）→ 保存。

3. **Panel が再起動されていない**: `SetTrustedProxies` は起動時にのみ有効になります。保存後に `systemctl restart taps-panel` が必要です。

4. **nginx がリモートホスト上にある**: 信頼済みリストに `127.0.0.1, ::1` しかないが、nginx が別のマシン（例: `10.0.0.5`）で稼働している → nginx の IP を追加。

**検証方法**: Panel にアクセスした後、「ログインログ」の IP 列を確認 — `127.0.0.1` ではなく実際のパブリック IP が表示されているはずです。

## SQLite のサイズ肥大化

```bash
ls -lh /var/lib/taps/panel/panel.db
sqlite3 /var/lib/taps/panel/panel.db "VACUUM;"
```

- ログ上限を確認:「システム設定」→「ログ容量制限」— 設定が高すぎないか？
- audit_logs / login_logs の行数を確認: `sqlite3 panel.db "SELECT COUNT(*) FROM audit_logs"`

## 「トークンが失効されました」が繰り返し表示される

考えられる原因: 管理者がロール / パスワードを頻繁に変更している → 変更のたびに `tokens_invalid_before` が更新される → 以前に発行されたすべての JWT が一括無効化される。

正常な動作であり、対処は不要です。ユーザーは一度再ログインするだけで解決します。

## 自動ハイバネーション: プレイヤーが参加できない

- プレイヤーがクライアントで接続 → 「サーバーを起動中です。約30秒後に再接続してください」というキックメッセージが表示されるはず
- Daemon がバックグラウンドで実コンテナを起動（`journalctl -u taps-daemon -n 30` で確認）
- `warmupMinutes` の経過を待つ → プレイヤーが再接続 → 参加可能

それでもプレイヤーが参加できない場合:
- インスタンスログを確認: 起動に失敗 / EULA / ポート競合がないか
- Daemon ログを確認: ハイバネーションマネージャーがエラーを報告していないか

## ログの場所

```bash
# Panel
journalctl -u taps-panel -f
journalctl -u taps-panel -n 200 --no-pager

# Daemon
journalctl -u taps-daemon -f

# インスタンスコンテナ
docker logs -f taps-<uuid>

# 監査 / ログインログ（ログインが必要）
ブラウザ → Panel → ユーザー管理 → 監査ログ / ログインログ
```

## データベースのロック

Panel プロセスがパニックした場合、`panel.db-shm` `panel.db-wal` が残る可能性があります:

```bash
systemctl stop taps-panel
sqlite3 /var/lib/taps/panel/panel.db ".quit"   # WAL を統合
systemctl start taps-panel
```

それでも解決しない場合: 閉じられていない `sqlite3` シェルが開いていないか確認してください。

## ここに記載されていない問題

Panel と Daemon 両方で `journalctl -f` を開き、問題を再現してログを開発者 / Issue トラッカーに貼り付けてください。

## CORS

監視ツール / ヘルスチェック / サードパーティダッシュボードが `/api/*` から期待されるデータではなく **403 Forbidden** を受信する場合 — CORS によるブロックの可能性が高いです。

**症状**:
- ブラウザの DevTools で `Access-Control-Allow-Origin: *` ヘッダーがなく、リクエストが SOP によりブロックされている
- curl で `-H 'Origin: https://yourtool.example.com'` を指定すると `HTTP/1.1 403` が返る
- Panel ログに GIN レスポンスコード 403 が表示される

**原因**: CORS は「システム設定 → CORS 許可オリジン」に登録されたオリジンのみを許可します。ホワイトリストが空の場合、Panel のパブリック URL（publicUrl）のみが許可されます。`Origin` ヘッダーを持つリクエストのオリジンがリストにない場合、ACAO ヘッダーが返されず、ブラウザがリクエストを拒否します。

**対処法**（いずれかを選択）:
1. **ホワイトリストに追加（推奨）**: Panel にログイン → システム設定 → CORS 許可オリジン → 監視ツールのオリジン（`https://prometheus.internal`、`https://uptime.example.com` など）を追加 → 保存。即座に反映され、再起動不要
2. **一時的な開発モード**: systemd ユニットに `Environment=TAPS_CORS_DEV=1` を追加して Panel を再起動 — ワイルドカード CORS が有効になります。**開発専用、本番では絶対に使用しないでください**
3. **Origin ヘッダーの回避**: 監視ツールが Origin ヘッダーを送信しないよう設定可能な場合（ほとんどの cURL ベースのツールはデフォルトで送信しない）、CORS のトリガー自体を回避できます

**補足**:
- Origin ヘッダーなしの API キーによるサーバー間通信は **CORS の影響を全く受けません** — ほとんどの自動化シナリオではデフォルトでこの方式です
- ブラウザ JS からの Panel API へのクロスオリジンアクセス（例: Panel を iframe に埋め込む、サードパーティ SPA から Panel API を呼び出す）は **必ず** ホワイトリストを使用する必要があります
- Panel 独自の SPA は同一オリジンで動作し、そのリクエストの Origin は publicURL と一致するため → 常に許可され、追加設定は不要です
