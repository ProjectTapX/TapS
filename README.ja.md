[English](README.md) | [中文](README.zh-CN.md) | **日本語**

# TapS

オープンソースのゲームサーバー管理パネル — 1つのパネルですべてのゲームサーバーを管理。

TapS は Panel + Daemon のデュアルアーキテクチャを採用。Panel が Web UI と集中管理を担当し、Daemon が各ゲームホストマシンでコンテナを運用します。Minecraft Java / Bedrock / Terraria および Docker コンテナ化されたプロセスの統合管理に対応し、リアル PTY ターミナル、ファイル管理、バックアップ復元、自動休止、SSO ログイン、多言語対応などのモダンな管理体験を提供します。

## ✨ 機能

- **デュアルアーキテクチャ** — Panel（コントロールプレーン + Web UI）+ Daemon（ホストエージェント）、WSS + TLS フィンガープリントピンニング
- **インスタンス管理** — Docker コンテナインスタンス：起動/停止/再起動/強制終了、自動起動・クラッシュ時自動再起動
- **ブラウザリアルタイムターミナル** — xterm.js + WebSocket、リアル PTY、自動再接続、ローカル行編集 + Tab 補完
- **ワンクリックデプロイ** — Vanilla / Paper / Purpur / Fabric / Forge / NeoForge テンプレート内蔵
- **ファイルマネージャー** — チャンクアップロード/ストリーミングダウンロード/オンライン編集/リネーム/コピー/移動/zip 圧縮解凍
- **バックアップ復元** — インスタンスレベルの zip スナップショット、カスタムメモ対応
- **マネージドボリューム** — loopback 固定サイズボリュームでインスタンスごとのディスククォータ
- **リソース監視** — CPU/メモリ/ディスク リアルタイムダッシュボード + 履歴グラフ
- **自動休止** — アイドル検出 → コンテナ停止 → カスタム MOTD 付き偽 SLP リスナー → プレイヤー接続で起動
- **ノードグループ** — マルチノード負荷分散スケジューリング
- **スケジュールタスク** — cron 式：コマンド送信/起動/停止/再起動/バックアップ
- **ユーザーと権限** — admin / user ロール、インスタンス単位の権限付与
- **API Key** — `tps_` プレフィックス長期認証情報、IP ホワイトリスト + Scope + 有効期限
- **SSO / OIDC** — Logto / Google / Microsoft / Keycloak 等の標準 OIDC プロバイダー対応
- **ログイン CAPTCHA** — Cloudflare Turnstile / reCAPTCHA Enterprise
- **セキュリティ強化** — CSP / X-Frame-Options / SSRF 防御 / パストラバーサル防御 / レート制限
- **多言語** — 中文 / English / 日本語（926 キー完全対応）
- **ダークテーマ** — ダーク/ライトモード切替

## 🚀 クイックスタート

### 必要条件

- **Go** ≥ 1.25
- **Node.js** ≥ 18 + npm
- **Docker Engine**（Daemon ホスト）
- **Linux**（Daemon 本番環境は Linux のみ）

### デプロイ

```bash
git clone https://github.com/yourname/TapS.git
cd TapS
bash scripts/build.sh
# 出力: dist/panel-linux-amd64, dist/daemon-linux-amd64, dist/web/
```

詳細なデプロイ手順: [docs/ja/](docs/ja/)

### ローカル開発

```bash
cd packages/daemon && go run ./cmd/daemon  # ターミナル 1
cd packages/panel && go run ./cmd/panel    # ターミナル 2
cd web && npm install && npm run dev       # ターミナル 3
```

デフォルト認証情報: `admin` / `admin`（初回ログイン時にパスワード変更が必須）

## 📚 ドキュメント

**[📖 完全なドキュメント](docs/ja/README.md)** — 利用ガイド、デプロイ、運用、開発、セキュリティ、API リファレンスを網羅。

## 🤝 コントリビューション

Issue と Pull Request を歓迎します。詳細は [Contributing Guide](CONTRIBUTING.md) をご覧ください。

> セキュリティ脆弱性は **hi@mail.mctap.org** にメールでご報告ください。公開 Issue は使用しないでください。

## 📜 ライセンス

[GPL-3.0](LICENSE)
