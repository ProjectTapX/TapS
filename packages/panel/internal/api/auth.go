package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/captcha"
	"github.com/taps/panel/internal/config"
	"github.com/taps/panel/internal/model"
)

type AuthHandler struct {
	DB       *gorm.DB
	Cfg      *config.Config
	Settings *SettingsHandler // for captcha config lookup
	Limits   *AuthLimiters    // login + change-password rate limit
}

// dummyBcryptHash is a one-time bcrypt hash of a 32-byte random secret
// nobody knows; CheckPassword against it always returns false but burns
// the same CPU as the real-user path. Used to keep failed-login response
// time roughly constant whether or not the requested username exists,
// closing the timing-side-channel half of the user-enumeration vulner-
// ability. Generated lazily at first failed login so we don't pay the
// bcrypt cost at startup if no one ever logs in.
var (
	dummyBcryptHash     string
	dummyBcryptHashOnce sync.Once
)

func getDummyBcryptHash() string {
	dummyBcryptHashOnce.Do(func() {
		seed := make([]byte, 32)
		_, _ = rand.Read(seed)
		h, err := auth.HashPassword(hex.EncodeToString(seed))
		if err != nil {
			// Fallback to an obviously-invalid string; CheckPassword on
			// a malformed hash returns false fast — the timing leak
			// reopens until next start, but availability wins.
			log.Printf("[auth] could not generate dummy bcrypt hash: %v (timing-attack mitigation degraded)", err)
			dummyBcryptHash = "$2a$10$invalid"
			return
		}
		dummyBcryptHash = h
	})
	return dummyBcryptHash
}

// jwtTTL is a tiny helper so handlers don't have to repeat the
// settings lookup at every call site. Falls back to the default if
// the row is missing or out of range.
func jwtTTL(db *gorm.DB) time.Duration {
	ttl, _ := LoadAuthTimings(db)
	return ttl
}

type loginReq struct {
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	CaptchaToken string `json:"captchaToken"`
}

type loginResp struct {
	Token string  `json:"token"`
	User  userDTO `json:"user"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	ip := c.ClientIP()
	// auth.loginMethod gate: when admin has set OIDC-only, the local
	// password endpoint refuses. Returns 403 + a hint so the SPA can
	// render "use SSO" instead of "wrong password".
	if LoadLoginMethod(h.DB) == LoginMethodOIDCOnly {
		apiErr(c, http.StatusForbidden, "auth.password_login_disabled", "this panel is configured for SSO-only login")
		return
	}
	if h.Limits != nil {
		if ok, retry := h.Limits.Login.Check(ip); !ok {
			abortRateLimited(c, retry)
			return
		}
	}
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	// Record every attempt, win or lose, so admins can audit access.
	rec := func(u *model.User, success bool, reason string) {
		entry := &model.LoginLog{
			Time:      time.Now(),
			Username:  req.Username,
			Success:   success,
			Reason:    reason,
			IP:        ip,
			UserAgent: c.GetHeader("User-Agent"),
		}
		if u != nil {
			entry.UserID = u.ID
		}
		h.DB.Create(entry)
	}
	// On failure: count against the IP, sleep the suggested backoff
	// (so even pre-ban attempts cost the attacker time), then emit.
	// `internalReason` is the rich one we drop to log/login_logs for
	// admins; `code`+`msg` is what the user sees, intentionally
	// uniform across all auth failure modes so the response cannot be
	// used to distinguish "no such user" from "wrong password".
	failLogin := func(u *model.User, status int, code, msg, internalReason string) {
		if h.Limits != nil {
			_, backoff := h.Limits.Login.Fail(ip)
			if backoff > 0 {
				time.Sleep(backoff)
			}
		}
		rec(u, false, internalReason)
		apiErr(c, status, code, msg)
	}
	// Verify captcha BEFORE checking the user — keeps brute-force
	// scripts from learning which usernames exist via timing.
	captchaBypassed := ""
	if h.Settings != nil {
		ccfg := h.Settings.LoadCaptcha()
		if ccfg.Provider != "" && ccfg.Provider != captcha.ProviderNone {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
			defer cancel()
			if err := captcha.Verify(ctx, ccfg, req.CaptchaToken, c.ClientIP()); err != nil {
				if errors.Is(err, captcha.ErrConfig) {
					log.Printf("[captcha] config error — falling open for this login: %v", err)
					captchaBypassed = err.Error()
				} else {
					log.Printf("[captcha] login rejected: %v", err)
					failLogin(nil, http.StatusUnauthorized, "auth.captcha_failed", "captcha verification failed", "captcha: "+err.Error())
					return
				}
			}
		}
	}
	var u model.User
	// audit-2026-04-25 H3: case-insensitive username lookup. The DB
	// migration in store.go ensured all stored usernames are
	// lowercase; new writes go through normalizeUsername. Treat
	// inbound login as case-insensitive by lowering the probe.
	if err := h.DB.Where("username = ?", normalizeUsername(req.Username)).First(&u).Error; err != nil {
		// Run a dummy bcrypt comparison so timing matches the
		// password-mismatch path: an attacker probing the endpoint
		// can't tell "user does not exist" from "user exists, wrong
		// password" by response latency alone. Cost should match the
		// hashes auth.HashPassword produces (currently bcrypt cost 10).
		auth.CheckPassword(getDummyBcryptHash(), req.Password)
		failLogin(nil, http.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", "no such user: "+req.Username)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		failLogin(&u, http.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", "wrong password")
		return
	}
	tok, err := auth.IssueToken(h.Cfg.JWTSecret, u.ID, u.Role, jwtTTL(h.DB))
	if err != nil {
		rec(&u, false, "issue token failed")
		apiErr(c, http.StatusInternalServerError, "common.issue_token_failed", "issue token failed")
		return
	}
	if h.Limits != nil {
		// Successful login wipes the failure counter so a fat-fingered
		// password earlier doesn't reduce the budget for this user.
		h.Limits.Login.Reset(ip)
	}
	rec(&u, true, captchaBypassed)
	c.JSON(http.StatusOK, loginResp{Token: tok, User: userToDTO(&u)})
}

func (h *AuthHandler) Me(c *gin.Context) {
	uid, _ := c.Get(auth.CtxUserID)
	var u model.User
	if err := h.DB.First(&u, uid).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	c.JSON(http.StatusOK, userToDTO(&u))
}

type changePwdReq struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword" binding:"required,min=4"`
}

