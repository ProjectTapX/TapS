[English](../../deployment/single-host.md) | [中文](../zh/deployment/single-host.md) | **日本語**

# シングルホストデプロイ（Panel + Daemon を同一マシンに構築）

Panel と Daemon を同じ Linux ホストにインストールします。VPS 1台で Panel と複数の Minecraft インスタンスを動かす、個人・小規模チーム向けの最も一般的な構成です。

## 前提条件

- Linux x86_64（Debian 12/13、Ubuntu 22.04+ 推奨。systemd 対応ディストリビューションであれば動作可能）
- Docker インストール済み（Daemon はデフォルトで `requireDocker=true`、すべてのインスタンスはコンテナ内で実行）
- root 権限（Daemon が Docker・ボリューム・ネットワークを管理するために必要）
- TCP ポート **24444**（Panel HTTP）、**24445**（Daemon HTTPS）を開放。nginx リバースプロキシについては [Nginx リバースプロキシ](nginx-https.md) を参照
- Minecraft インスタンス用ポート（デフォルト 25565+）

## 1. ディレクトリの準備

```bash
mkdir -p /opt/taps /var/lib/taps/panel /var/lib/taps/daemon
chmod 700 /var/lib/taps/panel /var/lib/taps/daemon
```

## 2. バイナリと Web アセットの配置

クロスコンパイル済みの成果物を配置します（ビルド手順は [development/build.md](../development/build.md) を参照）：

```bash
mv panel-linux-amd64  /opt/taps/panel
mv daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/panel /opt/taps/daemon

# フロントエンドの静的アセットを /opt/taps/web に展開
mkdir -p /opt/taps/web
tar -xzf web.tar.gz -C /opt/taps/web
```

最終的なディレクトリ構造：

```
/opt/taps/
├── panel
├── daemon
└── web/
    ├── index.html
    └── assets/

/var/lib/taps/
├── panel/    # Panel データ: panel.db, jwt.secret
└── daemon/   # Daemon データ: token, cert.pem, key.pem, files/, backups/, volumes/
```

> **データディレクトリ内のファイルはすべて初回起動時に自動生成されます**（SQLite DB、JWT シークレット、Daemon トークン、TLS 自己署名証明書、`config.json.template` サンプルを含む）。手動での初期化は不要です。

## 3. systemd ユニット

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

cat >/etc/systemd/system/taps-panel.service <<'EOF'
[Unit]
Description=TapS Panel
After=network-online.target taps-daemon.service
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
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
```

## 4. 起動

**Daemon を先に起動してください**（Panel は起動時に登録済みノードへの接続を試みます。初回起動時はノードが存在しないため問題ありませんが、本番環境ではこの順序を推奨します）：

```bash
systemctl enable --now taps-daemon
sleep 2
systemctl enable --now taps-panel
```

状態の確認：

```bash
systemctl status taps-daemon taps-panel
ss -lnt | grep -E '24444|24445'
```

`*:24444` と `*:24445` の両方がリッスン状態であれば正常です。

## 5. Daemon 情報の取得

```bash
# Daemon トークン（ノード追加時に必要）
cat /var/lib/taps/daemon/token

# Daemon TLS フィンガープリント（ノード追加時の TOFU 検証に必要）
journalctl -u taps-daemon -n 30 --no-pager | grep "tls fingerprint"
```

または、次のステップで Panel の自動プローブ機能を利用することもできます。

## 6. Panel への初回ログイン

ブラウザで `http://<サーバーIP>:24444/` を開きます：

1. `admin` / `admin` でログイン（初回起動時に `TAPS_ADMIN_USER`/`TAPS_ADMIN_PASS` で設定。DB が既に存在する場合は上書きされません）
2. システムが**パスワード変更**を要求します
3. ログイン成功後、ダッシュボードに入ります

## 7. Daemon ノードの追加

**「ノード管理」** → **「追加」** に進みます：

| フィールド | 値 |
|---|---|
| 名前 | `local`（任意の名前） |
| アドレス | `127.0.0.1:24445` |
| 表示ホスト | プレイヤーが接続する外部ドメイン/IP（例: `play.example.com`）。空欄の場合はアドレスのホスト部分が使用されます |
| ポート範囲 | 自動割り当てホストポートの範囲（デフォルト 25565-25600） |
| トークン | `cat /var/lib/taps/daemon/token` の出力内容 |

次に **「フィンガープリント取得」** をクリックします。Panel が Daemon に TLS プローブを行い、フィンガープリントを取得します。**フィンガープリントが** `journalctl ... grep "tls fingerprint"` の出力と一致することを確認し、**「承認して使用」** → **「保存」** をクリックします。

ノード一覧に「接続済み」と表示されれば成功です。

## 8. 最初のインスタンスの作成

**「インスタンス管理」** → **「新規作成」** に進み、テンプレート（Vanilla / Paper / Purpur / カスタム Docker）を選択して、名前・バージョン・メモリ・ポート・ディスクを入力します。詳細は [クイックスタート](../usage/quickstart.md) を参照してください。

---

## よくある設定変更

### Panel ポートの変更
**「システム設定」** → **「Panel リッスンポート」** で変更し、保存後に再起動します：
```bash
systemctl restart taps-panel
```

### Daemon 設定の変更（ポート / レート制限 / WS フレームサイズ）
2つの方法があります：
- **簡易的な方法**: `/etc/systemd/system/taps-daemon.service` の `Environment=` 行を編集 → `systemctl daemon-reload && systemctl restart taps-daemon`
- **永続的・推奨の方法**: `/var/lib/taps/daemon/config.json.template` を `config.json` にコピーして編集：
  ```bash
  cd /var/lib/taps/daemon
  cp config.json.template config.json
  vim config.json   # 上書きしないフィールドは削除
  systemctl restart taps-daemon
  journalctl -u taps-daemon -n 5 | grep "applied overrides"
  ```
  優先順位: `config.json` > 環境変数 > デフォルト値

### デフォルト認証情報
初回起動時の環境変数によって決まります。`TAPS_ADMIN_USER`/`TAPS_ADMIN_PASS` を変更しても、既存ユーザーには**影響しません** -- これらの環境変数は初回 DB 作成時のみ使用されます。

### HTTPS の有効化
nginx リバースプロキシ経由を推奨します: [Nginx リバースプロキシ + HTTPS](nginx-https.md)

---

## アンインストール

```bash
systemctl disable --now taps-panel taps-daemon
rm -f /etc/systemd/system/taps-{panel,daemon}.service
rm -rf /opt/taps /var/lib/taps
systemctl daemon-reload
# taps- プレフィックス付きの Docker コンテナを削除: docker rm -f $(docker ps -aq --filter name=taps-)
```
