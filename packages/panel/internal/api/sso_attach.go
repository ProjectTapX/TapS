// Wires SSO routes onto an existing gin engine, called from
// cmd/panel/main.go after NewRouter. Kept separate so the giant
// router.go doesn't need to grow another set of imports — and so
// admins building without the SSO feature can drop this one file.
package api

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/config"
	"github.com/ProjectTapX/TapS/packages/panel/internal/secrets"
	"github.com/ProjectTapX/TapS/packages/panel/internal/sso"
)

// settingsHandlerHook is set by router.go right after it constructs
// the singleton SettingsHandler. AttachSSO uses it to inject the SSO
// manager so the panel-public-url settings endpoint can hot-update
// callback URLs without restart.
var (
	settingsHandlerHook *SettingsHandler
	limitsHook          *AuthLimiters
)

func recordSettingsHandler(h *SettingsHandler) { settingsHandlerHook = h }

// recordLimits captures the per-process AuthLimiters bundle so
// AttachSSO can hand the OAuthStart bucket to the public SSO
// handler. router.go calls this once during NewRouter; sso_attach
// reads it on the same boot, no race.
func recordLimits(l *AuthLimiters) { limitsHook = l }

// AttachSSO registers all SSO endpoints.
//
// Public (no auth, on the bare engine):
//   GET  /api/oauth/providers           — login page button list
//   GET  /api/oauth/start/:name         — 302 to IdP
//   GET  /api/oauth/callback/:name      — IdP returns here, signs JWT
//   GET  /api/auth/login-method         — read-only; admin SET lives in router.go
//
// Authed (reuses authed group → JWT + EnforcePasswordChange + AuditMiddleware + BodyLimit):
//   GET    /api/oauth/me/identities
//   DELETE /api/oauth/me/identities/:id
//
// Admin (reuses adm group → all of the above + RequireRole(admin) + RequireScope(admin)):
//   GET    /api/admin/sso/providers
//   POST   /api/admin/sso/providers
//   GET    /api/admin/sso/providers/:id
//   PUT    /api/admin/sso/providers/:id
//   DELETE /api/admin/sso/providers/:id
//   POST   /api/admin/sso/providers/test
func AttachSSO(r *gin.Engine, authed, adm *gin.RouterGroup, db *gorm.DB, cfg *config.Config, mgr *sso.Manager, cipher *secrets.Cipher) {
	if settingsHandlerHook != nil {
		settingsHandlerHook.SSOManager = mgr
		// N3: SettingsHandler needs the same Cipher to encrypt the
		// captcha provider secret at rest. Inject here on the same
		// boot, before any captcha admin write can land.
		settingsHandlerHook.Cipher = cipher
	}

	pubH := &SSOHandler{DB: db, Cfg: cfg, Manager: mgr, Limits: limitsHook}
	r.GET("/api/oauth/providers", pubH.Providers)
	r.GET("/api/oauth/start/:name", pubH.Start)
	r.GET("/api/oauth/callback/:name", pubH.Callback)
	// Login page + account page both need to know the current login
	// method *without* being authenticated as admin. Read-only; the
	// admin-protected SET endpoint stays where it is in router.go.
	r.GET("/api/auth/login-method", PublicLoginMethod(db))

	// Self-service binding endpoints — any logged-in user can list /
	// unlink their own SSO bindings. Hangs off the shared `authed`
	// group so audit logging, password-change gate, and body limits
	// apply just like every other authenticated endpoint. The "account"
	// scope keeps narrowly-scoped API keys (e.g. instance.read-only)
	// out of account-management endpoints; JWT sessions and unscoped
	// (legacy / full-access) keys pass through unchanged.
	me := authed.Group("/oauth/me", auth.RequireScope("account"))
	me.GET("/identities", pubH.MyIdentities)
	me.DELETE("/identities/:id", pubH.UnlinkMyIdentity)

	adminH := &SSOAdminHandler{DB: db, Mgr: mgr, Cipher: cipher}
	// Admin CRUD piggybacks on the shared `adm` group; no need to
	// rebuild the role/scope/middleware stack here. Importantly this
	// brings AuditMiddleware into effect for every provider mutation
	// — previously add/edit/delete of an OIDC provider went unlogged.
	g := adm.Group("/admin/sso/providers")
	g.GET("", adminH.List)
	g.POST("", adminH.Create)
	g.POST("/test", adminH.Test)
	g.GET("/:id", adminH.Get)
	g.PUT("/:id", adminH.Update)
	g.DELETE("/:id", adminH.Delete)
}

