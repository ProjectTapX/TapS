# 单机部署（Panel + Daemon 同机）

把 Panel 和 Daemon 安装在同一台 Linux 主机上。最常见的"个人 / 小团队"模式：单台 VPS 跑面板 + 几个 Minecraft 实例。

## 前置要求

- Linux x86_64（推荐 Debian 12 / 13、Ubuntu 22.04+；其它 systemd 发行版同理）
- 已安装 Docker（Daemon 默认 `requireDocker=true`，所有实例跑容器）
- root 权限（Daemon 要管理 Docker、卷、网络）
- 开放 TCP 端口 **24444**（Panel HTTP）、**24445**（Daemon HTTPS）；如果用 nginx 反代见 [Nginx 反代](nginx-https.md)
- Minecraft 实例本身的端口（默认 25565+）

## 1. 准备目录

```bash
mkdir -p /opt/taps /var/lib/taps/panel /var/lib/taps/daemon
chmod 700 /var/lib/taps/panel /var/lib/taps/daemon
```

## 2. 放二进制 + Web

把交叉编译产物放进去（构建方法见 [development/build.md](../development/build.md)）：

```bash
mv panel-linux-amd64  /opt/taps/panel
mv daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/panel /opt/taps/daemon

# 解压前端静态产物到 /opt/taps/web
mkdir -p /opt/taps/web
tar -xzf web.tar.gz -C /opt/taps/web
```

最终结构：

```
/opt/taps/
├── panel
├── daemon
└── web/
    ├── index.html
    └── assets/

/var/lib/taps/
├── panel/    # panel 数据：panel.db, jwt.secret
└── daemon/   # daemon 数据：token, cert.pem, key.pem, files/, backups/, volumes/
```

> **数据目录里的所有文件首次启动会自动生成**（含 SQLite DB、JWT 密钥、Daemon Token、TLS 自签证书、`config.json.template` 示例）。无需手动初始化。

## 3. systemd 单元

```bash
cat >/etc/systemd/system/taps-daemon.service <<'EOF'
[Unit]
Description=TapS Daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/taps/daemon
WorkingDirectory=/opt/taps
Environment=TAPS_DAEMON_DATA=/var/lib/taps/daemon
Environment=TAPS_DAEMON_ADDR=:24445
Environment=TAPS_REQUIRE_DOCKER=true
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

cat >/etc/systemd/system/taps-panel.service <<'EOF'
[Unit]
Description=TapS Panel
After=network-online.target taps-daemon.service
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
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
```

## 4. 启动

**先启动 Daemon**（Panel 启动时会尝试连接已注册的节点；首次没有节点不影响，但生产环境推荐这个顺序）：

```bash
systemctl enable --now taps-daemon
sleep 2
systemctl enable --now taps-panel
```

确认状态：

```bash
systemctl status taps-daemon taps-panel
ss -lnt | grep -E '24444|24445'
```

应能看到 `*:24444` 和 `*:24445` 两个监听端口。

## 5. 抓取 Daemon 信息

```bash
# Daemon Token（添加节点时要填）
cat /var/lib/taps/daemon/token

# Daemon TLS 指纹（添加节点时要填，TOFU 验证用）
journalctl -u taps-daemon -n 30 --no-pager | grep "tls fingerprint"
```

或者让 Panel 后续自动 probe（见下一步）。

## 6. 首次登录 Panel

浏览器访问 `http://<服务器IP>:24444/`：

1. 用 `admin` / `admin` 登录（首次启动时由 `TAPS_ADMIN_USER`/`TAPS_ADMIN_PASS` 设置；DB 已存在则不再覆写）
2. 系统强制要求**修改密码**
3. 登录成功后进入仪表盘

## 7. 添加 Daemon 节点

进入「**节点管理**」→「**新增**」：

| 字段 | 值 |
|---|---|
| 名称 | `local`（任意） |
| 地址 | `127.0.0.1:24445` |
| 显示主机 | 玩家用来连接的对外域名/IP（例如 `play.example.com`），留空则回退到地址主机部分 |
| 端口范围 | 实例自动分配宿主端口的范围（默认 25565–25600） |
| Token | `cat /var/lib/taps/daemon/token` 的内容 |

接着点 **"抓取指纹"** 按钮：Panel 会用 TLS 探测 daemon 拿到指纹。**核对指纹**与 `journalctl ... grep "tls fingerprint"` 输出一致，点 **"接受并使用"** → **"保存"**。

节点列表里看到「已连接」即成功。

## 8. 创建第一个实例

进入「**实例管理**」→「**新建**」选模板（Vanilla / Paper / Purpur / 自定义 Docker），填名称、版本、内存、端口、磁盘，点确定。详见 [快速上手](../usage/quickstart.md)。

---

## 常见调整

### 改 Panel 端口
进入「**系统设置**」→「**Panel 监听端口**」，保存后重启：
```bash
systemctl restart taps-panel
```

### 改 Daemon 配置（端口 / 限频 / WS 帧大小）
两种方式：
- **临时 / 简单**：改 `/etc/systemd/system/taps-daemon.service` 的 `Environment=` 行 → `systemctl daemon-reload && systemctl restart taps-daemon`
- **持久 / 推荐**：复制 `/var/lib/taps/daemon/config.json.template` 到 `config.json` 编辑：
  ```bash
  cd /var/lib/taps/daemon
  cp config.json.template config.json
  vim config.json   # 删除你不想覆盖的项
  systemctl restart taps-daemon
  journalctl -u taps-daemon -n 5 | grep "applied overrides"
  ```
  优先级：`config.json` > env > 默认。

### 默认账号密码
首次启动时由环境变量决定。改了 `TAPS_ADMIN_USER`/`TAPS_ADMIN_PASS` **不会**修改已有用户——这两个 env 仅在 DB 第一次创建时使用。

### 启用 HTTPS
推荐走 nginx 反代：[Nginx 反代 + HTTPS](nginx-https.md)。

---

## 卸载

```bash
systemctl disable --now taps-panel taps-daemon
rm -f /etc/systemd/system/taps-{panel,daemon}.service
rm -rf /opt/taps /var/lib/taps
systemctl daemon-reload
# Docker 容器若以 taps- 前缀命名：docker rm -f $(docker ps -aq --filter name=taps-)
```
