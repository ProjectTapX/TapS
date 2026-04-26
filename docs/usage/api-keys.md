**English** | [中文](../zh/usage/api-keys.md) | [日本語](../ja/usage/api-keys.md)

# API Key

API Keys are long-lived credentials for machines (CI, scripts, external integrations). They share the same authentication middleware as human account JWTs but have an independent lifecycle.

## Format

- Fixed prefix `tps_`, followed by 48 hex characters (24 random bytes)
- Total length: 52 characters
- Example: `tps_3fe3c349dd703a4c8b...`

## Issuance

**"API Key"** page → **"New Key"**:

| Field | Description |
|---|---|
| Name | Display only, e.g., `ci-deploy` / `monitoring-prober` |
| IP Whitelist | Comma-separated IPs or CIDRs; empty = any IP |
| Scope | Comma-separated permission tags; empty = full access (inherits user role); see table below |
| Expiration | `Never` / 30/90/365 days / custom date |

Click **"OK"** → a **one-time-only** plaintext key is displayed; copy and store it securely. Once dismissed, it's **never shown again** (DB only stores SHA-256).

## Scope (Route Permission Tags)

Available values (CSV):

| Scope | Allowed API Groups |
|---|---|
| `instance.read` | View instances / monitoring / player list / node public metadata |
| `instance.control` | Start/stop / input / create / update / template deploy / serverdeploy |
| `files` | File management + backups |
| `tasks` | Scheduled tasks |
| `admin` | Users, nodes, settings, audit, API Key management |

Empty = no scope restriction (**full access**, limited only by role).

Example: monitoring script only needs read → `instance.read`; CI auto-restart script → `instance.read,instance.control`.

## Usage

```bash
curl -H "Authorization: Bearer tps_3fe3c349dd703a4c..." \
     https://taps.example.com/api/instances
```

Server-side flow:
1. Detects `tps_` prefix → API Key path
2. SHA-256 comparison → finds row
3. Validates IP whitelist (CIDR / exact IP)
4. Validates `revoked_at` is NULL
5. Validates `expires_at` > now (NULL = never expires)
6. Validates request scope match
7. 5 failures/min → bans that IP for 5 min (429 + Retry-After)

## Revoke vs Delete

Each key row has two buttons:

- **Revoke** (default color): sets `revoked_at` to now; row retained for audit; credential immediately invalidated
- **Delete** (red): physically deletes the row; irrecoverable

**"Revoke All My Keys"** button: sets `revoked_at = now` on **all non-revoked** keys under the current user at once, commonly used for "suspected key leak" emergencies.

## Expiration & Rotation

- At creation, choose 30 / 90 / 365 days / custom date / never
- After expiry, calls immediately return `401 invalid api key: api key expired`
- **Expired rows are not auto-deleted** — retained for audit; admin/user can manually delete to clean up
- Recommend 90-day cycles for CI credentials; deploy a new key before expiry, let the old one expire naturally

## Differences from JWT

| Dimension | JWT | API Key |
|---|---|---|
| Issuance | Signed on login | User manually creates |
| Revocation | Bump `tokens_invalid_before` (blanket all tokens) | Per-key `revoked_at` field (granular) |
| Default TTL | 1 hour (configurable) | Permanent (optional expiry at creation) |
| Sliding renewal | Yes (auto-refreshes when remaining < half TTL) | No (fixed credential) |
| Scope | No (follows user role) | Yes (CSV route group restriction) |
| IP whitelist | No | Yes |
| Best for | Browser interaction | CI / scripts / monitoring |

## Security Recommendations

- Give each CI task its **own key**; don't share
- Configure IP whitelist (CI runner egress IPs)
- Configure minimal scope (read-only monitoring → just `instance.read`)
- Rotate regularly (90 days)
- On suspected leak, immediately click **"Revoke All My Keys"** + change password
- **Never** put keys in code or commit to git; use GitHub Actions Secrets / GitLab CI Variables etc.
