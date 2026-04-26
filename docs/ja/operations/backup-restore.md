[English](../../operations/backup-restore.md) | [中文](../../zh/operations/backup-restore.md) | **日本語**

# バックアップとリカバリ

## 3段階のバックアップ

| レベル | 内容 | 推奨頻度 | ツール |
|---|---|---|---|
| **アプリケーション** | 個別インスタンスの作業ディレクトリ zip | 毎日 / 大きな変更の前 | Panel UI の「バックアップ」タブ |
| **コントロールプレーン** | `panel.db` + `jwt.secret`（Panel）+ `daemon/{token,cert.pem,key.pem,config.json}`（Daemon） | 毎日 | rsync / tar |
| **ホスト** | `/opt/taps`、`/var/lib/taps`、Docker ボリューム、Docker イメージ | 毎週 / 災害復旧 | LVM / btrfs / ZFS スナップショット、クラウドディスクスナップショット |

## Panel + Daemon の重要ファイル一覧

### Panel (`/var/lib/taps/panel/`)
```
panel.db           # すべてのビジネスデータ（ユーザー、ノード、インスタンス権限、API キー、設定、ログ）
jwt.secret         # JWT 署名に使用。削除するとすべてのトークンが即座に無効化される
```

### Daemon (`/var/lib/taps/daemon/`)
```
token              # Panel ↔ Daemon 共有シークレット
cert.pem           # 自己署名 TLS 証明書（Panel がフィンガープリントをピン留め）
key.pem            # 対応する秘密鍵
config.json        # オプション。管理者が記述した環境オーバーライド
files/             # 汎用ファイルルート（汎用インスタンスの作業ディレクトリ、ユーザーアップロードなど）
backups/           # アプリケーションレベルのバックアップ zip
volumes/           # マネージドボリューム + Docker インスタンスの /data ディレクトリ（インスタンスごとに inst-<short> サブディレクトリ）
```

> `files/` と `volumes/` は **ビジネスデータ** であり、非常に大きくなる可能性があります。これらをバックアップすることは、すべてのインスタンスのワールドファイルなどをバックアップすることを意味します。

## シンプルな rsync スクリプト

```bash
#!/bin/bash
# /usr/local/bin/taps-backup.sh
set -e
DATE=$(date +%Y%m%d-%H%M%S)
DEST=/srv/backup/taps/$DATE
mkdir -p $DEST

# Panel
cp /var/lib/taps/panel/panel.db   $DEST/
cp /var/lib/taps/panel/jwt.secret $DEST/

# Daemon
cp /var/lib/taps/daemon/token     $DEST/
cp /var/lib/taps/daemon/cert.pem  $DEST/
cp /var/lib/taps/daemon/key.pem   $DEST/
[ -f /var/lib/taps/daemon/config.json ] && cp /var/lib/taps/daemon/config.json $DEST/

# インスタンスデータ + バックアップ zip
rsync -a /var/lib/taps/daemon/files/   $DEST/files/
rsync -a /var/lib/taps/daemon/backups/ $DEST/backups/
# volumes は通常大きいため、必要に応じて含める
# rsync -a /var/lib/taps/daemon/volumes/ $DEST/volumes/

# 30日以上経過したバックアップを削除
find /srv/backup/taps -maxdepth 1 -type d -mtime +30 -exec rm -rf {} +
```

cron に追加:
```cron
0 4 * * *  /usr/local/bin/taps-backup.sh >> /var/log/taps-backup.log 2>&1
```

> Panel.db は SQLite です — **ホットコピーでは不整合な状態をキャプチャする可能性があります**。本番環境では `sqlite3 panel.db ".backup '/srv/backup/.../panel.db'"` を使用してください。SQLite 独自のバックアップ API により一貫性が保証されます。

```bash
sqlite3 /var/lib/taps/panel/panel.db ".backup '$DEST/panel.db'"
```

## 災害復旧: Panel をゼロから再構築

Panel ホストが完全に破壊されたがバックアップがある前提:

```bash
# 1. 新しい Panel ホストをセットアップ（panel-only.md のステップ 1-3 に従い、ディレクトリ + systemd を設定）

# 2. データを復元
systemctl stop taps-panel
cp /backup/.../panel.db   /var/lib/taps/panel/panel.db
cp /backup/.../jwt.secret /var/lib/taps/panel/jwt.secret
chmod 600 /var/lib/taps/panel/jwt.secret
systemctl start taps-panel

# 3. 確認
journalctl -u taps-panel -n 20 --no-pager | tail -10
# "panel listening" および（ノードが存在する場合）各 Daemon に対して "panel connected" と表示されるはず
```

`panel.db` + `jwt.secret` が無事であれば、すべてのユーザー、ノード、API キー、設定が復元され、**以前発行された JWT も引き続き有効です**（jwt.secret が変更されておらず + tokens_invalid_before も変更されていないため）。

## 災害復旧: Daemon をゼロから再構築

Daemon ホストが破壊された前提:

```bash
# 1. 新しいマシンに Daemon をセットアップ（daemon-only.md のステップ 1-2 に従う）

# 2. 復元
systemctl stop taps-daemon
cp /backup/.../token      /var/lib/taps/daemon/token
cp /backup/.../cert.pem   /var/lib/taps/daemon/cert.pem
cp /backup/.../key.pem    /var/lib/taps/daemon/key.pem
chmod 600 /var/lib/taps/daemon/{token,cert.pem,key.pem}
rsync -a /backup/.../files/   /var/lib/taps/daemon/files/
rsync -a /backup/.../backups/ /var/lib/taps/daemon/backups/

systemctl start taps-daemon
```

token + cert/key が一致していれば、**Panel でフィンガープリントを再 TOFU する必要はありません** — フィンガープリントがそのまま一致します。

token や cert が失われた場合は、Panel のノード編集画面で新しい token を更新してください。新しい cert の場合は、ノード編集画面でフィンガープリントを再取得してください。

## インスタンスレベルの復元

```bash
# Panel UI 経由: バックアップページ → 対象の zip を選択 → 「復元」をクリック
# または API 経由:
curl -X POST -H "Authorization: Bearer $JWT" \
     -H "Content-Type: application/json" \
     -d '{"name":"<backup-zip>"}' \
     https://panel/api/daemons/$ID/instances/$UUID/backups/restore
```

これは **同名の既存ファイルを上書きします**。差分同期は行いません。復元前にインスタンスを停止することを推奨します。
