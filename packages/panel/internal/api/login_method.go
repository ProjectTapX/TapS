// auth.loginMethod controls which login channels are accepted by the
// panel. Three values:
//
//   "password-only"   — local username/password only; SSO buttons hidden
//   "oidc+password"   — both, default after Day 2 ships
//   "oidc-only"       — SSO only; password endpoint returns 403
//
// Default for fresh deployments is "password-only" so a panel with no
// SSO configured doesn't accidentally lock the admin out.
//
// Switching to "oidc-only" runs the Q1+ safety check: at least one
// admin-role user must already be bound to an enabled IdP. Otherwise
// admin "switch then bind later" would brick admin login.
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/taps/panel/internal/model"
)

const (
	sysKeyLoginMethod = "auth.loginMethod"

	LoginMethodPasswordOnly = "password-only"
	LoginMethodOIDCPassword = "oidc+password"
	LoginMethodOIDCOnly     = "oidc-only"
)

// LoadLoginMethod returns the current channel mode. Default is
// "password-only" so an unconfigured panel still works.
func LoadLoginMethod(db *gorm.DB) string {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyLoginMethod).Error; err != nil {
		return LoginMethodPasswordOnly
	}
	switch s.Value {
	case LoginMethodPasswordOnly, LoginMethodOIDCPassword, LoginMethodOIDCOnly:
		return s.Value
	default:
		return LoginMethodPasswordOnly
	}
}

type loginMethodDTO struct {
	Method string `json:"method"`
}

// PublicLoginMethod is intentionally unauthenticated — both the login
// page (before any token exists) and the per-user account page (where
// non-admins manage their SSO bindings) need to know which channels
// are active. Only the *write* side (SetLoginMethod) is admin-gated.
func PublicLoginMethod(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, loginMethodDTO{Method: LoadLoginMethod(db)})
	}
}

func (h *SettingsHandler) GetLoginMethod(c *gin.Context) {
	c.JSON(http.StatusOK, loginMethodDTO{Method: LoadLoginMethod(h.DB)})
}

func (h *SettingsHandler) SetLoginMethod(c *gin.Context) {
	var b loginMethodDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	switch b.Method {
	case LoginMethodPasswordOnly, LoginMethodOIDCPassword, LoginMethodOIDCOnly:
	default:
		apiErr(c, http.StatusBadRequest, "login_method.invalid_value",
			"method must be password-only / oidc+password / oidc-only")
		return
	}
	// Wrap guard + Save in one transaction so a concurrent
	// User.Delete / UnlinkMyIdentity can't slip in between the
	// "is there at least one bound admin?" check and the actual
	// switch to oidc-only. SQLite serializes writes globally, so
	// pairing this with transactional Delete/Unlink (see those
	// handlers) makes the two flows mutually exclusive in practice.
	// FOR UPDATE is a no-op on SQLite (gorm dialect drops it) but
	// documents intent — it'll engage automatically if the panel is
	// later switched to MySQL/PostgreSQL.
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if b.Method == LoginMethodOIDCOnly {
			if err := guardOIDCOnlyTx(tx); err != nil {
				return err
			}
		}
		return tx.Save(&model.Setting{Key: sysKeyLoginMethod, Value: b.Method}).Error
	})
	if err != nil {
		// guardOIDCOnlyTx returns sentinel errors we map to stable codes.
		switch {
		case errors.Is(err, errOIDCOnlyNoProvider):
			apiErr(c, http.StatusBadRequest, "login_method.requires_provider",
				"oidc-only requires at least one enabled SSO provider")
		case errors.Is(err, errOIDCOnlyNoBoundAdmin):
			apiErr(c, http.StatusBadRequest, "login_method.requires_bound_admin",
				"oidc-only requires at least one admin already bound to an enabled SSO provider; bind yourself first then switch")
		default:
			apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

var (
	errOIDCOnlyNoProvider   = errors.New("oidc-only requires at least one enabled SSO provider")
	errOIDCOnlyNoBoundAdmin = errors.New("oidc-only requires at least one admin already bound to an enabled SSO provider; bind yourself first then switch")
)

// guardOIDCOnlyTx is the transactional version of the oidc-only
// safety check. Both SELECTs grab a row-level UPDATE lock so that on
// engines that honor it (MySQL/Postgres) a concurrent admin delete
// against the same rows blocks until our Save commits. SQLite ignores
// the clause but file-level write serialization gives us most of the
// same guarantee in practice.
func guardOIDCOnlyTx(tx *gorm.DB) error {
	var enabledProviders int64
	if err := tx.Model(&model.SSOProvider{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("enabled = ?", true).Count(&enabledProviders).Error; err != nil {
		return err
	}
	if enabledProviders == 0 {
		return errOIDCOnlyNoProvider
	}
	var boundAdmins int64
	if err := tx.Table("sso_identities AS i").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Joins("JOIN users u ON u.id = i.user_id").
		Joins("JOIN sso_providers p ON p.id = i.provider_id").
		Where("u.role = ? AND p.enabled = ?", model.RoleAdmin, true).
		Count(&boundAdmins).Error; err != nil {
		return err
	}
	if boundAdmins == 0 {
		return errOIDCOnlyNoBoundAdmin
	}
	return nil
}

// guardOIDCOnly is kept as a thin wrapper for non-transactional callers
// (legacy paths). New code should use guardOIDCOnlyTx inside a
// db.Transaction so the read+write sequence is atomic.
func guardOIDCOnly(db *gorm.DB) error {
	return guardOIDCOnlyTx(db)
}
