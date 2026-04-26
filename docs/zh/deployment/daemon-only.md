# 仅部署 Daemon

把一台新主机加入到现有 Panel。Daemon 是真正运行 Minecraft 容器的代理。

## 适用场景

- 在已有 Panel 之外加一台游戏服务器节点
- Daemon 跑在 IDC / 家用宽带 / 海外 VPS，被远程 Panel 集中管理

## 前置要求

- Linux x86_64
- Docker 已装好（`docker version` 能跑通）
- root 权限
- 入站开放：
  - **24445**（Daemon HTTPS / wss，给 Panel 出站连接）
  - 实例本身要的端口（Minecraft 25565+ 等）
- 出站要求（取决于配置）：
  - Docker 拉镜像（docker.io、ghcr.io 等）
  - 服务端 jar 下载源（fastmirror / papermc.io）

## 1. 准备

```bash
mkdir -p /opt/taps /var/lib/taps/daemon
mv daemon-linux-amd64 /opt/taps/daemon
chmod +x /opt/taps/daemon
```

## 2. systemd 单元

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

systemctl daemon-reload
systemctl enable --now taps-daemon
```

## 3. 抓取 Token + TLS 指纹

Daemon 首次启动会自动生成：

```bash
echo "Token:"
cat /var/lib/taps/daemon/token

echo "TLS Fingerprint:"
journalctl -u taps-daemon -n 30 --no-pager | grep "tls fingerprint"
```

记下这两个值——加到 Panel 时要用。

## 4. 在 Panel 端添加节点

到现有 Panel 的「**节点管理**」→「**新增**」：

| 字段 | 值 |
|---|---|
| 名称 | 任意（例 `bj-1`） |
| 地址 | `<daemon-public-ip>:24445` 或 `<内网IP>:24445` |
| 显示主机 | 玩家连接的域名/IP |
| 端口范围 | 25565-25600 或自定义 |
| Token | 步骤 3 拿到的 token |

点 **"抓取指纹"** → 弹出指纹后**逐字节核对**与步骤 3 输出的指纹一致 → **"接受并使用"** → **"保存"**。

节点列表显示「已连接」 = 通信链路成功。

---

## Daemon 数据目录详解

| 文件 / 目录 | 自动生成 | 说明 |
|---|---|---|
| `token` | ✅ | 32 字节随机 hex；Panel ↔ Daemon 共享密钥 |
| `cert.pem` / `key.pem` | ✅ | ECDSA P-256 自签 99 年；Panel pin 它的 SHA-256 指纹 |
| `files/` | ✅ | 实例工作目录根（按 UUID 分子目录） |
| `backups/` | ✅ | 备份 zip 存储 |
| `volumes/` | ✅ | 托管卷挂载点（用 loopback img 限制磁盘配额） |
| `volumes/<name>.img` | 按需 | 卷创建时生成 |
| `volumes/<name>/` | 按需 | 卷挂载点 |
| `config.json.template` | ✅ | 示例配置，**每次启动重写以保持与版本同步** |
| `config.json` | ❌ | 可选 admin 编辑文件，覆盖 env 配置 |

## 关键运维操作

### 轮换 Daemon Token

```bash
# 在 Daemon 主机
rm /var/lib/taps/daemon/token
systemctl restart taps-daemon
cat /var/lib/taps/daemon/token   # 新 token

# 在 Panel UI 中编辑该节点 → Token 字段填新值 → 保存
```

### 轮换 Daemon TLS 证书

```bash
# 在 Daemon 主机
rm /var/lib/taps/daemon/{cert,key}.pem
systemctl restart taps-daemon
journalctl -u taps-daemon -n 10 | grep "tls fingerprint"

# 在 Panel UI 编辑该节点 → 点 "抓取指纹" → 核对新指纹 → "接受并使用" → 保存
```

### 调整 Daemon 配置（端口 / 限频 / WS 帧大小）

复制并编辑 `config.json`：

```bash
cd /var/lib/taps/daemon
cp config.json.template config.json
vim config.json   # 删除你不想覆盖的字段，留下要改的
systemctl restart taps-daemon
journalctl -u taps-daemon -n 5 | grep "applied overrides"
```

支持的字段（`config.json`）：

```json
{
  "addr":                ":24445",
  "requireDocker":       true,
  "rateLimitThreshold":  10,
  "rateLimitBanMinutes": 10,
  "maxWsFrameBytes":     16777216
}
```

优先级：**config.json > env > 默认**。

## 安全注意

- **Daemon 监听公网 = root 等价**：拿到 Token 即可创建挂宿主路径的容器。生产环境建议：
  - 把 `addr` 改成 `127.0.0.1:24445`，配合 SSH 隧道或 Tailscale 暴露给 Panel
  - 或在云防火墙上限制 24445 的源 IP 仅允许 Panel 主机
- **Token 文件 mode 0600**，只有 root 可读
- **TLS 指纹 pin** 防止 Panel 被中间人替换 Daemon——首次添加节点必须核对指纹
