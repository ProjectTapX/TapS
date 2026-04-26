package api

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/config"
	"github.com/taps/panel/internal/model"
)

const (
	cspKeyScriptSrc = "csp.scriptSrcExtra"
	cspKeyFrameSrc  = "csp.frameSrcExtra"

	cspDefaultScriptSrc = "https://challenges.cloudflare.com,https://www.recaptcha.net"
	cspDefaultFrameSrc  = "https://challenges.cloudflare.com,https://www.google.com,https://www.recaptcha.net"
)

// LiveCSP holds the admin-tunable CSP domain allowlists. Reads are
// lock-free (atomic.Value); writes happen on the rare settings-save
// path. The middleware reads these on every request so the admin's
// changes take effect immediately without a panel restart.
type LiveCSP struct {
	scriptSrc atomic.Value // string
	frameSrc  atomic.Value // string
}

func NewLiveCSP(db *gorm.DB) *LiveCSP {
	l := &LiveCSP{}
	s, f := loadCSP(db)
	l.scriptSrc.Store(s)
	l.frameSrc.Store(f)
	return l
}

func (l *LiveCSP) ScriptSrc() string { return l.scriptSrc.Load().(string) }
func (l *LiveCSP) FrameSrc() string  { return l.frameSrc.Load().(string) }

func (l *LiveCSP) Apply(scriptSrc, frameSrc string) {
	l.scriptSrc.Store(scriptSrc)
	l.frameSrc.Store(frameSrc)
}

func loadCSP(db *gorm.DB) (scriptSrc, frameSrc string) {
	scriptSrc, frameSrc = cspDefaultScriptSrc, cspDefaultFrameSrc
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil && s.Value != "" {
			return s.Value
		}
		return ""
	}
	if v := get(cspKeyScriptSrc); v != "" {
		scriptSrc = v
	}
	if v := get(cspKeyFrameSrc); v != "" {
		frameSrc = v
	}
	return
}

// buildCSP assembles the full Content-Security-Policy header value.
// 'self' is always present and not admin-editable; the extra domains
// come from the admin-tuned allowlists.
func buildCSP(scriptExtra, frameExtra string) string {
	scriptDomains := "'self'"
	for _, d := range strings.Split(scriptExtra, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			scriptDomains += " " + d
		}
	}
	frameDomains := "'self'"
	for _, d := range strings.Split(frameExtra, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			frameDomains += " " + d
		}
	}
	return "default-src 'self'; " +
		"script-src " + scriptDomains + "; " +
		"style-src 'self' 'unsafe-inline'; " +
		"frame-src " + frameDomains + "; " +
		"img-src 'self' data:; " +
		"connect-src 'self' ws: wss:; " +
		"font-src 'self'"
}

// SecurityHeadersMiddleware sets all OWASP-recommended response
// headers on every response (audit-2026-04-25 VULN-001).
func SecurityHeadersMiddleware(cfg *config.Config, csp *LiveCSP) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		if csp != nil {
			c.Header("Content-Security-Policy", buildCSP(csp.ScriptSrc(), csp.FrameSrc()))
		}

		// HSTS: only when TLS is in play — panel's own cert, nginx
		// X-Forwarded-Proto, or the request itself arrived over TLS.
		// Without this guard, a pure-HTTP direct-connect deployment
		// would have browsers remember "always use HTTPS" and lock
		// themselves out.
		if cfg.TLSCert != "" ||
			c.GetHeader("X-Forwarded-Proto") == "https" ||
			c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}

// ----- admin endpoints -----

type cspDTO struct {
	ScriptSrcExtra string `json:"scriptSrcExtra"`
	FrameSrcExtra  string `json:"frameSrcExtra"`
}

func (h *SettingsHandler) GetCSP(c *gin.Context) {
	s, f := loadCSP(h.DB)
	c.JSON(http.StatusOK, cspDTO{ScriptSrcExtra: s, FrameSrcExtra: f})
}

func (h *SettingsHandler) SetCSP(c *gin.Context) {
	var b cspDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	scriptSrc := strings.TrimSpace(b.ScriptSrcExtra)
	frameSrc := strings.TrimSpace(b.FrameSrcExtra)
	h.DB.Save(&model.Setting{Key: cspKeyScriptSrc, Value: scriptSrc})
	h.DB.Save(&model.Setting{Key: cspKeyFrameSrc, Value: frameSrc})
	if h.CSP != nil {
		h.CSP.Apply(scriptSrc, frameSrc)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
