**[English](CHANGELOG.md)** | [中文](CHANGELOG.zh-CN.md) | **日本語**

# 変更履歴

TapS の各バージョンの主な変更を記録します。フォーマットは [Keep a Changelog](https://keepachangelog.com/ja/1.0.0/) に準拠しています。

## [26.1.0] - 2026-04-26

初回公開リリース。

### 追加

- **Panel + Daemon デュアルアーキテクチャ**：Panel（Go + Gin + GORM + SQLite）が Web UI と集中管理を担当、Daemon（Go + gorilla/websocket + Docker CLI）がホストマシンでコンテナを実行
- **React フロントエンド**：Vite 5 + React 18 + TypeScript + Ant Design 5、ダーク/ライトテーマ切替対応
- **インスタンス管理**：Docker コンテナインスタンスの作成/起動・停止/強制終了/自動起動/クラッシュ自動再起動
- **ブラウザリアルタイムターミナル**：xterm.js + WebSocket、本物の PTY、切断時自動再接続、ローカル行編集 + Tab 補完
- **ワンクリックデプロイテンプレート**：Vanilla / Paper / Purpur / Fabric / Forge / NeoForge、バージョン選択で即デプロイ
- **ファイルマネージャ**：チャンク分割アップロード/ストリーミングダウンロード/オンライン編集/リネーム/コピー/移動/zip 圧縮・解凍
- **バックアップ＆リストア**：インスタンス単位の zip スナップショット、メモ付き、バックアップはディスククォータに計上
- **マネージドボリューム**：loopback 固定サイズボリューム、インスタンスごとに独立したディスククォータ
- **リソースモニタリング**：ノード CPU/メモリ/ディスクのリアルタイムダッシュボード + 履歴グラフ、インスタンス単位の Docker stats
- **自動ハイバネーション**：アイドル検出 → コンテナ停止 → 偽 SLP リスナー → プレイヤー接続で自動復帰
- **ノードグルーピング**：マルチノード負荷分散、ディスク空き容量 + 最低メモリで自動ノード選択
- **スケジュールタスク**：cron 式、アクション：コマンド/起動/停止/再起動/バックアップ
- **ユーザーと権限**：admin / user ロール、インスタンス単位の権限付与
- **API Key**：`tps_` プレフィックスの長期認証情報、IP ホワイトリスト + スコープ + 有効期限
- **SSO / OIDC**：Logto / Google / Microsoft / Keycloak などの標準 OIDC プロバイダ対応、PKCE + HMAC state
- **ログインキャプチャ**：Cloudflare Turnstile / reCAPTCHA Enterprise
- **Docker イメージ管理**：プル/削除/カスタム表示名、OCI ラベル自動読み取り
- **多言語対応**：中国語 / English / 日本語（926 キー 三言語整合）
- **Webhook 通知**：ノードのオフライン/復帰時に JSON をプッシュ（60 秒デバウンス）

### セキュリティ

- Content-Security-Policy（admin が script-src / frame-src ホワイトリストを設定可能）
- X-Frame-Options / X-Content-Type-Options / Referrer-Policy / 条件付き HSTS
- SSRF 防御：ClassifyHost 三分類 + DialContext DNS rebinding 再検証
- パストラバーサル防御：EvalSymlinks + containedIn + O_NOFOLLOW + zip symlink 拒否
- JWT：HS256 + スライディング更新 + TokensInvalidBefore 失効 + alg:none 拒否
- bcrypt パスワードハッシュ + AES-GCM secrets at-rest
- レート制限：login / changePw / apiKey / oauthStart 独立バケット
- WS dispatch 同時実行上限（デフォルト 8192）+ WS フレームサイズ上限
- HTTP サーバータイムアウト（slow-loris 防御）
- グレースフルシャットダウン：SIGTERM → srv.Shutdown → hib.Shutdown → vm.UnmountAll
- すべてのマルチキー settings 書き込みは db.Transaction 内で実行
- 累計 99 項目のセキュリティ強化、6 ラウンド監査で評価 A
