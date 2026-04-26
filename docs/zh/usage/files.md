# 文件与备份

## 文件浏览器

实例详情页 →「**文件**」标签。

普通用户只能看到自己有权限的实例目录（`/data/inst-<short>` 子树，short 是实例 UUID 去除连字符后的前 12 位 hex）。Admin 看整棵 `/data` 和 `/files`。

### 操作

| 操作 | 备注 |
|---|---|
| 上传 | 分片协议（init → chunk × N），自动按 1 MiB 切分；每文件先 init 检查配额 |
| 下载 | 流式，直接 `Content-Disposition: attachment` |
| 编辑 | 单文件 ≤ 4 MiB 可在线编辑（更大走 read-only） |
| 新建文件夹 / 重命名 / 复制 / 移动 / 删除 | 标准操作 |
| 压缩 zip | 把多个选中项打成一个 zip |
| 解压 zip | 自动防 zip-slip |

### 上传配额

每次上传**先调 init**：
- Daemon 用 `statfs` 算出该实例数据目录所在卷的剩余空间
- 总声明字节数 > 剩余空间 → **HTTP 507 quota_exceeded**
- 通过则返回 `uploadId`，后续每个分片必须带 `?uploadId=`

上传中途客户端崩溃没 `final=true`：1 小时后 daemon 自动清理 `.partial` 文件。

### 已知限制

- 单分片最大 1 GiB（daemon 端 `MaxBytesReader`）
- 总文件大小受卷剩余空间限制
- 不支持流式压缩 / 解压（压缩在 daemon 内存里组装 zip）

## 备份

「**备份**」标签：

| 操作 | 行为 |
|---|---|
| 创建 | zip 整个实例工作目录 → 存到 `backups/<uuid>/<timestamp>-<note>.zip` |
| 列出 | 显示 zip 名、大小、时间、备注 |
| 下载 | 导出 zip 到本地 |
| 还原 | 解压回工作目录（**会覆盖现有同名文件**） |
| 删除 | 删除单个 zip |

### 备份名校验

`name` 字段强制正则 `^[A-Za-z0-9._-]{1,128}$`（防路径穿越 / shell 字符）。
`note` 字段最长 512 字符，不可含换行符。

### 备份的存储位置

- 如果实例配置了**托管卷**（创建时填了"磁盘配额"）：备份存在该卷内的 `.taps-backups/` 子目录
- 否则存在 daemon 数据目录 `/var/lib/taps/daemon/backups/<uuid>/`

前者会算进卷的配额（防止备份把磁盘填满）；后者不算配额。

### 备份策略建议

- 定时任务里设个 `command` 类型 → 提前 `say` 通知玩家
- 用 cron 加另一个 `restart` 任务，时间错开
- 重要服务器用宿主机层级的快照（LVM / btrfs / ZFS）做二次保护——TapS 备份是**应用级**，不防止 daemon 主机本身故障
