# 实例管理

## 状态机

```
┌────────┐  start    ┌─────────┐  ready    ┌─────────┐
│stopped │ ────────> │starting │ ────────> │ running │
└────┬───┘           └─────────┘           └────┬────┘
     │                                          │
     │   delete                            stop │
     │                                          v
     │                                     ┌─────────┐
     │                                     │stopping │
     │                                     └────┬────┘
     │                                          │
     │                                          v
     │            ┌─────────┐  exit ≠ 0    ┌────────┐
     │            │ crashed │ <─────────── │stopped │
     │            └─────────┘              └────────┘
     v
   (gone)
```

加上自动休眠状态：

```
running ──idle──> hibernating ──client connect──> starting ──> running
```

## 创建

「**实例管理**」→「**新建**」→ 选类型：
- **模板**（Vanilla / Paper / Purpur / Fabric / Forge / NeoForge）：快速向导 + 自动下 jar
- **Docker**：自由指定镜像 / 挂载 / 端口

字段说明：

| 字段 | 用途 |
|---|---|
| 名称 | 显示用，可重名 |
| 节点 | 部署到哪台 Daemon（或选择分组让 Panel 自动选） |
| 工作目录 | 留空 = `<DataDir>/files` 根；填相对路径会拼到该根下；填绝对路径直接用。docker 实例的 `/data` 卷由"磁盘配额"自动建在 `<DataDir>/volumes/inst-<short>/`，与本字段无关 |
| 命令 | type=docker 时是镜像名（`itzg/minecraft-server`）。选择器优先显示 admin 设的显示名称（镜像页可编辑）；type=generic 时是可执行文件 |
| 参数 | 命令的 args（数组） |
| 停止指令 | 写到 stdin 的命令，例 `stop`（玩家友好关服） |
| 自动启动 | Daemon 启动时是否自动拉起这个实例 |
| 自动重启 | crashed 后是否自动重启 |
| 重启延迟 | 自动重启前等待秒数（默认 5） |

## 启停

- **启动** / **停止**：调用 stopCmd（默认 stdin 写 `stop`，给 MC 优雅关服时间）
- **强制结束**：直接 `docker kill` / `SIGKILL`
- **重启**：stop → 等待退出 → start（可在终端里 `say` 通知玩家）

## 终端

实例详情页 →「**终端**」：xterm.js 全功能终端，**实时**收 stdout，**键盘**发 stdin。

权限要求：
- **打开**终端：`PermView`
- **发送输入**：`PermTerminal` 或 `PermControl`

只读用户能看到滚动历史，键入会被静默丢弃。

## 文件管理

实例详情页 →「**文件**」标签：浏览 / 上传 / 下载 / 编辑 / 压缩 / 解压 / 重命名 / 移动 / 复制 / 删除。

详见 [文件与备份](files.md)。

## 备份

实例详情页 →「**备份**」标签：

- **创建备份**：把当前工作目录全量打 zip 存到 `backups/<uuid>/<时间戳>.zip`，可填备注
- **下载**：导出 zip
- **还原**：解压回工作目录（覆盖现有）
- **删除**：移除单个备份 zip

备份会算进**实例的磁盘配额**（如果走托管卷）。

## 监控

实例详情页 →「**监控**」标签：CPU / 内存 / 磁盘 / 网络实时曲线（每 5 秒一个采样点）。

## 玩家列表

Minecraft 实例自动 SLP 探测，详情页显示在线玩家数 + 名字。

## 删除实例

实例详情页 →「**删除**」：

- 容器停止并 `docker rm`
- 工作目录 / 托管卷**保留**（防误删）；要彻底清磁盘需在「文件管理」手动删除

## 自动休眠（仅 Minecraft Java）

启用后空闲 N 分钟自动停容器，Panel 在原端口跑假 SLP listener：
- 玩家在客户端服务器列表看到自定义 MOTD + 图标
- 玩家点连接 → 触发唤醒 → 真实容器启动 → 倒计时 N 秒后玩家可正式进入

配置：「系统设置」→「Minecraft Java 服务器自动休眠」全局默认；每实例可在编辑页覆盖（`hibernationEnabled`、`hibernationIdleMinutes`）。
