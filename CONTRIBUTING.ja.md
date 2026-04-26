**[English](CONTRIBUTING.md)** | [中文](CONTRIBUTING.zh-CN.md) | **日本語**

# コントリビューションガイド

TapS にご関心をお寄せいただきありがとうございます！以下は参加方法と規約です。

## Issue の提出

- **バグ報告**：再現手順、Panel/Daemon バージョン、ブラウザバージョン、`journalctl` ログの抜粋を記載してください
- **機能提案**：ユースケースと期待する動作を記述してください
- **セキュリティ脆弱性**：公開 Issue として提出**しないでください**。**hi@mail.mctap.org** にメールをお送りください

## Pull Request の手順

1. リポジトリをフォーク
2. フィーチャーブランチを作成：`git checkout -b feature/my-feature`
3. 開発とテスト
4. 以下のチェックをパスすることを確認：
   ```bash
   # Go ビルド
   cd packages/panel && go build ./cmd/panel
   cd packages/daemon && go build ./cmd/daemon

   # フロントエンドビルド
   cd web && npm run build

   # i18n 整合チェック（中/英/日 三言語のキーが一致すること）
   node scripts/i18n-gap-check.js
   ```
5. 変更をコミット：`git commit -m 'feat: add some feature'`
6. プッシュして Pull Request を作成

## コミット規約

[Conventional Commits](https://www.conventionalcommits.org/) フォーマットを推奨：

```
feat: 新機能
fix: バグ修正
docs: ドキュメント変更
style: コードフォーマット（機能に影響なし）
refactor: リファクタリング（新機能でもバグ修正でもない）
perf: パフォーマンス最適化
test: テスト関連
chore: ビルド/ツール/依存関係の変更
security: セキュリティ関連の修正
```

## コーディング規約

### Go バックエンド

- 標準の Go コードスタイル（`gofmt`）に従う
- すべての API エラーは `apiErr(c, status, "domain.error_code", "message")` で統一的に返す
- エラーコード形式：`domain.snake_case`（例：`auth.invalid_credentials`、`fs.missing_path`）
- マルチキー settings の書き込みは必ず `db.Transaction` 内で行う
- 新しい API ルートは `packages/panel/internal/api/router.go` に登録
- コメントは「なぜ」を説明する場合のみ記述、「何をするか」は不要

### React フロントエンド

- TypeScript strict モード
- ユーザーに表示されるすべてのテキストは i18n（`t('key')`）経由、中国語/英語/日本語のハードコード禁止
- 新しい i18n キーは `web/src/i18n/zh.ts`、`en.ts`、`ja.ts` の 3 ファイルに同時追加が必須
- `node scripts/i18n-gap-check.js` を実行して三言語の整合を確認
- 状態管理は Zustand、API コールは `web/src/api/` ディレクトリに配置
- エラー表示は `formatApiError(e)` で i18n エラーコードテーブルを自動参照

### セキュリティ

- ユーザー入力は必ずバリデーション（バックエンド handler 層 + フロントエンドフォーム rules）
- ファイル操作は必ず `fs.Resolve`（EvalSymlinks + containedIn）を通す
- Docker CLI 引数は必ず `ValidImage` / `validInstanceUUID` でバリデーション
- URL フラグメント / エラーレスポンスに内部エラー詳細を漏洩しない
- 新しい settings エンドポイントは admin ロール + トランザクション整合性を検討

## ディレクトリ構成

```
packages/panel/internal/api/   ← Panel HTTP API ハンドラ
packages/daemon/internal/rpc/  ← Daemon WS RPC ハンドラ
packages/shared/protocol/      ← Panel↔Daemon 共有プロトコル
web/src/pages/                 ← React ページコンポーネント
web/src/i18n/                  ← 翻訳ファイル（zh/en/ja）
docs/                          ← プロジェクトドキュメント
```

## ローカル開発

```bash
# ターミナル 1 — Daemon
cd packages/daemon && go run ./cmd/daemon

# ターミナル 2 — Panel
cd packages/panel && go run ./cmd/panel

# ターミナル 3 — フロントエンドホットリロード
cd web && npm install && npm run dev
# http://localhost:5173 が自動的に /api → localhost:24444 にプロキシ
```

デフォルトアカウント `admin` / `admin`、初回ログイン時にパスワード変更が必須です。

## ライセンス

本プロジェクトは [GPL-3.0](LICENSE) ライセンスを使用しています。コントリビューションの提出は、あなたのコードが同じライセンスの下で公開されることに同意したことを意味します。