// ChangePassword lets a logged-in user (or someone in the must-change state)
// rotate their own password.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	// In oidc-only mode the password is unreachable from the login
	// page, so changing/setting one is a no-op that just confuses the
	// user. Refuse loudly here too — the UI hides this action, but
	// belt-and-suspenders for direct API callers and stale tabs.
	if LoadLoginMethod(h.DB) == LoginMethodOIDCOnly {
		apiErr(c, http.StatusForbidden, "common.password_changes_are_disabled", "password changes are disabled in oidc-only mode")
		return
	}
	ip := c.ClientIP()
	if h.Limits != nil {
		if ok, retry := h.Limits.ChangePw.Check(ip); !ok {
			abortRateLimited(c, retry)
			return
		}
	}
	var req changePwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	uid, _ := c.Get(auth.CtxUserID)
	var u model.User
	if err := h.DB.First(&u, uid).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	// Allow skipping the "old password" check in two cases:
	//   1. MustChangePassword (forced first-login rotation), or
	//   2. !HasPassword (SSO-only account claiming its first password).
	// Both states mean "no password the user knows yet", so requiring
	// one would be a deadlock.
	if !u.MustChangePassword && u.HasPassword {
		if !auth.CheckPassword(u.PasswordHash, req.OldPassword) {
			if h.Limits != nil {
				_, backoff := h.Limits.ChangePw.Fail(ip)
				if backoff > 0 {
					time.Sleep(backoff)
				}
			}
			apiErr(c, http.StatusUnauthorized, "user.wrong_current_password", "wrong current password")
			return
		}
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	u.PasswordHash = hash
	u.MustChangePassword = false
	u.HasPassword = true
	// Revoke *older* JWT sessions but leave the caller's own token
	// alive: set TokensInvalidBefore to (this token's iat - 1s) so the
	// middleware's `iat.After(TokensInvalidBefore)` check still passes
	// for the in-flight request. Any other session with iat <= cutoff
	// gets rejected on its next call. API-key requests have no iat —
	// fall back to time.Now() (kills all JWTs; the API key itself
	// keeps working because key auth doesn't consult this field).
	if v, ok := c.Get(auth.CtxIssuedAt); ok {
		if iat, ok2 := v.(time.Time); ok2 {
			u.TokensInvalidBefore = iat.Add(-time.Second)
		} else {
			u.TokensInvalidBefore = time.Now()
		}
	} else {
		u.TokensInvalidBefore = time.Now()
	}
	if err := h.DB.Save(&u).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	if h.Limits != nil {
		h.Limits.ChangePw.Reset(ip)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
