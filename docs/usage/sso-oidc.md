**English** | [中文](../zh/usage/sso-oidc.md) | [日本語](../ja/usage/sso-oidc.md)

# Single Sign-On (SSO / OIDC)

TapS Panel supports standard **OpenID Connect (OIDC)** login. Compatible with Google, Microsoft Entra ID, Keycloak, Casdoor, Logto, or any IdP that follows the OIDC specification and provides `.well-known/openid-configuration`.

> This document covers: admin IdP configuration, user binding/unbinding, login method switching, and recovery when locked out.

---

## 1. Prerequisites

1. **Panel must have an externally reachable address**. The OIDC callback is redirected by the IdP back to the browser, which then returns to Panel — it doesn't matter if the IdP can't reach Panel on the internal network/`localhost`, but the **browser must be able to access** Panel.
2. Pre-register an OIDC Client at your IdP and prepare:
   - **Issuer URL** (e.g., `https://accounts.google.com`, `https://login.microsoftonline.com/{tenantId}/v2.0`, self-hosted `https://sso.example.com/realms/master`)
   - **Client ID** + **Client Secret**

---

## 2. Configuration Steps (Admin)

### 2.1 Set Panel Public URL

Go to **System Settings → SSO → "Panel Public URL"**, enter the external URL (including protocol), e.g., `https://taps.example.com`. **All SSO flows will fail if this is not set.**

The callback URL is automatically: `<public URL>/api/oauth/callback/<provider slug>`.

### 2.2 Add Provider

**System Settings → SSO → Add Provider**, select a template (Google / Microsoft Entra / Keycloak / Logto / Custom). Required fields:

| Field | Description |
|---|---|
| Name (slug) | URL-safe identifier, **cannot be changed after saving**. Determines callback URL path |
| Display Name | Button text on the login page |
| Issuer | OIDC issuer URL; endpoints auto-discovered via `.well-known/openid-configuration` |
| Client ID / Secret | Credentials obtained when registering with the IdP; Secret is AES-256-GCM encrypted at rest |
| Scopes | Default `openid profile email`; add more as needed |
| Allowed Email Domains | Empty = no restriction; multiple separated by commas |
| Auto-Create Account | On = creates a local account when IdP email has no local match; Off = rejects login |
| Default Role | Only effective when "Auto-Create" is enabled |

Click **Test Issuer** to verify reachability, then save.

### 2.3 Register Callback URL at the IdP

**The modal footer displays** the provider's callback URL — copy it to the IdP console's "Redirect URI" whitelist.

### 2.4 Switch Login Method

**System Settings → SSO → Login Method**, choose one of three:

- `password-only` — default; login page shows only username/password
- `oidc+password` — both supported, **recommended**
- `oidc-only` — SSO only; password login rejected

> **Lockout prevention**: switching to `oidc-only` requires "at least one admin bound to an enabled SSO provider", otherwise the backend rejects it. The reason is obvious — if the IdP goes down, nobody can log in.

---

## 3. User Binding / Unbinding

Regular users see their bound SSO list in **avatar (top-right) → Account Settings**.

- **Bind**: click "Bind X", follows the SSO login flow to redirect to the IdP; after IdP approval, returns to Panel and auto-binds to the current account.
- **Unbind**: click "Unbind" on the list row. In `oidc-only` mode, unbinding the last binding is rejected (prevents self-lockout).
- **Auto-binding**: when a user first logs in via SSO, matching follows this order:
  1. `(provider, subject)` already exists → finds the original account
  2. IdP-returned email matches a local account's `email` → binds to that account
  3. No match → checks whether "Auto-Create" is enabled

Admins can manage any user's bindings via the User Management page (useful for unbinding locked-out colleagues).

---

## 4. Recovery After Lockout

If you unfortunately switched to `oidc-only` and broke the IdP, nobody can access the login page. On the Panel host's shell, run:

```bash
# Switch back to password login (preserves all SSO config; can switch back to oidc+password later)
./taps-panel reset-auth-method --to password-only
```

No Panel process restart required; the next login request will read the new value.

---

## 5. Security Notes

- **State + PKCE + Nonce** all enabled. State is HMAC-SHA256 signed (provider + nonce + expiry, 5-minute TTL). **PKCE verifier is stored in Panel process memory** (not in the URL), 10-minute TTL + maxEntries cap to prevent DoS.
- **Client Secret** is AES-256-GCM encrypted at rest; key file at `<dataDir>/secret-encryption.key` (auto-generated on first start; permissions 0600). **Losing this file = all secrets invalidated**; include it in backups.
- **Email case normalization**: IdP-returned email is `ToLower`'d at entry, preventing case variation bypasses of admin auto-bind rejection.
- **Callback error code mapping**: all callback failures use `CallbackError{Code, Err}` typed wrapper; URL fragment only passes stable codes (e.g., `sso.token_exchange_failed`), never leaking IdP internal errors to the browser. Audit logs retain the full internal error.
- **Token pass-through**: after OIDC success, Panel issues its own TapS JWT and passes it to the SPA via URL hash fragment (`#oauth-token=...`). The IdP's access/refresh tokens **never** enter the browser.

---

## 6. FAQ

**Q: Callback keeps reporting `redirect_uri_mismatch`?**
A: Check that the "Redirect URI" in the IdP console **exactly matches** (including protocol, port, `/api/oauth/callback/<slug>`). The slug can't be changed, so choose wisely from the start.

**Q: Can one local account bind to multiple IdPs?**
A: Yes. The Account Settings page allows binding multiple IdPs to the same account.

**Q: If a user changes their email at the IdP, does it auto-sync?**
A: On login, the latest email is written back to `sso_identities.email`, but **does not** change the local `users.email` (to avoid conflicts with the User Management page).

**Q: Deleting an SSO provider removes all bindings?**
A: Yes. Deletion cascades and clears all rows in `sso_identities` for that `provider_id`; local accounts themselves are retained.
