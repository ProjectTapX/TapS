**[English](CHANGELOG.md)** | **中文** | [日本語](CHANGELOG.ja.md)

# 更新日志

本文件记录 TapS 各版本的主要变更。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)。

## [26.1.0] - 2026-04-26

首个公开发布版本。

### 新增

- **Panel + Daemon 双端架构**：Panel（Go + Gin + GORM + SQLite）负责 Web UI 和集中管控，Daemon（Go + gorilla/websocket + Docker CLI）部署在宿主机运行容器
- **React 前端**：Vite 5 + React 18 + TypeScript + Ant Design 5，支持深色/浅色主题切换
- **实例管理**：Docker 容器实例的创建/启停/强制终止/自动启动/崩溃自动重启
- **浏览器实时终端**：xterm.js + WebSocket，真 PTY，断线自动重连，本地行编辑 + Tab 补全
- **一键部署模板**：Vanilla / Paper / Purpur / Fabric / Forge / NeoForge，选版本即部署
- **文件管理器**：分片上传/流式下载/在线编辑/重命名/复制/移动/zip 压缩解压
- **备份还原**：实例级 zip 快照，支持备注，备份计入磁盘配额
- **托管卷**：loopback 固定大小卷，给每个实例独立的磁盘配额
- **资源监控**：节点 CPU/内存/磁盘实时仪表盘 + 历史曲线，单实例 Docker stats
- **自动休眠**：空闲检测 → 停容器 → 假 SLP 监听器 → 玩家连接即唤醒
- **节点分组**：多节点负载调度，按磁盘可用 + 内存最低自动选节点
- **计划任务**：cron 表达式，动作：命令/启动/停止/重启/备份
- **用户与权限**：admin / user 角色，按实例粒度授权
- **API Key**：`tps_` 前缀长期凭据，IP 白名单 + Scope + 过期时间
- **SSO / OIDC**：支持 Logto / Google / Microsoft / Keycloak 等标准 OIDC 提供商，PKCE + HMAC state
- **登录验证码**：Cloudflare Turnstile / reCAPTCHA Enterprise
- **Docker 镜像管理**：拉取/删除/自定义显示名称，OCI label 自动读取
- **多语言**：中文 / English / 日本語（926 key 三语对齐）
- **Webhook 通知**：节点掉线/恢复时推送 JSON（60s 防抖）

### 安全

- Content-Security-Policy（admin 可配 script-src / frame-src 白名单）
- X-Frame-Options / X-Content-Type-Options / Referrer-Policy / 条件 HSTS
- SSRF 防护：ClassifyHost 三分法 + DialContext DNS rebinding 重检
- 路径穿越防护：EvalSymlinks + containedIn + O_NOFOLLOW + zip symlink 拒绝
- JWT：HS256 + 滑动续期 + TokensInvalidBefore 吊销 + alg:none 拒绝
- bcrypt 密码哈希 + AES-GCM secrets at-rest
- 速率限制：login / changePw / apiKey / oauthStart 独立桶
- WS dispatch 并发上限（默认 8192）+ WS 帧大小上限
- HTTP server timeouts（slow-loris 防护）
- Graceful shutdown：SIGTERM → srv.Shutdown → hib.Shutdown → vm.UnmountAll
- 所有多键 settings 写入 db.Transaction
- 累计 99 项安全加固，六轮审计评级 A
