**English** | [中文](CONTRIBUTING.zh-CN.md) | [日本語](CONTRIBUTING.ja.md)

# Contributing Guide

Thank you for your interest in TapS! Here's how to get involved.

## Submitting Issues

- **Bug reports**: Please include reproduction steps, Panel/Daemon version, browser version, and `journalctl` log snippets
- **Feature requests**: Describe your use case and expected behavior
- **Security vulnerabilities**: Please do **NOT** submit a public Issue. Email **hi@mail.mctap.org**

## Pull Request Workflow

1. Fork this repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Develop and test
4. Ensure the following checks pass:
   ```bash
   # Go build
   cd packages/panel && go build ./cmd/panel
   cd packages/daemon && go build ./cmd/daemon

   # Frontend build
   cd web && npm run build

   # i18n alignment check (zh/en/ja keys must match)
   node scripts/i18n-gap-check.js
   ```
5. Commit your changes: `git commit -m 'feat: add some feature'`
6. Push and create a Pull Request

## Commit Convention

We recommend [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: new feature
fix: bug fix
docs: documentation changes
style: code formatting (no functional change)
refactor: refactoring (neither new feature nor bug fix)
perf: performance optimization
test: test-related
chore: build/tooling/dependency changes
security: security-related fix
```

## Code Standards

### Go Backend

- Follow standard Go style (`gofmt`)
- All API errors use `apiErr(c, status, "domain.error_code", "message")` for uniform responses
- Error code format: `domain.snake_case` (e.g., `auth.invalid_credentials`, `fs.missing_path`)
- Multi-key settings writes must be wrapped in `db.Transaction`
- Register new API routes in `packages/panel/internal/api/router.go`
- Only write comments to explain "why", never "what"

### React Frontend

- TypeScript strict mode
- All user-visible text goes through i18n (`t('key')`); no hardcoded Chinese/English/Japanese
- New i18n keys must be added simultaneously to `web/src/i18n/zh.ts`, `en.ts`, and `ja.ts`
- Run `node scripts/i18n-gap-check.js` to verify three-language alignment
- State management with Zustand; API calls go in `web/src/api/`
- Error display uses `formatApiError(e)` to auto-lookup the i18n error code table

### Security

- User input must be validated (backend handler layer + frontend form rules)
- File operations must go through `fs.Resolve` (EvalSymlinks + containedIn)
- Docker CLI arguments must be validated via `ValidImage` / `validInstanceUUID`
- Never leak internal error details in URL fragments or error responses
- New settings endpoints should consider admin role requirements + transaction consistency

## Directory Structure

```
packages/panel/internal/api/   ← Panel HTTP API handlers
packages/daemon/internal/rpc/  ← Daemon WS RPC handlers
packages/shared/protocol/      ← Panel↔Daemon shared protocol
web/src/pages/                 ← React page components
web/src/i18n/                  ← Translation files (zh/en/ja)
docs/                          ← Project documentation
```

## Local Development

```bash
# Terminal 1 — Daemon
cd packages/daemon && go run ./cmd/daemon

# Terminal 2 — Panel
cd packages/panel && go run ./cmd/panel

# Terminal 3 — Frontend hot reload
cd web && npm install && npm run dev
# http://localhost:5173 auto-proxies /api → localhost:24444
```

Default account `admin` / `admin`; password change is required on first login.

## License

This project is licensed under [GPL-3.0](LICENSE). By submitting contributions, you agree that your code will be published under the same license.
