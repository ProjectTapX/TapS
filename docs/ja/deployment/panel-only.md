[English](../../deployment/panel-only.md) | [中文](../zh/deployment/panel-only.md) | **日本語**

# Panel 単体デプロイ

Panel はコントロールプレーン兼 Web UI であり、Minecraft インスタンス自体は実行しません。軽量なホスト（クラウド VPS、自宅 NAS、内部管理サーバー）にスタンドアロンでデプロイし、wss 経由で複数のリモート Daemon に接続します。

## ユースケース

- マルチホストクラスタ: 1台の Panel で N台の Daemon（各 Daemon がゲームサーバーを実行）を管理
- Panel をパブリックインターネットに配置し、すべての Daemon を内部ネットワークに配置（Panel から Daemon へアウトバウンド接続）
- Panel + nginx/Caddy をフロントエンドマシンに配置し、コンピュートノードはワークロード専用

## 前提条件

- Linux x86_64（Panel は Docker に依存しません）
- TCP **24444** を開放（HTTP。nginx リバースプロキシ設定後は 443 のみ公開）
- Panel ホストから各 Daemon のポート 24445 に**能動的に**接続できること（wss アウトバウンド）

## 1. ディレクトリの準備

```bash
mkdir -p /opt/taps /var/lib/taps/panel
chmod 700 /var/lib/taps/panel
```

## 2. バイナリと Web アセットの配置

```bash
mv panel-linux-amd64 /opt/taps/panel
chmod +x /opt/taps/panel
mkdir -p /opt/taps/web
tar -xzf web.tar.gz -C /opt/taps/web
```

## 3. systemd ユニット

```bash
cat >/etc/systemd/system/taps-panel.service <<'EOF'
[Unit]
Description=TapS Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/taps/panel
WorkingDirectory=/opt/taps
Environment=TAPS_DATA_DIR=/var/lib/taps/panel
Environment=TAPS_WEB_DIR=/opt/taps/web
Environment=TAPS_ADDR=:24444
Environment=TAPS_ADMIN_USER=admin
Environment=TAPS_ADMIN_PASS=admin
# Panel で直接 HTTPS を有効にする場合（nginx を使わない場合）、以下のコメントを解除して証明書を指定:
# Environment=TAPS_TLS_CERT=/etc/letsencrypt/live/example.com/fullchain.pem
# Environment=TAPS_TLS_KEY=/etc/letsencrypt/live/example.com/privkey.pem
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now taps-panel
```

確認：

```bash
systemctl status taps-panel
ss -lnt | grep 24444
```

## 4. 初回ログインとパスワード変更

`http://<Panel ホスト>:24444/` にアクセスし、admin/admin でログイン → パスワード変更が強制されます。

## 5. リモート Daemon ノードの追加

Panel ホストから Daemon ホストに到達できることを確認します：

```bash
# Panel ホストから接続テスト
nc -zv <Daemon ホスト> 24445
# 必要に応じて openssl で Daemon のフィンガープリントを事前に確認
echo | openssl s_client -connect <Daemon ホスト>:24445 2>/dev/null | openssl x509 -fingerprint -sha256 -noout
```

Panel の **「ノード管理」** → **「追加」** に進みます：

| フィールド | 値 |
|---|---|
| 名前 | `node-1` |
| アドレス | `<Daemon ホスト>:24445` |
| 表示ホスト | プレイヤー向けの外部ドメイン/IP |
| ポート範囲 | 25565-25600 |
| トークン | Daemon ホストで取得: `cat /var/lib/taps/daemon/token` |

**「フィンガープリント取得」** をクリック → 確認（Daemon 起動ログと一致すること） → **「承認して使用」** → **「保存」**。

追加の Daemon についても同様に繰り返します。

## 次のステップ

- Daemon を単体でデプロイする方法: [Daemon 単体デプロイ](daemon-only.md)
- nginx リバースプロキシで HTTPS を追加: [Nginx リバースプロキシ + HTTPS](nginx-https.md)
- ノードグループ（インスタンス作成時に最も負荷の低いノードを自動選択）: Panel の「ノードグループ」ページで設定

---

## Panel データディレクトリの内容

| ファイル | 自動生成 | 説明 |
|---|---|---|
| `panel.db` | はい | SQLite、すべてのビジネスデータを格納。GORM AutoMigrate がテーブルの作成とカラムの追加を自動実行 |
| `jwt.secret` | はい | 96文字の16進数。削除するとすべての JWT が即座に無効化されます |

`config.json` の概念はありません（Panel の設定はすべて環境変数とシステム設定 DB で管理されます）。
