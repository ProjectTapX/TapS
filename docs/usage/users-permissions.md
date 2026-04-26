**English** | [中文](../zh/usage/users-permissions.md) | [日本語](../ja/usage/users-permissions.md)

# Users & Permissions

## Roles

| Role | Description |
|---|---|
| `admin` | Full access: all users / nodes / instances / settings |
| `user` | Regular user; can't see any instances by default, requires explicit admin authorization |
| `guest` | Reserved enum, **not currently enabled** (creating/updating users only accepts `admin` / `user`; passing `guest` returns 400) |

**Strict enum validation**: when admin creates/updates a user, the `role` field must be `admin` or `user`; invalid values are rejected with 400.

**Username/email case-insensitive**: auto `ToLower` + `TrimSpace` on creation and login, with SQLite using a `LOWER()` unique index. `Admin` and `admin` are treated as the same user.

## Last Admin Protection

- **Cannot demote the last admin** (HTTP 400 `cannot demote the last admin`)
- **Cannot delete the last admin** (HTTP 400 `cannot delete the last admin`)
- Prevents the system from losing all administrators and becoming locked out of admin routes

## Instance Permission Bits

Stored in the `instance_permissions` table, each row records a user's permission bitmask for an instance:

| Bit | Name | Meaning |
|---|---|---|
| 1 | `PermView` | View instance / monitoring / open terminal read-only |
| 2 | `PermControl` | Start / stop / restart / input + deploy server + edit instance config (partial fields) |
| 4 | `PermFiles` | File management + backup operations + file upload/download |
| 8 | `PermTerminal` | Terminal write (send commands to stdin) |
| 16 | `PermManage` | Edit full instance config (admin-equivalent; use with caution) |

Combinable, e.g., `PermView | PermFiles = 5`; `PermAll = 31` for full access.

**Admin role bypasses all permission checks** — admin = full access.

## Authorization Workflow

**"User Management"** → select user → **"Permissions"** → lists the user's existing instance permissions:

1. Click **"Add Permission"**
2. Select target node + instance + check permission bits
3. Save → takes effect immediately (middleware reads DB on next request)

**Revoke**: click the delete button at the end of the permission row → DB row deleted → user immediately loses access to that instance.

## User-Level Operations

| Endpoint / Page | Admin Required | Description |
|---|---|---|
| Create user | Yes | Set initial password + role |
| Change user password | Yes | Bumps `tokens_invalid_before` → all of that user's JWTs immediately invalidated |
| Change user role | Yes | Same as above, immediately revokes old tokens |
| Delete user | Yes | Cascade-deletes the user's API keys; login logs retained; **instance permissions (`InstancePermission`) do not currently cascade** — manually revoke if concerned about residual entries |
| View login logs | Yes | Global; shows IP / UA / failure reason |
| View audit logs | Yes | Global; records all POST/PUT/DELETE |
| Self-service password change | Any logged-in user | `/api/auth/me/password` |

## Self-Service Password Change

- Regular users can change password via top-right menu → **"Change Password"**
- On first login with `mustChangePassword=true`, all write requests are intercepted by the `EnforcePasswordChange` middleware to the password change page
- After changing, `tokens_invalid_before` is set → current JWT invalidated → must re-login

## API Key & User Relationship

- Each API Key belongs to a user and **inherits the user's role**
- When a user is deleted, all their keys are **cascade-deleted**
- **Password change does not auto-revoke** keys (by design: password change targets human login; keys are machine credentials with independent lifecycle)
- To bulk-invalidate keys: user can go to the API Key page and click "Revoke All My Keys", or admin can delete individually

See [API Key](api-keys.md) for details.

## Security Recommendations

- Give each person their **own account**; don't share admin
- Use **long passwords** for admin accounts (≥ 12 characters + complexity)
- Use **API Keys** for CI/scripts; don't put human account passwords in CI environment variables
- Regularly audit **login logs** for unusual IPs / UAs
- Password rotation: admin can set 90-day expiry cycles for sensitive accounts (manual reminders)
