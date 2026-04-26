[English](../../deployment/daemon-only.md) | [中文](../zh/deployment/daemon-only.md) | **日本語**

# Daemon 単体デプロイ

既存の Panel に新しいホストを追加します。Daemon は Minecraft コンテナを実際に実行するエージェントです。

## ユースケース

- 既存の Panel にゲームサーバーノードを追加する
- Daemon をデータセンター / 自宅ネットワーク / 海外 VPS で実行し、リモートの Panel から一元管理する

## 前提条件

- Linux x86_64
- Docker インストール済み（`docker version` が動作すること）
- root 権限
- 受信ポートの開放：
  - **24445**（Daemon HTTPS / wss、Panel からの接続用）
  - インスタンス用ポート（Minecraft 25565+ など）
- 送信要件（設定により異なる）：
  - Docker イメージの取得（docker.io、ghcr.io など）
  - サーバー jar のダウンロード元（fastmirror / papermc.io）

## 1. 準備

```bash
mkdir -p /opt/taps /var/lib/taps/daemon
mv daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/daemon
```

## 2. systemd ユニット

```bash
cat >/etc/systemd/system/taps-daemon.service <<'EOF'
[Unit]
Description=TapS Daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/taps/daemon
WorkingDirectory=/opt/taps
Environment=TAPS_DAEMON_DATA=/var/lib/taps/daemon
Environment=TAPS_DAEMON_ADDR=:24445
Environment=TAPS_REQUIRE_DOCKER=true
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now taps-daemon
```

## 3. トークンと TLS フィンガープリントの取得

Daemon は初回起動時にこれらを自動生成します：

```bash
echo "Token:"
cat /var/lib/taps/daemon/token

echo "TLS Fingerprint:"
journalctl -u taps-daemon -n 30 --no-pager | grep "tls fingerprint"
```

この2つの値を控えてください。Panel への追加時に必要です。

## 4. Panel でノードを追加

既存の Panel の **「ノード管理」** → **「追加」** に進みます：

| フィールド | 値 |
|---|---|
| 名前 | 任意（例: `bj-1`） |
| アドレス | `<Daemon のパブリック IP>:24445` または `<内部 IP>:24445` |
| 表示ホスト | プレイヤーが接続するドメイン/IP |
| ポート範囲 | 25565-25600 またはカスタム |
| トークン | 手順 3 で取得したトークン |

**「フィンガープリント取得」** をクリック → 表示されたフィンガープリントが手順 3 の出力と**完全に一致する**ことを確認 → **「承認して使用」** → **「保存」**。

ノード一覧に「接続済み」と表示されれば、通信リンクが確立されています。

---

## Daemon データディレクトリの詳細

| ファイル / ディレクトリ | 自動生成 | 説明 |
|---|---|---|
| `token` | はい | 32バイトのランダム16進数。Panel と Daemon 間の共有シークレット |
| `cert.pem` / `key.pem` | はい | ECDSA P-256 自己署名証明書（有効期間99年）。Panel が SHA-256 フィンガープリントをピン留め |
| `files/` | はい | インスタンス作業ディレクトリのルート（UUID ごとにサブディレクトリ） |
| `backups/` | はい | バックアップ zip の保存先 |
| `volumes/` | はい | マネージドボリュームのマウントポイント（ディスククォータ用ループバック img） |
| `volumes/<name>.img` | オンデマンド | ボリューム作成時に生成 |
| `volumes/<name>/` | オンデマンド | ボリュームのマウントポイント |
| `config.json.template` | はい | 設定例。**起動のたびにバージョンと同期するため上書きされます** |
| `config.json` | いいえ | 管理者が編集する任意のファイル。環境変数設定を上書き |

## 主な操作

### Daemon トークンのローテーション

```bash
# Daemon ホスト上で実行
rm /var/lib/taps/daemon/token
systemctl restart taps-daemon
cat /var/lib/taps/daemon/token   # 新しいトークン

# Panel UI でノードを編集 → トークンフィールドを更新 → 保存
```

### Daemon TLS 証明書のローテーション

```bash
# Daemon ホスト上で実行
rm /var/lib/taps/daemon/{cert,key}.pem
systemctl restart taps-daemon
journalctl -u taps-daemon -n 10 | grep "tls fingerprint"

# Panel UI でノードを編集 → 「フィンガープリント取得」をクリック → 新しいフィンガープリントを確認 → 「承認して使用」 → 保存
```

### Daemon 設定の変更（ポート / レート制限 / WS フレームサイズ）

`config.json` をコピーして編集します：

```bash
cd /var/lib/taps/daemon
cp config.json.template config.json
vim config.json   # 上書きしないフィールドは削除し、変更するフィールドのみ残す
systemctl restart taps-daemon
journalctl -u taps-daemon -n 5 | grep "applied overrides"
```

対応フィールド（`config.json`）：

```json
{
  "addr":                ":24445",
  "requireDocker":       true,
  "rateLimitThreshold":  10,
  "rateLimitBanMinutes": 10,
  "maxWsFrameBytes":     16777216
}
```

優先順位: **config.json > 環境変数 > デフォルト値**

## セキュリティに関する注意事項

- **Daemon をパブリックネットワークでリッスンさせることは root 権限と同等です**: トークンを持つ者は誰でもホストパスをマウントしたコンテナを作成できます。本番環境での推奨事項：
  - `addr` を `127.0.0.1:24445` に変更し、SSH トンネルまたは Tailscale 経由で Panel に公開する
  - またはクラウドファイアウォールで 24445 のソース IP を Panel ホストのみに制限する
- **トークンファイルのパーミッションは 0600**、root のみ読み取り可能
- **TLS フィンガープリントのピン留め**により Daemon の中間者攻撃による差し替えを防止 -- ノード初回追加時にフィンガープリントの検証が必須
