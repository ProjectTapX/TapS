package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/alerts"
	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/config"
	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/panel/internal/loglimit"
	"github.com/ProjectTapX/TapS/packages/panel/internal/monitorhist"
	"github.com/ProjectTapX/TapS/packages/panel/internal/scheduler"
)

func NewRouter(cfg *config.Config, db *gorm.DB, reg *daemonclient.Registry, sched *scheduler.Scheduler, hist *monitorhist.Collector, al *alerts.Dispatcher, logCap *loglimit.Manager) (*gin.Engine, *gin.RouterGroup, *gin.RouterGroup) {
	// gin.New + manual middleware so we can swap the default Recovery
	// for one that returns the {error,message} JSON shape (gin.Default's
	// recovery renders text/plain "internal server error", which our
	// formatApiError frontend would just show as opaque text).
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		log.Printf("[panic] %s %s: %v", c.Request.Method, c.Request.URL.Path, recovered)
		apiErr(c, http.StatusInternalServerError, "common.internal", "internal server error")
	}))

	corsCfg := cors.DefaultConfig()
	// B23: replace AllowAllOrigins=true with an explicit allowlist.
	// Token lives in localStorage so cross-site CSRF was never the
	// concern, but a wildcard CORS plus exposing X-Refreshed-Token
	// meant any third-party page that *somehow* obtained a token
	// (XSS, copy-pasted token, browser-extension exfiltration) could
	// keep refreshing it indefinitely from its own origin. Locking
	// the allowlist to known panel origins forces the attacker to
	// front a server-side proxy, which is louder and easier to spot.
	//
	// Dev escape hatch: TAPS_CORS_DEV=1 reopens the wildcard so local
	// `vite dev` (port 5173 → /api proxy at 24444) keeps working
	// without writing every dev URL into the panel settings table.
	if os.Getenv("TAPS_CORS_DEV") == "1" {
		corsCfg.AllowAllOrigins = true
	} else {
		corsCfg.AllowOriginFunc = func(origin string) bool {
			// Empty Origin = same-origin / non-browser request — pass.
			// (Browser SOP only sets Origin on cross-origin requests.)
			if origin == "" {
				return true
			}
			// Mirror parseOriginsCSV's normalisation on the way in so
			// "HTTPS://Example.com" (an exotic but RFC-legal Origin)
			// matches a stored "https://example.com" entry.
			needle := strings.ToLower(strings.TrimRight(origin, "/"))
			for _, o := range loadAllowedOrigins(db) {
				if o == needle {
					return true
				}
			}
			return false
		}
	}
	corsCfg.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
	corsCfg.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	// Expose response headers cross-origin SPAs need to read:
	//   X-Refreshed-Token  — sliding-renewal payload from auth middleware
	//   Retry-After        — rate-limit hint on 429 responses
	//   Content-Disposition — filename for browser downloads
	corsCfg.ExposeHeaders = []string{"X-Refreshed-Token", "Retry-After", "Content-Disposition"}
	r.Use(cors.New(corsCfg))

	// Per-IP failure throttles for login / change-pw / api-key. Built
	// once and shared between the AuthHandler (login + change-pw),
	// the auth middleware (api-key), and the SettingsHandler (so the
	// admin can retune the threshold without restarting).
	limits := NewAuthLimiters(db)
	recordLimits(limits)
	// Live, admin-tunable byte caps for buffered JSON bodies and WS
	// frames. Same hot-reload pattern: handlers read the current value
	// per request, settings page calls Apply() to push new values.
	sizeLimits := NewLiveLimits(db)

	// audit-2026-04-25 VULN-001: CSP domain allowlists + security
	// headers middleware. LiveCSP is hot-reloaded on every settings
	// save; the middleware reads it per-request.
	liveCSP := NewLiveCSP(db)
	// Security headers on every response.
	r.Use(SecurityHeadersMiddleware(cfg, liveCSP))

	authH := &AuthHandler{DB: db, Cfg: cfg, Limits: limits}
	userH := &UserHandler{DB: db}
	daemonH := &DaemonHandler{DB: db, Reg: reg}
	instH := &InstanceHandler{DB: db, Reg: reg}
	termH := &TerminalHandler{Cfg: cfg, Reg: reg, DB: db, Limits: sizeLimits}
	fsH := &FsHandler{Reg: reg, DB: db, Limits: sizeLimits}
	filesH := &FilesProxyHandler{Reg: reg, Fs: fsH}
	monH := &MonitorHandler{Reg: reg}
	taskH := &TaskHandler{DB: db, Sched: sched}
	keyH := &APIKeyHandler{DB: db}
	permH := &PermissionHandler{DB: db}
	dockerH := &DockerHandler{Reg: reg, DB: db}
	mcH := &McHandler{DB: db, Reg: reg}
	deployH := &DeployHandler{Reg: reg}
	bkH := &BackupHandler{DB: db, Reg: reg}
	histH := &MonitorHistoryHandler{Reg: reg, Hist: hist}
	procH := &ProcessHandler{DB: db, Reg: reg}
	auditH := &AuditHandler{DB: db}
	settingsH := &SettingsHandler{Alerts: al, DB: db, Reg: reg, LogLimit: logCap, AuthLimiters: limits, Limits: sizeLimits, CSP: liveCSP}
	recordSettingsHandler(settingsH)
	settingsH.SyncDeploySourceFromDB()
	authH.Settings = settingsH
	// Push hib config to each daemon when (re)connecting — daemon doesn't
	// persist it, so without this a daemon restart leaves it disabled
	// until the user next saves settings.
	reg.AddConnectHook(func(c *daemonclient.Client) { settingsH.PushTo(c) })
	volumeH := &VolumeHandler{Reg: reg}
	freePortH := &FreePortHandler{DB: db, Reg: reg}
	groupH := &GroupHandler{DB: db, Reg: reg}
	srvDeployH := &ServerDeployHandler{DB: db, Reg: reg}

	api := r.Group("/api")
	// Cap public endpoints' body too — login + captcha-config etc.
	// don't need more than 4 KiB but we use the same setting; an
	// attacker can't OOM the panel via login even before captcha.
	api.Use(BodyLimitMiddleware(sizeLimits))
	api.POST("/auth/login", authH.Login)
	// Public so the login page can render the right widget without
	// needing a token. Only ships provider + siteKey (no secrets).
	api.GET("/captcha/config", settingsH.PublicCaptchaConfig)
	// Brand: site name + favicon. Public so the login page can show
	// custom title/icon before any auth.
	api.GET("/brand", settingsH.PublicBrand)
	api.GET("/brand/favicon", settingsH.PublicBrandFavicon)

	// terminal WS auth via query param (browsers can't set headers on ws)
	api.GET("/ws/instance/:id/:uuid/terminal", termH.Handle)

	// file download/upload also auth via query token because browsers can't set
	// headers on <a href> downloads or some upload helpers; we still validate JWT.
	api.GET("/daemons/:id/files/download", queryAuth(cfg.JWTSecret, db), filesH.Download)
	api.POST("/daemons/:id/files/upload", queryAuth(cfg.JWTSecret, db), filesH.Upload)
	api.POST("/daemons/:id/files/upload/init", queryAuth(cfg.JWTSecret, db), filesH.UploadInit)
	api.GET("/daemons/:id/instances/:uuid/backups/download", queryAuth(cfg.JWTSecret, db), bkH.Download)
	// Hibernation icon preview: <img src> can't send Authorization
	// headers, so this lives outside the JWT-header middleware. The asset
	// itself is just a 64×64 PNG; we don't require auth at all.
	api.GET("/settings/hibernation/icon", settingsH.GetHibIcon)

	authed := api.Group("")
	authed.Use(auth.Middleware(cfg.JWTSecret, db, limits.APIKey, func() time.Duration {
		ttl, _ := LoadAuthTimings(db)
		return ttl
	}))
	authed.Use(EnforcePasswordChange(db))
	authed.Use(AuditMiddleware(db))
	// adm is a sub-group of authed gated by role=admin + scope=admin.
	// Hoisted out of the route-registration block below so SSO routes
	// (registered separately via AttachSSO from main.go) can reuse the
	// same middleware chain — audit logging, password-change gate, and
	// body-size limit — without duplicating it.
	adm := authed.Group("", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
	{
		authed.GET("/auth/me", authH.Me)
		authed.POST("/auth/me/password", authH.ChangePassword)

		users := authed.Group("/users", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
		users.GET("", userH.List)
		users.POST("", userH.Create)
		users.PUT("/:id", userH.Update)
		users.DELETE("/:id", userH.Delete)

		daemons := authed.Group("/daemons", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
		daemons.GET("", daemonH.List)
		daemons.POST("", daemonH.Create)
		daemons.PUT("/:id", daemonH.Update)
		daemons.DELETE("/:id", daemonH.Delete)
		// TLS fingerprint helpers used by the add/edit-daemon UI.
		// Probe takes a brand-new address (no daemon row yet);
		// refetch reads the address off the existing row and returns
		// the currently-served fingerprint so the operator can
		// re-accept after a cert rotation.
		daemons.POST("/probe-fingerprint", daemonH.ProbeFingerprint)
		daemons.POST("/:id/refetch-fingerprint", daemonH.RefetchFingerprint)

		// aggregate across all daemons (filtered by access)
		authed.GET("/instances", auth.RequireScope("instance.read"), instH.AggregateList)
		// publicly-readable daemon metadata for non-admins (display host etc)
		authed.GET("/daemons/:id/public", auth.RequireScope("instance.read"), daemonH.PublicView)

		// per-daemon instance ops — read scope for GET, control scope for mutations.
		di := authed.Group("/daemons/:id/instances")
		di.GET("", auth.RequireScope("instance.read"), instH.List)
		di.POST("", auth.RequireScope("instance.control"), instH.Create)
		di.PUT("/:uuid", auth.RequireScope("instance.control"), instH.Update)
		di.DELETE("/:uuid", auth.RequireScope("instance.control"), instH.Delete)
		di.POST("/:uuid/start", auth.RequireScope("instance.control"), instH.Start)
		di.POST("/:uuid/stop", auth.RequireScope("instance.control"), instH.Stop)
		di.POST("/:uuid/kill", auth.RequireScope("instance.control"), instH.Kill)
		di.POST("/:uuid/input", auth.RequireScope("instance.control"), instH.Input)
		di.GET("/:uuid/dockerstats", auth.RequireScope("instance.read"), instH.DockerStats)
		authed.GET("/daemons/:id/instances-dockerstats", auth.RequireScope("instance.read"), instH.DockerStatsAll)
		authed.GET("/daemons/:id/instances-players", auth.RequireScope("instance.read"), instH.PlayersAll)

		// helper: pick an unused host port on this daemon (admin only)
		authed.GET("/daemons/:id/free-port", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"), freePortH.Get)

		// node groups (admin only). Group resolve also needs admin since
		// it can scan every member's load and instance list.
		groups := authed.Group("/groups", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
		groups.GET("", groupH.List)
		groups.POST("", groupH.Create)
		groups.PUT("/:id", groupH.Update)
		groups.DELETE("/:id", groupH.Delete)
		groups.POST("/:id/resolve", groupH.Resolve)
		groups.POST("/:id/instances", groupH.CreateInstance)

		// tasks per instance
		dt := authed.Group("/daemons/:id/instances/:uuid/tasks", auth.RequireScope("tasks"))
		dt.GET("", taskH.List)
		dt.POST("", taskH.Create)
		dt.PUT("/:taskId", taskH.Update)
		dt.DELETE("/:taskId", taskH.Delete)

		// fs ops (path-scoped per user; admin sees the whole tree, non-admins
		// limited to /data/inst-<short> for instances they hold PermFiles on).
		df := authed.Group("/daemons/:id/fs", auth.RequireScope("files"))
		df.GET("/list", fsH.List)
		df.GET("/read", fsH.Read)
		df.POST("/write", fsH.Write)
		df.POST("/mkdir", fsH.Mkdir)
		df.DELETE("/delete", fsH.Delete)
		df.POST("/rename", fsH.Rename)
		df.POST("/copy", fsH.Copy)
		df.POST("/move", fsH.Move)
		df.POST("/zip", fsH.Zip)
		df.POST("/unzip", fsH.Unzip)

		// Daemon-level host metrics expose CPU / memory / disk / network of
		// the underlying machine. Restricted to admins so non-admin users
		// can't enumerate infrastructure they shouldn't see. Per-instance
		// process/player views below remain access-checked at handler level.
		authed.GET("/daemons/:id/monitor", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"), monH.Snapshot)
		authed.GET("/daemons/:id/monitor/history", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"), histH.History)
		authed.GET("/daemons/:id/instances/:uuid/process", auth.RequireScope("instance.read"), procH.Snapshot)

		// backups (per-instance, access-checked)
		bk := authed.Group("/daemons/:id/instances/:uuid/backups", auth.RequireScope("files"))
		bk.GET("", bkH.List)
		bk.POST("", bkH.Create)
		bk.POST("/restore", bkH.Restore)
		bk.DELETE("", bkH.Delete)

		// API keys (per-user; admin can see all). Manage own keys
		// always allowed regardless of scope (else a scoped key
		// could never even rotate itself), so no scope guard here.
		keys := authed.Group("/apikeys")
		keys.GET("", keyH.List)
		keys.POST("", keyH.Create)
		keys.POST("/revoke-all", keyH.RevokeAll)
		keys.POST("/:id/revoke", keyH.Revoke)
		keys.DELETE("/:id", keyH.Delete)

		// permissions (admin only)
		perms := authed.Group("/permissions", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
		perms.GET("", permH.List)
		perms.POST("", permH.Grant)
		perms.DELETE("", permH.Revoke)

		// docker images: read-only listing is open to any authenticated user
		// so they can pick a runtime in the instance edit form. Pull / remove
		// remain admin-only.
		authed.GET("/daemons/:id/docker/images", auth.RequireScope("instance.read"), dockerH.Images)
		dock := authed.Group("/daemons/:id/docker", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
		dock.POST("/pull", dockerH.Pull)
		dock.DELETE("/remove", dockerH.Remove)
		dock.PUT("/images/:ref/alias", dockerH.SetAlias)

		// managed loopback volumes (admin only)
		vol := authed.Group("/daemons/:id/volumes", auth.RequireRole(model.RoleAdmin), auth.RequireScope("admin"))
		vol.GET("", volumeH.List)
		vol.POST("", volumeH.Create)
		vol.DELETE("", volumeH.Remove)

		// minecraft (per-instance, access-checked)
		authed.GET("/daemons/:id/instances/:uuid/players", auth.RequireScope("instance.read"), mcH.Players)

		// quick deploy templates
		authed.GET("/templates", auth.RequireScope("instance.read"), deployH.Templates)
		authed.GET("/templates/paper/versions", auth.RequireScope("instance.read"), deployH.PaperVersions)
		authed.POST("/daemons/:id/templates/deploy", auth.RequireScope("instance.control"), deployH.Deploy)

		// server deploy (Vanilla / Paper / Purpur / Fabric / Forge / NeoForge)
		// — provider catalog is open to any authed user; per-instance
		// start/status calls are gated inside the handler by permission.
		authed.GET("/serverdeploy/types", auth.RequireScope("instance.read"), srvDeployH.Types)
		authed.GET("/serverdeploy/versions", auth.RequireScope("instance.read"), srvDeployH.Versions)
		authed.GET("/serverdeploy/builds", auth.RequireScope("instance.read"), srvDeployH.Builds)
		authed.POST("/daemons/:id/instances/:uuid/serverdeploy", auth.RequireScope("instance.control"), srvDeployH.Start)
		authed.GET("/daemons/:id/instances/:uuid/serverdeploy/status", auth.RequireScope("instance.read"), srvDeployH.Status)

		// audit log + settings (admin only)
		adm.GET("/audit", auditH.List)
		adm.GET("/logins", auditH.ListLogins)
		adm.GET("/settings/webhook", settingsH.GetWebhook)
		adm.PUT("/settings/webhook", settingsH.SetWebhook)
		adm.POST("/settings/webhook/test", settingsH.TestWebhook)
		adm.GET("/settings/deploy-source", settingsH.GetDeploySource)
		adm.PUT("/settings/deploy-source", settingsH.SetDeploySource)
		adm.GET("/settings/captcha", settingsH.GetCaptchaConfig)
		adm.PUT("/settings/captcha", settingsH.SetCaptchaConfig)
		adm.POST("/settings/captcha/test", settingsH.TestCaptchaConfig)
		adm.PUT("/settings/brand/site-name", settingsH.SetBrandSiteName)
		adm.POST("/settings/brand/favicon", settingsH.SetBrandFavicon)
		adm.DELETE("/settings/brand/favicon", settingsH.DeleteBrandFavicon)
		adm.GET("/settings/log-limits", settingsH.GetLogLimits)
		adm.PUT("/settings/log-limits", settingsH.SetLogLimits)
		adm.GET("/settings/rate-limit", settingsH.GetRateLimit)
		adm.PUT("/settings/rate-limit", settingsH.SetRateLimit)
		adm.GET("/settings/limits", settingsH.GetLimits)
		adm.PUT("/settings/limits", settingsH.SetLimits)
		adm.GET("/settings/auth-timings", settingsH.GetAuthTimings)
		adm.PUT("/settings/auth-timings", settingsH.SetAuthTimings)
		adm.GET("/settings/panel-port", settingsH.GetPanelPort)
		adm.PUT("/settings/panel-port", settingsH.SetPanelPort)
		adm.GET("/settings/http-timeouts", settingsH.GetHTTPTimeouts)
		adm.PUT("/settings/http-timeouts", settingsH.SetHTTPTimeouts)
		adm.GET("/settings/panel-public-url", settingsH.GetPanelPublicURL)
		adm.PUT("/settings/panel-public-url", settingsH.SetPanelPublicURL)
		adm.GET("/settings/login-method", settingsH.GetLoginMethod)
		adm.PUT("/settings/login-method", settingsH.SetLoginMethod)
		adm.GET("/settings/trusted-proxies", settingsH.GetTrustedProxies)
		adm.PUT("/settings/trusted-proxies", settingsH.SetTrustedProxies)
		adm.GET("/settings/cors-origins", settingsH.GetAllowedOrigins)
		adm.PUT("/settings/cors-origins", settingsH.SetAllowedOrigins)
		adm.GET("/settings/csp", settingsH.GetCSP)
		adm.PUT("/settings/csp", settingsH.SetCSP)
		adm.GET("/settings/hibernation", settingsH.GetHib)
		adm.PUT("/settings/hibernation", settingsH.SetHib)
		adm.POST("/settings/hibernation/icon", settingsH.SetHibIcon)
		adm.DELETE("/settings/hibernation/icon", settingsH.DeleteHibIcon)
	}

	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// 405 vs 404 split for /api/* (audit N7). gin's default behavior
	// when a path matches a registered route but the method doesn't is
	// to fall through to NoRoute, which then masks the difference as a
	// 404 and (worse) lets the SPA fallback below serve text/html for
	// what is clearly an API client. With HandleMethodNotAllowed=true
	// gin instead routes to NoMethod, where we emit the proper 405 JSON.
	r.HandleMethodNotAllowed = true
	r.NoMethod(func(c *gin.Context) {
		apiErr(c, http.StatusMethodNotAllowed, "common.method_not_allowed", "method not allowed")
	})

	// Serve the SPA from cfg.WebDir if it exists. /api/* takes priority above.
	if st, err := os.Stat(cfg.WebDir); err == nil && st.IsDir() {
		fileServer := http.FileServer(http.Dir(cfg.WebDir))
		r.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/api/") {
				apiErr(c, http.StatusNotFound, "common.no_such_api", "no such api endpoint")
				return
			}
			// try the requested file; if missing, fall back to index.html (SPA routing)
			full := filepath.Join(cfg.WebDir, filepath.FromSlash(path))
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}
	return r, authed, adm
}
