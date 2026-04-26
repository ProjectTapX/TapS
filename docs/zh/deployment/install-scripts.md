[English](../../deployment/install-scripts.md) | **中文** | [日本語](../ja/deployment/install-scripts.md)

# 一键安装脚本

三个脚本可从最新的 GitHub Release 快速安装 TapS。它们会自动检测 CPU 架构（x86_64 / ARM64）、下载二进制文件、配置 systemd 服务并启动所有组件。

## 快速开始

```bash
# 单机部署（Panel + Daemon 在同一台机器上）— 最常见的方式
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash

# 仅安装 Panel（控制面板，不运行游戏实例）
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-panel.sh | bash

# 仅安装 Daemon（将游戏服务器节点添加到已有的 Panel）
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-daemon.sh | bash
```

## 前置条件

| 要求 | Panel | Daemon | 单机部署 |
|------|-------|--------|----------|
| Linux x86_64 或 ARM64 | Yes | Yes | Yes |
| Root 权限 | Yes | Yes | Yes |
| curl | Yes | Yes | Yes |
| Docker | No | Yes | Yes |
| systemd | Yes | Yes | Yes |

## 各脚本功能说明

### `install.sh`（单机部署）

1. 检测 CPU 架构
2. 从 GitHub 获取最新发布版本号
3. 提示输入配置项（端口、数据目录、管理员凭据）
4. 下载 `panel-linux-{arch}`、`daemon-linux-{arch}` 和 `web.tar.gz`
5. 创建数据目录并设置 `chmod 700` 权限
6. 为 `taps-daemon` 和 `taps-panel` 编写 systemd 单元文件
7. 先启动 Daemon，等待就绪后再启动 Panel
8. 输出 token、TLS 指纹和访问 URL

### `install-panel.sh`（仅 Panel）

1. 检测 CPU 架构
2. 获取最新发布版本号
3. 提示输入：端口、数据目录、Web 目录、管理员用户名/密码
4. 下载 `panel-linux-{arch}` 和 `web.tar.gz`
5. 创建 systemd 单元 `taps-panel`
6. 启动 Panel 并输出访问 URL

### `install-daemon.sh`（仅 Daemon）

1. 检测 CPU 架构
2. 获取最新发布版本号
3. 提示输入：监听地址、数据目录
4. 下载 `daemon-linux-{arch}`
5. 创建 systemd 单元 `taps-daemon`
6. 启动 Daemon 并输出 token 和 TLS 指纹

## 配置选项

所有选项均有合理的默认值 - 直接按 Enter 即可接受默认设置。

| 选项 | 默认值 | 脚本 |
|------|--------|------|
| Panel 监听端口 | `24444` | Panel, Single-Host |
| Panel 数据目录 | `/var/lib/taps/panel` | Panel, Single-Host |
| Web 静态文件目录 | `/opt/taps/web` | Panel, Single-Host |
| 管理员用户名 | `admin` | Panel, Single-Host |
| 管理员密码 | `admin` | Panel, Single-Host |
| Daemon 监听地址 | `:24445` | Daemon, Single-Host |
| Daemon 数据目录 | `/var/lib/taps/daemon` | Daemon, Single-Host |

## 安装后验证

```bash
# 检查服务状态
systemctl status taps-panel taps-daemon

# 检查监听端口
ss -lnt | grep -E '24444|24445'

# 查看 Daemon token（在 Panel 中添加节点时需要）
cat /var/lib/taps/daemon/token

# 查看 Daemon TLS 指纹
journalctl -u taps-daemon | grep "tls fingerprint"
```

## 代理支持

脚本使用 `curl` 进行下载。如果你在代理后面，请在运行前设置环境变量：

```bash
export HTTPS_PROXY=http://proxy:port
curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash
```

## 卸载

```bash
systemctl disable --now taps-panel taps-daemon
rm -f /etc/systemd/system/taps-{panel,daemon}.service
systemctl daemon-reload
rm -rf /opt/taps /var/lib/taps
```
