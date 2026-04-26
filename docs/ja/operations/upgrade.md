[English](../../operations/upgrade.md) | [中文](../../zh/operations/upgrade.md) | **日本語**

# アップグレード手順

## ローリングアップグレード戦略

- **Daemon**: Daemon を先にアップグレードしても Panel には影響しません。そのノード上で稼働中のインスタンスは Daemon の再起動による影響を受けません（Docker コンテナは独立したプロセスです）。Daemon はグレースフルシャットダウンに対応しています（SIGTERM → 30秒待機 → hib.Shutdown → volumes.UnmountAll）。systemd の再起動はこのパスに従います
- **Panel**: Panel をアップグレードすると、すべての Panel↔Daemon WebSocket 接続が切断されます。再接続まで数秒間 Panel が利用できなくなります。Daemon 側のインスタンスは引き続き稼働します。Panel もグレースフルシャットダウンに対応しています
- **推奨順序**: Daemon を先に → 数秒待機 → その後 Panel。こうすることで Panel 起動時に Daemon が既に準備完了の状態になります

## アップグレード前の準備

```bash
# 1. SQLite と重要な設定ファイルをバックアップ
TS=$(date +%Y%m%d-%H%M%S)
mkdir -p /opt/taps/backup
cp /var/lib/taps/panel/panel.db /opt/taps/backup/panel.db.$TS
cp /var/lib/taps/panel/jwt.secret /opt/taps/backup/jwt.secret.$TS
cp /var/lib/taps/daemon/token /opt/taps/backup/daemon-token.$TS
cp /var/lib/taps/daemon/cert.pem /opt/taps/backup/daemon-cert.$TS
cp /var/lib/taps/daemon/key.pem /opt/taps/backup/daemon-key.$TS
cp /opt/taps/panel  /opt/taps/backup/panel.$TS
cp /opt/taps/daemon /opt/taps/backup/daemon.$TS

# 2. 稼働中のインスタンスを確認（停止中のものは問題なし、稼働中のものが重要）
systemctl status taps-panel taps-daemon
ss -lnt | grep -E '24444|24445'
```

## Daemon のアップグレード

```bash
# 新しいバイナリが /tmp/daemon-linux-amd64 にある前提
systemctl stop taps-daemon
mv /tmp/daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/daemon
systemctl start taps-daemon
sleep 3

# 正常に稼働しているか確認
systemctl is-active taps-daemon
journalctl -u taps-daemon -n 20 --no-pager | tail -10
# config.json が適用されたか確認
journalctl -u taps-daemon -n 20 | grep "applied overrides"
# トークン / フィンガープリントが変わっていないか確認（変わっていたらトークン / 証明書ファイルが失われた可能性あり）
journalctl -u taps-daemon -n 20 | grep -E 'token:|fingerprint:'
```

`cert.pem` / `key.pem` が存在しない場合（例: クリーンアップ中に誤って削除した場合）、Daemon は新しい証明書を再生成します → **Panel で新しいフィンガープリントを再承認する必要があります**。

## Panel のアップグレード

```bash
# 新しいバイナリと Web アセットが /tmp/ にある前提
systemctl stop taps-panel
mv /tmp/panel-linux-amd64 /opt/taps/panel
chmod +x /opt/taps/panel
rm -rf /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf /tmp/web.tar.gz -C /opt/taps/web
rm /tmp/web.tar.gz
systemctl start taps-panel
sleep 3

# 確認
systemctl is-active taps-panel
journalctl -u taps-panel -n 30 --no-pager | tail -15
# "panel listening on :24444" と表示されるはず
# "panel connected: ..." と表示され、各 Daemon への再接続が成功していることを確認
```

## DB マイグレーションの自動適用

起動時に Panel の GORM `AutoMigrate` が自動的に以下を実行します:
- 新しいテーブルの追加（新バージョンで導入された場合）
- 新しいカラムの追加（例: Batch #4 の `tokens_invalid_before`、Batch #7 の `expires_at`/`revoked_at`）
- 新しいインデックスの追加

**行わないこと**: カラムの削除、カラム型の変更、ロールバック。

アップグレードログに `record not found` 警告が表示された場合（特定の `settings` テーブルキーに関して）、それは新バージョンで導入されたまだ使用されていない新設定です → 正常な動作で、デフォルト値が使用されます。

## ロールバック

```bash
TS=timestamp_of_latest_backup

systemctl stop taps-panel taps-daemon

# バイナリを復元
cp /opt/taps/backup/panel.$TS  /opt/taps/panel
cp /opt/taps/backup/daemon.$TS /opt/taps/daemon

# DB を復元（新バージョンがカラムを追加していた場合、旧 Panel は余分なカラムを無視します — 問題なし）
cp /opt/taps/backup/panel.db.$TS /var/lib/taps/panel/panel.db

systemctl start taps-daemon
sleep 2
systemctl start taps-panel
```

> 新バージョンが **新しい設定** や **expiresAt/revokedAt 付きの API キー** を書き込んでいた場合、そのデータはロールバック後も DB に残りますが、旧 Panel はそれらを使用しません — 機能が壊れることはなく、新機能が「消える」だけです。

## フロントエンドのみのアップグレード（Panel の再起動不要）

Panel を再起動せずに Web 静的アセットを置き換えます:

```bash
rm -rf /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf /tmp/web.tar.gz -C /opt/taps/web
# Panel 内蔵の http.FileServer はディレクトリ一覧をキャッシュしないため、次のブラウザリクエストで即座に反映されます
```

ユーザーには **強制リロード**（Ctrl+F5）を行い、Vite のハッシュ付きファイル名キャッシュをクリアするよう案内してください。

## systemd ユニットのアップグレード

新バージョンで追加の環境変数 / ExecStart の変更 / `LimitNOFILE` の追加などが必要な場合:

```bash
vim /etc/systemd/system/taps-panel.service
systemctl daemon-reload
systemctl restart taps-panel
```
