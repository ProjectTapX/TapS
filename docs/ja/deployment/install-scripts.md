[English](../../deployment/install-scripts.md) | [中文](../zh/deployment/install-scripts.md) | **日本語**

# ワンクリックインストールスクリプト

最新の GitHub リリースから TapS を素早くインストールするための 3 つのスクリプトです。CPU アーキテクチャ（x86_64 / ARM64）を自動検出し、バイナリのダウンロード、systemd サービスの設定、すべてのコンポーネントの起動を行います。

## クイックスタート

```bash
# シングルホスト（Panel + Daemon を同一マシンに導入）— 最も一般的な構成
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash

# Panel のみ（コントロールプレーン、ゲームインスタンスなし）
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-panel.sh | bash

# Daemon のみ（既存の Panel にゲームサーバーノードを追加）
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-daemon.sh | bash
```

## 前提条件

| 要件 | Panel | Daemon | シングルホスト |
|------|-------|--------|----------------|
| Linux x86_64 または ARM64 | Yes | Yes | Yes |
| Root 権限 | Yes | Yes | Yes |
| curl | Yes | Yes | Yes |
| Docker | No | Yes | Yes |
| systemd | Yes | Yes | Yes |

## 各スクリプトの動作

### `install.sh`（シングルホスト）

1. CPU アーキテクチャを検出
2. GitHub から最新リリースバージョンを取得
3. 設定項目の入力を求める（ポート、データディレクトリ、管理者資格情報）
4. `panel-linux-{arch}`、`daemon-linux-{arch}`、`web.tar.gz` をダウンロード
5. `chmod 700` でデータディレクトリを作成
6. `taps-daemon` と `taps-panel` の systemd ユニットファイルを作成
7. Daemon を先に起動し、待機後に Panel を起動
8. token、TLS フィンガープリント、アクセス URL を表示

### `install-panel.sh`（Panel のみ）

1. CPU アーキテクチャを検出
2. 最新リリースバージョンを取得
3. 入力項目：ポート、データディレクトリ、Web ディレクトリ、管理者ユーザー名/パスワード
4. `panel-linux-{arch}` と `web.tar.gz` をダウンロード
5. systemd ユニット `taps-panel` を作成
6. Panel を起動しアクセス URL を表示

### `install-daemon.sh`（Daemon のみ）

1. CPU アーキテクチャを検出
2. 最新リリースバージョンを取得
3. 入力項目：リッスンアドレス、データディレクトリ
4. `daemon-linux-{arch}` をダウンロード
5. systemd ユニット `taps-daemon` を作成
6. Daemon を起動し token と TLS フィンガープリントを表示

## 設定オプション

すべてのオプションには適切なデフォルト値が設定されています。Enter キーを押すだけでデフォルトを使用できます。

| オプション | デフォルト値 | スクリプト |
|------------|--------------|------------|
| Panel リッスンポート | `24444` | Panel, Single-Host |
| Panel データディレクトリ | `/var/lib/taps/panel` | Panel, Single-Host |
| Web 静的ファイルディレクトリ | `/opt/taps/web` | Panel, Single-Host |
| 管理者ユーザー名 | `admin` | Panel, Single-Host |
| 管理者パスワード | `admin` | Panel, Single-Host |
| Daemon リッスンアドレス | `:24445` | Daemon, Single-Host |
| Daemon データディレクトリ | `/var/lib/taps/daemon` | Daemon, Single-Host |

## インストール後の確認

```bash
# サービスの状態を確認
systemctl status taps-panel taps-daemon

# リッスンポートを確認
ss -lnt | grep -E '24444|24445'

# Daemon の token を確認（Panel でノードを追加する際に必要）
cat /var/lib/taps/daemon/token

# Daemon の TLS フィンガープリントを確認
journalctl -u taps-daemon | grep "tls fingerprint"
```

## プロキシ対応

スクリプトはダウンロードに `curl` を使用します。プロキシ環境下では、実行前に環境変数を設定してください。

```bash
export HTTPS_PROXY=http://proxy:port
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash
```

## アンインストール

```bash
systemctl disable --now taps-panel taps-daemon
rm -f /etc/systemd/system/taps-{panel,daemon}.service
systemctl daemon-reload
rm -rf /opt/taps /var/lib/taps
```
