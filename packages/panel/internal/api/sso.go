// HTTP entry points for the OIDC login flow:
//
//   GET /api/oauth/providers           — public; list enabled IdPs for login page
//   GET /api/oauth/start/:name         — public; 302 to IdP authorize URL
//   GET /api/oauth/callback/:name      — public; IdP returns here, sign TapS JWT
//
// The callback's success path 302s to "<publicURL>/#oauth-token=<jwt>"
// so the SPA's main.tsx can pull the token out of the hash fragment
// and stash it in localStorage. Failure paths redirect to
// /#oauth-error=<urlEncoded message> so the login page can display it.
package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/config"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/panel/internal/sso"
)

type SSOHandler struct {
	DB      *gorm.DB
	Cfg     *config.Config
	Manager *sso.Manager
	// Limits is the same AuthLimiters bundle used by /auth/login etc.
	// We borrow the OAuthStart bucket to throttle anonymous bursts on
	// /api/oauth/start that would otherwise fill the in-memory PKCE
	// store. Pass nil in tests to disable throttling.
	Limits *AuthLimiters
}

// publicProvider is what the login page receives — never includes
// the client secret or anything that could enable a second flow
// from the browser.
type publicProvider struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// Providers returns the enabled IdPs for the login page button row.
// Public endpoint — no auth required (the login page hits this
// before the user has signed in).
//
// In password-only mode we return an empty list — the SPA renders
// no SSO buttons even if the admin left providers enabled in the DB
// (they might be testing config or staging a future switch).
func (h *SSOHandler) Providers(c *gin.Context) {
	method := LoadLoginMethod(h.DB)
	if method == LoginMethodPasswordOnly {
		c.JSON(http.StatusOK, []publicProvider{})
		return
	}
	var rows []model.SSOProvider
	if err := h.DB.Where("enabled = ?", true).Order("display_name asc").Find(&rows).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	out := make([]publicProvider, 0, len(rows))
	for _, r := range rows {
		out = append(out, publicProvider{Name: r.Name, DisplayName: r.DisplayName})
	}
	c.JSON(http.StatusOK, out)
}

