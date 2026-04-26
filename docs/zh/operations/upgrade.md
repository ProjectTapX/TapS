# 升级流程

## 滚动方针

- **Daemon**：先升级 Daemon 不影响 Panel；升级期间该节点上的运行实例不受 Daemon 重启影响（Docker 容器是独立进程）。Daemon 支持 graceful shutdown（SIGTERM → 30s 等待 → hib.Shutdown → volumes.UnmountAll），systemd restart 会正确走这条路径
- **Panel**：升级 Panel 时，所有 Panel↔Daemon WebSocket 连接会断开，重连前几秒内 Panel 不可用；Daemon 端实例继续运行。Panel 同样支持 graceful shutdown
- **顺序建议**：先 Daemon → 等几秒 → 再 Panel；这样在 Panel 启动时 Daemon 已经 ready

## 升级前

```bash
# 1. 备份 SQLite + 关键配置
TS=$(date +%Y%m%d-%H%M%S)
mkdir -p /opt/taps/backup
cp /var/lib/taps/panel/panel.db /opt/taps/backup/panel.db.$TS
cp /var/lib/taps/panel/jwt.secret /opt/taps/backup/jwt.secret.$TS
cp /var/lib/taps/daemon/token /opt/taps/backup/daemon-token.$TS
cp /var/lib/taps/daemon/cert.pem /opt/taps/backup/daemon-cert.$TS
cp /var/lib/taps/daemon/key.pem /opt/taps/backup/daemon-key.$TS
cp /opt/taps/panel  /opt/taps/backup/panel.$TS
cp /opt/taps/daemon /opt/taps/backup/daemon.$TS

# 2. 看看运行中的实例（停的不重要，跑的要在意）
systemctl status taps-panel taps-daemon
ss -lnt | grep -E '24444|24445'
```

## 升级 Daemon

```bash
# 假设新二进制放在 /tmp/daemon-linux-amd64
systemctl stop taps-daemon
mv /tmp/daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/daemon
systemctl start taps-daemon
sleep 3

# 看是否正常
systemctl is-active taps-daemon
journalctl -u taps-daemon -n 20 --no-pager | tail -10
# 看是否应用了 config.json
journalctl -u taps-daemon -n 20 | grep "applied overrides"
# 看 token / fingerprint 是否变（不应变；变了说明 token / cert 文件丢失）
journalctl -u taps-daemon -n 20 | grep -E 'token:|fingerprint:'
```

如果 `cert.pem` / `key.pem` 不在了（比如清理误删），daemon 会重新生成新证书 → **Panel 端必须 re-accept 新指纹**。

## 升级 Panel

```bash
# 假设新二进制 + web 在 /tmp/
systemctl stop taps-panel
mv /tmp/panel-linux-amd64 /opt/taps/panel
chmod +x /opt/taps/panel
rm -rf /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf /tmp/web.tar.gz -C /opt/taps/web
rm /tmp/web.tar.gz
systemctl start taps-panel
sleep 3

# 验证
systemctl is-active taps-panel
journalctl -u taps-panel -n 30 --no-pager | tail -15
# 应该看到 "panel listening on :24444"
# 应该看到 "panel connected: ..." 表示重连每个 daemon 成功
```

## DB Migration 自动应用

Panel 启动时 GORM `AutoMigrate` 自动：
- 加新表（如果新版引入）
- 加新字段（如 Batch #4 的 `tokens_invalid_before`、Batch #7 的 `expires_at`/`revoked_at`）
- 加新索引

**不会**：删字段、改字段类型、回滚。

如果升级日志看到 `record not found` 类警告（针对 `settings` 表的特定 key），那是新版引入的设置项还没用过 → 正常，会用默认值。

## 回滚

```bash
TS=最近备份的时间戳

systemctl stop taps-panel taps-daemon

# 恢复二进制
cp /opt/taps/backup/panel.$TS  /opt/taps/panel
cp /opt/taps/backup/daemon.$TS /opt/taps/daemon

# 恢复 DB（如果新版加了字段，旧版 panel 启动会忽略多余字段，OK）
cp /opt/taps/backup/panel.db.$TS /var/lib/taps/panel/panel.db

systemctl start taps-daemon
sleep 2
systemctl start taps-panel
```

> ⚠️ 如果新版本写过任何**新设置项**或 **API key 加了 expiresAt/revokedAt**，回滚后这些数据还在 DB 里但旧版 panel 不会用——不破坏功能，只是新功能"消失"。

## 升级前端（无需重启 Panel）

只换 web 静态资源不重启 panel：

```bash
rm -rf /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf /tmp/web.tar.gz -C /opt/taps/web
# Panel 自带的 http.FileServer 不缓存目录列表，下次浏览器请求即生效
```

让用户**强刷浏览器**（Ctrl+F5）以清掉 Vite 输出的 hashed 文件名缓存。

## 升级 systemd 单元

如果新版要求更多 env / 改 ExecStart / 加 `LimitNOFILE` 等：

```bash
vim /etc/systemd/system/taps-panel.service
systemctl daemon-reload
systemctl restart taps-panel
```
