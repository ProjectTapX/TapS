[English](../../deployment/nginx-https.md) | [中文](../zh/deployment/nginx-https.md) | **日本語**

# Nginx リバースプロキシ + HTTPS

Panel を nginx の背後に配置し、Let's Encrypt 証明書で HTTPS を有効にします。**Daemon 側の変更は不要です**（Daemon は既に wss + 自己署名証明書 + フィンガープリントピン留めを使用しており、Panel が TLS 接続を開始します）。

## アーキテクチャ

```
Browser ──https──> nginx(443) ──http──> panel(127.0.0.1:24444)
                                          │
                                          └──wss──> daemon(*:24445)  [self-signed + fingerprint pin]
```

## 1. Panel をループバックのみにバインド

nginx を経由せずにポート 24444 へ直接アクセスされることを防ぎます。2つの方法があります：

- **systemd ユニットを編集**: `Environment=TAPS_ADDR=127.0.0.1:24444` → `systemctl restart taps-panel`
- **またはシステム設定で**: 「Panel リッスンポート」は現在ポートのみの設定（ホスト指定なし）のため、ループバックへの固定には前者を使用してください

## 2. nginx + certbot のインストール

```bash
apt-get install -y nginx certbot python3-certbot-nginx
```

## 3. nginx サイト設定

```bash
cat >/etc/nginx/sites-available/taps <<'EOF'
upstream taps_panel {
    server 127.0.0.1:24444;
    keepalive 16;
}

# WebSocket アップグレード検出: クライアントが Upgrade ヘッダーを
# 送信した場合のみ Connection: upgrade を設定。
# 通常の HTTP リクエストには Connection: close を使用。
# すべてのリクエストに "upgrade" を固定すると一部の CDN/プロキシで問題が発生する可能性があります。
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    listen [::]:80;
    server_name taps.example.com;
    # certbot がこのブロックを変更します。ACME を許可し、それ以外はすべて HTTPS に 301 リダイレクト
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name taps.example.com;

    # certbot がこの2行を自動的に挿入します
    # ssl_certificate     /etc/letsencrypt/live/taps.example.com/fullchain.pem;
    # ssl_certificate_key /etc/letsencrypt/live/taps.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    # TLS パフォーマンス: セッションキャッシュ + OCSP ステープリング
    ssl_session_cache   shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_stapling        on;
    ssl_stapling_verify on;

    # アップロードチャンクは最大 1 GiB（Daemon 側の制限）+ メタデータの余裕分
    client_max_body_size 1100M;

    # タイムアウト設定
    proxy_connect_timeout 10s;       # localhost から Panel へは十分。リモートプロキシの場合は増加
    proxy_read_timeout    3600s;     # WebSocket 長時間接続 + SSE ストリーミング進捗
    proxy_send_timeout    3600s;     # 大容量ファイルアップロード

    # gzip: Panel の JSON / JS / CSS を nginx 層で圧縮
    gzip              on;
    gzip_vary         on;
    gzip_min_length   256;
    gzip_proxied      any;
    gzip_types        text/plain application/json application/javascript text/css
                      application/xml text/xml image/svg+xml;

    # セキュリティヘッダー
    # Panel は CSP / X-Frame-Options / nosniff / Referrer-Policy を既に含んでいます。
    # Panel は X-Forwarded-Proto: https を検出すると自動的に HSTS を追加します。
    # 以下のヘッダーは冗長ですが無害です。Panel 組み込みヘッダーのみを使用したい場合は削除してください。
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    location / {
        proxy_pass http://taps_panel;
        proxy_http_version 1.1;

        # 必須: Panel が X-Forwarded-For から実際のクライアント IP を解決するために必要
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 必須: ターミナル WebSocket アップグレード（map 変数を使用。非 WS リクエストは close）
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        # ストリーミングダウンロード / SSE / 大容量ファイルアップロード用にバッファリングを無効化
        proxy_buffering         off;
        proxy_request_buffering off;
    }
}
EOF

ln -s /etc/nginx/sites-available/taps /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx
```

## 4. 証明書の取得

```bash
certbot --nginx -d taps.example.com --redirect --agree-tos -m you@example.com
# certbot が ssl_certificate 行と 80→443 リダイレクトを自動挿入します
systemctl reload nginx
```

## 5. Panel に nginx を信頼させる

この手順を行わないと、**Panel からはすべてのクライアント IP が 127.0.0.1 に見え**、レート制限・監査ログ・API キーの IP ホワイトリストが正しく機能しません。

**「システム設定」** → **「信頼済みプロキシリスト」** に進みます：

- デフォルトの `127.0.0.1, ::1` はローカル nginx の構成をカバーしているため、**変更不要です**
- nginx が別のホストにある場合: nginx サーバーの IP を追加（例: `127.0.0.1, ::1, 10.0.0.5`）
- CIDR 表記対応: `127.0.0.1, ::1, 10.0.0.0/24`

保存 → **Panel を再起動**: `systemctl restart taps-panel`

> **再起動しないと反映されません** -- `gin.Engine.SetTrustedProxies()` は起動時に一度だけ適用されます。

## 6. 確認

`https://taps.example.com/` を開きます：
- ブラウザに鍵アイコンが表示される
- ログインが正常に動作する
- **「監査ログ」** / **「ログインログ」** に実際のパブリック IP が表示され、`127.0.0.1` では**ない**こと

## 7. ファイアウォールの整理

```bash
# 受信は 80 / 443 のみ許可。24444 はループバックのみ（Panel のホストバインドで対応済み）
ufw allow 22,80,443/tcp
ufw deny  24444/tcp
ufw deny  24445/tcp   # Daemon が同一マシンの場合、Panel は 127.0.0.1 を使用するため外部アクセスは不要
ufw enable
```

Daemon が**別のマシン**にある場合、Daemon ホストは Panel ホストからの 24445 受信を開放しておく必要があります。

---

## FAQ

### 大容量ファイルのアップロードで 413 Request Entity Too Large が返される
nginx のデフォルトのボディサイズ制限は 1 MiB です。上記の設定では `client_max_body_size 1100M;` を指定し、最大 1 GiB のチャンク + メタデータの余裕分をカバーしています。それでも 413 が発生する場合は、`nginx.conf` のグローバル `http {}` ブロックにより小さい `client_max_body_size` が設定されていないか確認してください。

### ターミナルの WebSocket が接続できない
- nginx サイト設定に `map $http_upgrade` + `proxy_set_header Upgrade / Connection` の3点セットが必要
- `proxy_read_timeout` を十分に大きくする必要があります（デフォルトの 60s では不十分。上記では 3600s に設定）
- Panel のパブリック URL が設定されていない場合、Panel は WS アップグレードを拒否します（503 `settings.public_url_required` を返します）

### 信頼済みプロキシリストを更新したが Panel に 127.0.0.1 が表示される
- Panel を再起動したか確認: `systemctl restart taps-panel`
- nginx が実際に `X-Forwarded-For` を送信しているか確認: `curl -sD - https://taps.example.com/api/healthz | grep -i x-`

### Daemon も nginx を経由させるべきか
**不要です**。Daemon は既に HTTPS（自己署名証明書 + フィンガープリントピン留め）を使用しています。間に nginx を挟むと wss のフォワーディングやフィンガープリントチェーンの再構築が必要になります。Panel から Daemon への直接接続が最もシンプルです。
