# 仅部署 Panel

Panel 是控制面 + Web UI，自身不运行任何 Minecraft 实例。把它单独部署在一台轻量主机（云轻量、家用 NAS、内网管理机），通过 wss 连接多台远端 Daemon。

## 适用场景

- 多机集群：1 台 Panel 管 N 台 Daemon（每台跑游戏服务器）
- Panel 放在公网 / Daemon 全部在内网（Panel 出站连 Daemon）
- 把 Panel 跟 nginx / Caddy 部署在前端机，业务节点纯算力

## 前置要求

- Linux x86_64（Panel 不依赖 Docker）
- 开放 TCP **24444**（HTTP；上 nginx 反代后只暴露 443）
- Panel 主机能**主动**连到每台 Daemon 的 24445 端口（wss 出站）

## 1. 准备目录

```bash
mkdir -p /opt/taps /var/lib/taps/panel
chmod 700 /var/lib/taps/panel
```

## 2. 放二进制 + Web

```bash
mv panel-linux-amd64 /opt/taps/panel
chmod +x /opt/taps/panel
mkdir -p /opt/taps/web
tar -xzf web.tar.gz -C /opt/taps/web
```

## 3. systemd 单元

```bash
cat >/etc/systemd/system/taps-panel.service <<'EOF'
[Unit]
Description=TapS Panel
After=network-online.target
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
# 如果你想 panel 直接 https（不走 nginx），取消下面两行注释并提供证书
# Environment=TAPS_TLS_CERT=/etc/letsencrypt/live/example.com/fullchain.pem
# Environment=TAPS_TLS_KEY=/etc/letsencrypt/live/example.com/privkey.pem
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now taps-panel
```

确认：

```bash
systemctl status taps-panel
ss -lnt | grep 24444
```

## 4. 首次登录 + 改密

`http://<panel-host>:24444/`，admin/admin → 强制改密。

## 5. 加远端 Daemon 节点

确保 Panel 主机能连到 Daemon 主机：

```bash
# 在 Panel 主机上 SSH 测试
nc -zv <daemon-host> 24445
# 用 openssl 拉取 daemon 指纹（可选，先了解期望值）
echo | openssl s_client -connect <daemon-host>:24445 2>/dev/null | openssl x509 -fingerprint -sha256 -noout
```

进入 Panel 「**节点管理**」→「**新增**」：

| 字段 | 值 |
|---|---|
| 名称 | `node-1` |
| 地址 | `<daemon-host>:24445` |
| 显示主机 | 玩家用的对外域名/IP |
| 端口范围 | 25565-25600 |
| Token | Daemon 主机上 `cat /var/lib/taps/daemon/token` |

点 **"抓取指纹"** → 核对（与 daemon 启动日志一致）→ **"接受并使用"** → **"保存"**。

新增多台 Daemon 重复以上步骤即可。

## 后续

- Daemon 端如何独立部署：[仅部署 Daemon](daemon-only.md)
- 用 nginx 反代加 HTTPS：[Nginx 反代 + HTTPS](nginx-https.md)
- 节点分组（让"创建实例"自动选最空闲的节点）：在 Panel「节点分组」页配置

---

## Panel 数据目录内容

| 文件 | 自动生成 | 备注 |
|---|---|---|
| `panel.db` | ✅ | SQLite，全部业务数据；GORM AutoMigrate 自动建表 + 加列 |
| `jwt.secret` | ✅ | 96 字符 hex；删了所有 JWT 立即失效 |

不存在 `config.json` 概念（Panel 配置全部走 env + 系统设置 DB）。