// Start redirects the browser to the IdP's authorize URL. Public
// endpoint. Errors render as a redirect back to /#oauth-error=...
// so the user lands in the login page with a visible message
// instead of a raw 500.
func (h *SSOHandler) Start(c *gin.Context) {
	if LoadLoginMethod(h.DB) == LoginMethodPasswordOnly {
		h.failRedirect(c, "sso.password_only_mode", errors.New("panel configured for password-only login"))
		return
	}
	// Per-IP throttle (audit N2a). Hitting the cap on this anonymous
	// endpoint is the single cheapest way to OOM the server-side PKCE
	// store, so we redirect to the login page with a code the SPA
	// renders in the user's language — same UX as a real IdP failure.
	if h.Limits != nil && h.Limits.OAuthStart != nil {
		ip := c.ClientIP()
		if ok, _ := h.Limits.OAuthStart.Check(ip); !ok {
			h.Limits.OAuthStart.Fail(ip) // accumulate so the ban renews
			h.failRedirect(c, "auth.rate_limited", errors.New("oauth-start rate limit exceeded"))
			return
		}
		// Each Start call counts toward the budget regardless of
		// outcome — without this an attacker could open millions of
		// "successful" Starts to fill PKCE without ever tripping a
		// counter that only increments on Fail.
		h.Limits.OAuthStart.Fail(ip)
	}
	name := c.Param("name")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	url, err := h.Manager.BuildAuthURL(ctx, name)
	if err != nil {
		// PKCE store at capacity counts as anti-flood, surface as the
		// generic rate-limit error so attackers can't distinguish
		// "store is full" from "you're throttled" via response text.
		if errors.Is(err, sso.ErrPKCEStoreFull) {
			h.failRedirect(c, "auth.rate_limited", err)
			return
		}
		h.failRedirect(c, "sso.start_failed", err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

// idpErrorRe matches the OAuth2-spec character set for the `error`
// parameter (RFC 6749 §5.2 - error codes are token68-ish: ASCII
// letters, digits, dot, hyphen, underscore). Anything else is almost
// certainly an attacker probing log-injection / XSS / CSV-injection
// surfaces, not a real IdP response.
var idpErrorRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

func (h *SSOHandler) Callback(c *gin.Context) {
	name := c.Param("name")
	code := c.Query("code")
	state := c.Query("state")
	if errStr := c.Query("error"); errStr != "" {
		// IdP-side rejection (e.g. user clicked "deny"). Untrusted
		// input — cap and whitelist before it reaches LoginLog.Reason
		// and the front-end fragment. See B6 in the audit.
		if len(errStr) > 64 {
			errStr = errStr[:64]
		}
		if !idpErrorRe.MatchString(errStr) {
			errStr = "invalid_error_code"
		}
		// Audit-2026-04-25-v2 MED17: surface the IdP-side error using
		// our typed model. URL fragment shows only the stable code;
		// the audit log keeps the (validated) IdP error string for
		// admin visibility.
		h.failRedirect(c, "sso.idp_rejected", errors.New("idp_rejected: "+errStr))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.Manager.Callback(ctx, name, code, state)
	if err != nil {
		// Audit-2026-04-25-v2 MED17: pull the stable code out of the
		// typed CallbackError so the URL fragment stays code-only.
		// Anything not wrapped (shouldn't happen with current code,
		// but defends against future regressions) falls back to a
		// generic sso.unknown — admins still see the underlying error
		// in the audit log.
		var ce *sso.CallbackError
		if errors.As(err, &ce) {
			h.failRedirect(c, ce.Code, ce.Err)
		} else {
			h.failRedirect(c, "sso.unknown", err)
		}
		return
	}
	ttl := jwtTTL(h.DB)
	jwt, err := auth.IssueToken(h.Cfg.JWTSecret, res.User.ID, res.User.Role, ttl)
	if err != nil {
		h.failRedirect(c, "sso.token_issue_failed", err)
		return
	}
	// Audit: record this as a successful login. Reuses LoginLog table.
	h.DB.Create(&model.LoginLog{
		Time:      time.Now(),
		UserID:    res.User.ID,
		Username:  res.User.Username,
		Success:   true,
		Reason:    "sso:" + name,
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})

	base := h.Manager.PublicURL()
	if base == "" {
		base = "" // SPA at root
	}
	target := base + "/#oauth-token=" + url.QueryEscape(jwt)
	c.Redirect(http.StatusFound, target)
}

// failRedirect logs the failure (with the full internal err for
// admin visibility) and redirects the browser to the SPA with a
// stable error *code* in the URL fragment. Audit-2026-04-25-v2
// MED17: the URL fragment never carries free-form error strings any
// more — only `code` makes it out, and the SPA looks the
// translation up via i18n. Internal err details land in
// LoginLog.Reason, which is admin-only audit data.
func (h *SSOHandler) failRedirect(c *gin.Context, code string, internalErr error) {
	internal := ""
	if internalErr != nil {
		internal = internalErr.Error()
	}
	// Belt-and-suspenders cap on the audit-log payload (URL fragment
	// only contains `code` so it's already short). Callers
	// (Callback / Manager.Callback) constrain their inputs, but keep
	// the upper bound here so a future bug can't flood login_logs
	// with a 1MB error string from the IdP.
	if len(internal) > 256 {
		internal = internal[:256]
	}
	reason := "sso:" + c.Param("name") + ": " + code
	if internal != "" {
		reason += " (" + internal + ")"
	}
	h.DB.Create(&model.LoginLog{
		Time:      time.Now(),
		Username:  "(sso)",
		Success:   false,
		Reason:    reason,
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})
	base := h.Manager.PublicURL()
	target := base + "/#oauth-error=" + url.QueryEscape(code)
	c.Redirect(http.StatusFound, target)
}

// MyIdentities lists the SSO providers the *current* (logged-in)
// user has bound. Used by the account-settings page to show the
// "your linked SSO" list. Returns provider name + display name only;
// no internal IDs leaked.
type myIdentityView struct {
	ID                  uint      `json:"id"`
	ProviderName        string    `json:"providerName"`
	ProviderDisplayName string    `json:"providerDisplayName"`
	Email               string    `json:"email"`
	LinkedAt            time.Time `json:"linkedAt"`
	LastUsedAt          time.Time `json:"lastUsedAt"`
}

func (h *SSOHandler) MyIdentities(c *gin.Context) {
	uid, _ := c.Get("uid")
	var rows []model.SSOIdentity
	if err := h.DB.Where("user_id = ?", uid).Order("linked_at desc").Find(&rows).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	out := make([]myIdentityView, 0, len(rows))
	for _, r := range rows {
		var p model.SSOProvider
		if err := h.DB.First(&p, r.ProviderID).Error; err != nil {
			continue // provider deleted; skip
		}
		out = append(out, myIdentityView{
			ID: r.ID, ProviderName: p.Name, ProviderDisplayName: p.DisplayName,
			Email: r.Email, LinkedAt: r.LinkedAt, LastUsedAt: r.LastUsedAt,
		})
	}
	c.JSON(http.StatusOK, out)
}

// UnlinkMyIdentity drops one binding. Self-only — admin manages
// other users' bindings via the user admin page. Two refusal cases:
//
//   1. Any unlink in oidc-only mode (every binding is a live login
//      method; removing one shrinks the user's surface and removing
//      all locks them out).
//   2. The last binding when only oidc-only mode is in effect (kept
//      separately for clarity even though case 1 already covers it
//      — leaves the warning useful if the policy ever loosens).
func (h *SSOHandler) UnlinkMyIdentity(c *gin.Context) {
	uid, _ := c.Get("uid")
	id := c.Param("id")
	if LoadLoginMethod(h.DB) == LoginMethodOIDCOnly {
		apiErr(c, http.StatusBadRequest, "sso.unlink_disabled_oidc_only",
			"cannot unlink SSO bindings while panel is in oidc-only mode")
		return
	}
	// Wrap in a transaction so the "is this the user's last login
	// channel?" guard and the actual delete are atomic against any
	// concurrent admin-side mutation (User.Delete, password reset,
	// another binding being added). Pairs with the transactions in
	// SetLoginMethod / User.Delete to make the three flows mutually
	// exclusive on SQLite — see B4 in the audit.
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var row model.SSOIdentity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", id, uid).First(&row).Error; err != nil {
			return errBindingNotFound
		}
		// Defense against self-bricking (B5): an SSO-auto-created
		// account has HasPassword=false and a random throwaway
		// password it doesn't know. Removing the last SSO binding
		// would leave it with literally no way to log in. Refuse and
		// hint at the recovery path. Pre-fix this only checked the
		// oidc-only mode flag above; in oidc+password mode users
		// could happily brick themselves.
		var u model.User
		if err := tx.Select("id", "has_password").First(&u, uid).Error; err != nil {
			return err
		}
		if !u.HasPassword {
			var remaining int64
			if err := tx.Model(&model.SSOIdentity{}).
				Where("user_id = ? AND id != ?", uid, row.ID).Count(&remaining).Error; err != nil {
				return err
			}
			if remaining == 0 {
				return errLastBindingNoPassword
			}
		}
		return tx.Delete(&row).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, errBindingNotFound):
			apiErr(c, http.StatusNotFound, "sso.binding_not_found", "binding not found")
		case errors.Is(err, errLastBindingNoPassword):
			apiErr(c, http.StatusBadRequest, "sso.unlink_no_password_left",
				"Set a local password first before unlinking your last SSO binding")
		default:
			apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

var (
	errBindingNotFound       = errors.New("binding not found")
	errLastBindingNoPassword = errors.New("set a local password first")
)
