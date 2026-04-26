[English](../README.md) | **中文** | [日本語](../ja/README.md)

# TapS 文档

TapS 是一个面向 Minecraft（及其它通用 Docker 容器化进程）的游戏服务器管理面板，由 **Panel**（控制面 + Web UI）和 **Daemon**（运行在每台目标主机的代理）组成。

Panel 通过 WSS + TLS 指纹 pin 与 Daemon 安全通信，提供实例编排、文件管理、备份还原、监控、定时任务、休眠唤醒、SSO/OIDC、API Key、用户与权限等能力。

**技术栈**：Go 1.25 + gin + gorm + SQLite（Panel/Daemon 后端）、React 18 + TypeScript + Vite + Ant Design（前端）、Docker CLI（实例运行时）。

---

## 📦 部署

| 文档 | 适用场景 |
|------|---------|
| [单机部署（Panel + Daemon 同机）](deployment/single-host.md) | 个人 / 小团队，所有功能在一台机器 |
| [仅部署 Panel](deployment/panel-only.md) | Panel 集中部署，远端连多台 Daemon |
| [仅部署 Daemon](deployment/daemon-only.md) | 在新机器上加节点，连入现有 Panel |
| [Nginx 反代 + HTTPS](deployment/nginx-https.md) | 用域名 + Let's Encrypt 证书暴露 Panel |

---

## 🚀 使用

| 文档 | 内容 |
|------|------|
| [快速上手](usage/quickstart.md) | 首次登录、添加节点、创建实例、连接玩家 |
| [实例管理](usage/instances.md) | 创建 / 启停 / 终端 / 部署 Paper/Vanilla / 休眠 |
| [文件与备份](usage/files.md) | 文件浏览、上传下载、备份 / 还原 |
| [用户与权限](usage/users-permissions.md) | 角色、按实例授权、API Key |
| [API Key](usage/api-keys.md) | 签发、过期、撤销、Scope |
| [SSO / OIDC](usage/sso-oidc.md) | 接入 Logto / Google / Microsoft / Keycloak / 自建 IdP |
| [系统设置](usage/settings.md) | 全部 17 个设置卡片详解（含 CSP、HTTP 超时、速率限制等） |

---

## 🔌 API

| 文档 | 内容 |
|------|------|
| [API 概览](api/overview.md) | 鉴权、错误格式、限频、CORS、安全 Header |
| [端点参考](api/endpoints.md) | 全部 HTTP / WS 端点列表（100+ 端点） |

---

## 🛠 运维

| 文档 | 内容 |
|------|------|
| [升级流程](operations/upgrade.md) | 平滑升级 Panel / Daemon |
| [备份与恢复](operations/backup-restore.md) | DB / 数据目录 / 灾难恢复 |
| [故障排查](operations/troubleshooting.md) | 常见问题与解法 |

---

## 🔒 安全

| 文档 | 内容 |
|------|------|
| [安全架构](security/architecture.md) | 99 项加固后的完整防御层清单 |
| [部署加固清单](security/best-practices.md) | 上线前必做项 |

---

## 🧑‍💻 开发

| 文档 | 内容 |
|------|------|
| [从源码构建](development/build.md) | 本地构建、交叉编译 |
| [项目结构](development/architecture.md) | 模块划分、关键代码导览 |

---

## 反馈与贡献

发 issue / PR 到项目仓库。提交安全漏洞请走私下渠道，不要公开 issue。
