# 备份与恢复

## 三个层级的备份

| 层 | 内容 | 频率建议 | 工具 |
|---|---|---|---|
| **应用级** | 单个实例的工作目录 zip | 每天 / 重大修改前 | Panel UI「备份」标签 |
| **控制面级** | `panel.db` + `jwt.secret`（Panel） + `daemon/{token,cert.pem,key.pem,config.json}`（Daemon） | 每天 | rsync / tar |
| **宿主级** | `/opt/taps`、`/var/lib/taps`、Docker 卷、Docker 镜像 | 每周 / 灾备 | LVM / btrfs / ZFS 快照、云硬盘快照 |

## Panel + Daemon 关键文件清单

### Panel（`/var/lib/taps/panel/`）
```
panel.db           # 全部业务数据（用户、节点、实例权限、API key、设置、日志）
jwt.secret         # 签 JWT 用；删了所有 token 立即失效
```

### Daemon（`/var/lib/taps/daemon/`）
```
token              # Panel ↔ Daemon 共享密钥
cert.pem           # 自签 TLS 证书（Panel pin 它的指纹）
key.pem            # 上对应私钥
config.json        # 可选；admin 写的 env 覆盖
files/             # 通用文件根目录（generic 实例的工作目录、用户上传等）
backups/           # 应用级备份 zip
volumes/           # 托管卷 + docker 实例的 /data 目录（每实例 inst-<short> 子目录）
```

> `files/` 和 `volumes/` 是**业务数据**，可能很大；备份它们 = 备份所有实例的世界文件等。

## 简单 rsync 脚本

```bash
#!/bin/bash
# /usr/local/bin/taps-backup.sh
set -e
DATE=$(date +%Y%m%d-%H%M%S)
DEST=/srv/backup/taps/$DATE
mkdir -p $DEST

# Panel
cp /var/lib/taps/panel/panel.db   $DEST/
cp /var/lib/taps/panel/jwt.secret $DEST/

# Daemon
cp /var/lib/taps/daemon/token     $DEST/
cp /var/lib/taps/daemon/cert.pem  $DEST/
cp /var/lib/taps/daemon/key.pem   $DEST/
[ -f /var/lib/taps/daemon/config.json ] && cp /var/lib/taps/daemon/config.json $DEST/

# 实例数据 + 备份 zip
rsync -a /var/lib/taps/daemon/files/   $DEST/files/
rsync -a /var/lib/taps/daemon/backups/ $DEST/backups/
# volumes 通常很大，按需
# rsync -a /var/lib/taps/daemon/volumes/ $DEST/volumes/

# 清理 30 天前的备份
find /srv/backup/taps -maxdepth 1 -type d -mtime +30 -exec rm -rf {} +
```

加 cron：
```cron
0 4 * * *  /usr/local/bin/taps-backup.sh >> /var/log/taps-backup.log 2>&1
```

> ⚠️ Panel.db 是 SQLite，**热拷贝可能拷到不一致状态**。生产上建议用 `sqlite3 panel.db ".backup '/srv/backup/.../panel.db'"` 走 SQLite 自身的备份 API，能保证一致性。

```bash
sqlite3 /var/lib/taps/panel/panel.db ".backup '$DEST/panel.db'"
```

## 灾难恢复：从零重建 Panel

假设 Panel 主机彻底炸了，但你有备份：

```bash
# 1. 装一台新 panel 主机（按 panel-only.md 的 1-3 步建立目录、systemd）

# 2. 还原数据
systemctl stop taps-panel
cp /backup/.../panel.db   /var/lib/taps/panel/panel.db
cp /backup/.../jwt.secret /var/lib/taps/panel/jwt.secret
chmod 600 /var/lib/taps/panel/jwt.secret
systemctl start taps-panel

# 3. 验证
journalctl -u taps-panel -n 20 --no-pager | tail -10
# 应能看到 "panel listening" 和（如有节点）"panel connected" 给每个 daemon
```

只要 `panel.db` + `jwt.secret` 完整，所有用户、节点、API Key、设置一并恢复，且**用户已签发的 JWT 仍然有效**（jwt.secret 没变 + tokens_invalid_before 没变）。

## 灾难恢复：从零重建 Daemon

假设 Daemon 主机炸了：

```bash
# 1. 新机器装 daemon（按 daemon-only.md 的 1-2 步）

# 2. 还原
systemctl stop taps-daemon
cp /backup/.../token      /var/lib/taps/daemon/token
cp /backup/.../cert.pem   /var/lib/taps/daemon/cert.pem
cp /backup/.../key.pem    /var/lib/taps/daemon/key.pem
chmod 600 /var/lib/taps/daemon/{token,cert.pem,key.pem}
rsync -a /backup/.../files/   /var/lib/taps/daemon/files/
rsync -a /backup/.../backups/ /var/lib/taps/daemon/backups/

systemctl start taps-daemon
```

只要 token + cert/key 一致，**Panel 不需要重新 TOFU 抓指纹**——指纹仍然匹配。

如果 token 或 cert 丢了，需要用新 token 在 Panel 节点编辑里更新；用新 cert 需在节点编辑里 re-probe 指纹。

## 实例级"还原"

```bash
# 进入 Panel UI 的备份页 → 选目标 zip → 点"还原"
# 或 API:
curl -X POST -H "Authorization: Bearer $JWT" \
     -H "Content-Type: application/json" \
     -d '{"name":"<backup-zip>"}' \
     https://panel/api/daemons/$ID/instances/$UUID/backups/restore
```

会**覆盖现有同名文件**，不会增量同步。建议还原前先停实例。
