# 部署加固清单

上线前过一遍。从严重度从高到低。

## 必做（P0）

- [ ] **TLS**：用 nginx 反代 + Let's Encrypt 给 Panel 套 HTTPS（[指南](../deployment/nginx-https.md)）
- [ ] **改默认密码**：首次登录后立即把 `admin/admin` 改掉
- [ ] **关掉 daemon 公网入站**（如果 daemon 和 panel 同机）：daemon 改 `addr=127.0.0.1:24445`，云防火墙拒绝外网 24445
- [ ] **配反代信任列表**：系统设置 → 反向代理信任列表 → 加 nginx 主机 IP → 重启 panel。**不配的话限频 = 形同虚设**
- [ ] **配 Panel 公开地址**：系统设置 → Panel 公开地址 → 填 `https://你的域名`。不配的话 SSO 回调 / 终端 WS 同源校验 / CORS 回退全部不生效
- [ ] **Daemon Token 与 TLS 指纹核对**：添加节点时**逐字节核对**指纹与 daemon 启动日志的指纹一致

## 强烈建议（P1）

- [ ] **改默认 admin 用户名**：`TAPS_ADMIN_USER` 设非 `admin` 的字符串（仅首次 seed 生效）
- [ ] **限频更严**：系统设置 → 速率限制 → 把 5/min 改成 3/min；ban 时长 5 → 15 min
- [ ] **JWT TTL 缩短**：系统设置 → 会话有效期 → 60 → 30 分钟
- [ ] **WS 心跳间隔缩短**：5 → 2 分钟
- [ ] **CORS 白名单收紧**：系统设置 → 跨源访问白名单 → 列出仅信任的前端域名
- [ ] **CSP 白名单审查**：系统设置 → 内容安全策略（CSP）→ 确认 script-src / frame-src 只包含你实际用的 captcha CDN
- [ ] **Webhook URL 走专用业务域名**：仅填可信域名
- [ ] **数据库定期备份**：见 [备份与恢复](../operations/backup-restore.md)
- [ ] **审计日志监控**：定期检查登录日志中的异常 IP / 大量 401

## 推荐（P2）

- [ ] **节点机器单独账号**：daemon 跑在独立 VPS / 独立 VLAN
- [ ] **防火墙白名单**：daemon 24445 只允许 panel 出站 IP
- [ ] **API Key 带 IP 白名单 + 过期**：CI key 给 90 天
- [ ] **HTTP 超时调紧**：默认已足够（10/60/120/120s），高风险场景可缩短
- [ ] **Docker 镜像源**：用国内镜像加速器
- [ ] **systemd 单元限制**：`MemoryMax=`、`TasksMax=`、`PrivateTmp=true`
- [ ] **SELinux / AppArmor**
- [ ] **主机基础加固**：关闭 root SSH 直登、SSH key only、ufw 默认 deny

## 可选（P3）

- [ ] **WAF**：Cloudflare / 阿里云 WAF
- [ ] **VPN 兜底**：WireGuard / Tailscale 内网，公网不暴露

## 上线前 30 秒自检

```bash
# 1. 默认密码改了？
curl -s -X POST https://panel.example.com/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | grep -q "invalid_credentials" \
  && echo "✓ 默认密码已改" || echo "✗ 默认密码还在！"

# 2. HTTPS 工作？
curl -sI https://panel.example.com/healthz | grep -q "200 OK" \
  && echo "✓ HTTPS OK" || echo "✗ HTTPS 异常"

# 3. 安全 Header？
curl -sI https://panel.example.com/ | grep -q "X-Frame-Options" \
  && echo "✓ 安全 Header 就位" || echo "✗ 安全 Header 缺失"

# 4. daemon 公网未暴露？
nc -zv panel.example.com 24445 -w 3 2>&1 | grep -q "succeeded" \
  && echo "✗ daemon 24445 公网开着！" || echo "✓ daemon 24445 已关"

# 5. 限频生效？
for i in 1 2 3 4 5 6; do
  curl -sw "%{http_code}\n" -o /dev/null -X POST https://panel.example.com/api/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"username":"x","password":"x"}'
done
# 第 5/6 次应该是 429
```

上述五项有任何一个不通过，**先解决再放生产流量**。
