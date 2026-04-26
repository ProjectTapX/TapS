[English](../../security/best-practices.md) | [中文](../../zh/security/best-practices.md) | **日本語**

# デプロイ堅牢化チェックリスト

本番運用前に確認してください。重要度の高い順に並べています。

## 必須（P0）

- [ ] **TLS**: nginx リバースプロキシ + Let's Encrypt で Panel の HTTPS を設定（[ガイド](../deployment/nginx-https.md)）
- [ ] **デフォルトパスワードの変更**: 初回ログイン後すぐに `admin/admin` を変更
- [ ] **Daemon の外部公開を遮断**（Daemon と Panel が同一マシンの場合）: Daemon を `addr=127.0.0.1:24445` に変更し、クラウドファイアウォールで外部からの 24445 をブロック
- [ ] **信頼済みプロキシリストの設定**: システム設定 → 信頼済みプロキシリスト → nginx ホストの IP を追加 → Panel を再起動。**これがないとレート制限が実質的に無効になります**
- [ ] **Panel パブリック URL の設定**: システム設定 → Panel パブリック URL → `https://yourdomain` を入力。これがないと SSO コールバック / ターミナル WS オリジンチェック / CORS フォールバックがすべて失敗します
- [ ] **Daemon トークンと TLS フィンガープリントの検証**: ノード追加時に、フィンガープリントが Daemon の起動ログ出力と **バイト単位で一致する** ことを確認

## 強く推奨（P1）

- [ ] **デフォルト管理者ユーザー名の変更**: `TAPS_ADMIN_USER` を `admin` 以外に設定（初回シード時のみ有効）
- [ ] **レート制限の強化**: システム設定 → レート制限 → 5回/分を3回/分に変更。ブロック期間を5分から15分に変更
- [ ] **JWT TTL の短縮**: システム設定 → セッション有効期間 → 60分を30分に変更
- [ ] **WS ハートビート間隔の短縮**: 5分を2分に変更
- [ ] **CORS ホワイトリストの制限**: システム設定 → CORS 許可オリジン → 信頼済みフロントエンドドメインのみを記載
- [ ] **CSP ホワイトリストの確認**: システム設定 → コンテンツセキュリティポリシー (CSP) → script-src / frame-src に実際に使用する CAPTCHA CDN のみが含まれていることを確認
- [ ] **Webhook URL は専用ドメインで**: 信頼済みドメイン名のみを使用
- [ ] **定期的なデータベースバックアップ**: [バックアップとリカバリ](../operations/backup-restore.md)を参照
- [ ] **監査ログの監視**: ログインログで不審な IP / 過度な 401 がないか定期的に確認

## 推奨（P2）

- [ ] **ノードマシン用の個別アカウント**: 専用 VPS / VLAN で Daemon を実行
- [ ] **ファイアウォールホワイトリスト**: Daemon の 24445 は Panel のエグレス IP のみ許可
- [ ] **IP ホワイトリスト + 有効期限付き API キー**: CI キーは90日に設定
- [ ] **HTTP タイムアウトの強化**: デフォルトで十分（10/60/120/120秒）。リスクの高いシナリオではより短い値を使用可能
- [ ] **Docker イメージミラー**: ローカル/リージョンのミラーアクセラレータを使用
- [ ] **systemd ユニット制限**: `MemoryMax=`、`TasksMax=`、`PrivateTmp=true`
- [ ] **SELinux / AppArmor**
- [ ] **ホストレベルの堅牢化**: root SSH ログインの無効化、SSH 鍵認証のみ、ufw デフォルト拒否

## オプション（P3）

- [ ] **WAF**: Cloudflare / クラウドプロバイダ WAF
- [ ] **VPN フォールバック**: WireGuard / Tailscale 内部ネットワーク、外部公開なし

## 本番稼働前の30秒セルフチェック

```bash
# 1. デフォルトパスワードは変更済み？
curl -s -X POST https://panel.example.com/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | grep -q "invalid_credentials" \
  && echo "✓ Default password changed" || echo "✗ Default password still active!"

# 2. HTTPS は動作している？
curl -sI https://panel.example.com/healthz | grep -q "200 OK" \
  && echo "✓ HTTPS OK" || echo "✗ HTTPS issue"

# 3. セキュリティヘッダーはある？
curl -sI https://panel.example.com/ | grep -q "X-Frame-Options" \
  && echo "✓ Security headers present" || echo "✗ Security headers missing"

# 4. Daemon は外部公開されていない？
nc -zv panel.example.com 24445 -w 3 2>&1 | grep -q "succeeded" \
  && echo "✗ Daemon 24445 is publicly open!" || echo "✓ Daemon 24445 closed"

# 5. レート制限は動作している？
for i in 1 2 3 4 5 6; do
  curl -sw "%{http_code}\n" -o /dev/null -X POST https://panel.example.com/api/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"username":"x","password":"x"}'
done
# 5回目/6回目で 429 が返るはず
```

上記5つのチェックのいずれかが失敗した場合、**本番トラフィックを受け入れる前に解決してください**。
