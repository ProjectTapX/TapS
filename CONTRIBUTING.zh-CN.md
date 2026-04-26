**[English](CONTRIBUTING.md)** | **中文** | [日本語](CONTRIBUTING.ja.md)

# 贡献指南

感谢你对 TapS 的关注！以下是参与贡献的流程和规范。

## 提交 Issue

- **Bug 反馈**：请提供复现步骤、Panel/Daemon 版本、浏览器版本、`journalctl` 日志片段
- **功能建议**：描述你的使用场景和期望的行为
- **安全漏洞**：请**不要**公开提交 Issue。发送邮件至 **hi@mail.mctap.org**

## Pull Request 流程

1. Fork 本仓库
2. 创建特性分支：`git checkout -b feature/my-feature`
3. 开发和测试
4. 确保以下检查通过：
   ```bash
   # Go 编译
   cd packages/panel && go build ./cmd/panel
   cd packages/daemon && go build ./cmd/daemon

   # 前端构建
   cd web && npm run build

   # i18n 对齐检查（中/英/日三语必须 key 一致）
   node scripts/i18n-gap-check.js
   ```
5. 提交更改：`git commit -m 'feat: add some feature'`
6. 推送并创建 Pull Request

## Commit 规范

建议使用 [Conventional Commits](https://www.conventionalcommits.org/) 格式：

```
feat: 新增功能
fix: 修复 bug
docs: 文档变更
style: 代码格式（不影响功能）
refactor: 重构（不新增功能也不修 bug）
perf: 性能优化
test: 测试相关
chore: 构建/工具/依赖变更
security: 安全相关修复
```

## 代码规范

### Go 后端

- 遵循标准 Go 代码风格（`gofmt`）
- 所有 API 错误使用 `apiErr(c, status, "domain.error_code", "message")` 统一返回
- 错误码格式：`domain.snake_case`（如 `auth.invalid_credentials`、`fs.missing_path`）
- 多键 settings 写入必须包在 `db.Transaction` 内
- 新增 API 路由在 `packages/panel/internal/api/router.go` 注册
- 不写注释，除非解释"为什么"而不是"做什么"

### React 前端

- TypeScript strict 模式
- 所有用户可见文本走 i18n（`t('key')`），不允许硬编码中文/英文/日文
- 新增 i18n key 必须同时在 `web/src/i18n/zh.ts`、`en.ts`、`ja.ts` 三个文件中添加
- 运行 `node scripts/i18n-gap-check.js` 确认三语对齐
- 状态管理用 Zustand；API 调用放 `web/src/api/` 目录
- 错误展示用 `formatApiError(e)` 自动查 i18n 错误码表

### 安全

- 用户输入必须校验（后端 handler 层 + 前端表单 rules）
- 文件操作必须经过 `fs.Resolve`（EvalSymlinks + containedIn）
- Docker CLI 参数必须经过 `ValidImage` / `validInstanceUUID` 校验
- 不在 URL fragment / 错误响应中泄漏内部错误细节
- 新增 settings 端点需考虑是否需要 admin 角色 + 事务一致性

## 目录结构

```
packages/panel/internal/api/   ← Panel HTTP API handler
packages/daemon/internal/rpc/  ← Daemon WS RPC handler
packages/shared/protocol/      ← Panel↔Daemon 共享协议
web/src/pages/                 ← React 页面组件
web/src/i18n/                  ← 翻译文件（zh/en/ja）
docs/                          ← 项目文档
```

## 本地开发

```bash
# 终端 1 — Daemon
cd packages/daemon && go run ./cmd/daemon

# 终端 2 — Panel
cd packages/panel && go run ./cmd/panel

# 终端 3 — 前端热更新
cd web && npm install && npm run dev
# http://localhost:5173 自动代理 /api → localhost:24444
```

默认账号 `admin` / `admin`，首次登录强制改密。

## 许可证

本项目使用 [GPL-3.0](LICENSE) 协议。提交贡献即表示你同意你的代码在同一协议下发布。
