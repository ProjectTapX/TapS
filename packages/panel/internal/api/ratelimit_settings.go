// Authentication-side rate limiting. This file owns three independent
// per-IP buckets used by login, change-password, and API-key validation.
// The buckets live for the lifetime of the panel process — counts are
// not persisted across restarts (intentional: a restart is itself a
// "clean slate" event for any stuck attacker).
//
// Settings are admin-tunable via the SettingsHandler endpoints below;
// values are persisted in the `settings` table and re-applied on boot.
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/shared/ratelimit"
)

const (
	rlKeyAuthThresh = "auth.rateLimitPerMin"   // panel auth (login + changepw + api-key)
	rlKeyAuthBan    = "auth.banDurationMinutes"

	rlDefaultAuthThresh = 5
	rlDefaultAuthBan    = 5 // minutes

	rlKeyOAuthStartCount  = "auth.rateLimit.oauthStartCount"
	rlKeyOAuthStartWindow = "auth.rateLimit.oauthStartWindowSec"

	rlDefaultOAuthStartCount  = 30
	rlDefaultOAuthStartWindow = 300 // 5 min

	termKeyReadDeadline    = "terminal.wsReadDeadlineSec"
	termKeyInputRatePerSec = "terminal.inputRatePerSec"
	termKeyInputBurst      = "terminal.inputBurst"

	termDefaultReadDeadline    = 60
	termDefaultInputRatePerSec = 200
	termDefaultInputBurst      = 50

	rlKeyIconCacheMaxAge = "icon.cacheMaxAgeSec"
	rlKeyIconRatePerMin  = "icon.ratePerMin"

	rlDefaultIconCacheMaxAge = 300 // 5 min
	rlDefaultIconRatePerMin  = 10
)

// AuthLimiters bundles the three panel-side buckets so handlers and
// the middleware can share the same configuration. All three currently
// share one threshold/ban setting per the agreed design (Q2/Q4: single
// IP-based counter, separate buckets so an attacker who exhausts one
// route doesn't get a "free" attempt on another).
type AuthLimiters struct {
	Login      *ratelimit.Bucket
	ChangePw   *ratelimit.Bucket
	APIKey     *ratelimit.Bucket
	OAuthStart *ratelimit.Bucket
}

// NewAuthLimiters builds the bundle with current settings (or defaults
// if the rows don't exist yet) and returns it.
func NewAuthLimiters(db *gorm.DB) *AuthLimiters {
	thresh, ban := loadAuthRateLimit(db)
	osCount, osWindow := loadOAuthStartRateLimit(db)
	return &AuthLimiters{
		Login:      ratelimit.New("login", thresh, time.Duration(ban)*time.Minute),
		ChangePw:   ratelimit.New("changepw", thresh, time.Duration(ban)*time.Minute),
		APIKey:     ratelimit.New("apikey", thresh, time.Duration(ban)*time.Minute),
		OAuthStart: ratelimit.NewWithWindow("oauth-start", osCount, time.Duration(osWindow)*time.Second, time.Duration(osWindow)*time.Second),
	}
}

// Apply pushes new threshold + ban-duration into the three classic
// auth buckets at once. Called by SetRateLimit after the values are
// validated. The OAuth-start bucket has its own setter (different
// shape: count + window in seconds rather than threshold + ban).
func (a *AuthLimiters) Apply(thresh, banMinutes int) {
	for _, b := range []*ratelimit.Bucket{a.Login, a.ChangePw, a.APIKey} {
		b.SetThreshold(thresh)
		b.SetBan(time.Duration(banMinutes) * time.Minute)
	}
}

// ApplyOAuthStart updates the SSO-start bucket. Window is also the
// ban duration so a flooded IP cools off for the same length of time
// the rolling window covers — simpler mental model than two knobs.
func (a *AuthLimiters) ApplyOAuthStart(count, windowSec int) {
	if a.OAuthStart == nil {
		return
	}
	a.OAuthStart.SetThreshold(count)
	d := time.Duration(windowSec) * time.Second
	a.OAuthStart.SetBan(d)
	a.OAuthStart.SetWindow(d)
}

func loadAuthRateLimit(db *gorm.DB) (thresh, banMinutes int) {
	thresh = rlDefaultAuthThresh
	banMinutes = rlDefaultAuthBan
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(rlKeyAuthThresh); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			thresh = n
		}
	}
	if v := get(rlKeyAuthBan); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1440 {
			banMinutes = n
		}
	}
	return
}

// loadOAuthStartRateLimit mirrors loadAuthRateLimit but for the SSO
// start bucket. Bounds are tighter (count <= 1000, window 30s..1h)
// because the endpoint is anonymous and one source IP rarely needs
// more than a few flows per session.
func loadOAuthStartRateLimit(db *gorm.DB) (count, windowSec int) {
	count = rlDefaultOAuthStartCount
	windowSec = rlDefaultOAuthStartWindow
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(rlKeyOAuthStartCount); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1000 {
			count = n
		}
	}
	if v := get(rlKeyOAuthStartWindow); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 30 && n <= 3600 {
			windowSec = n
		}
	}
	return
}

// ----- HTTP handlers (admin-only) -----

type rateLimitDTO struct {
	RateLimitPerMin    int `json:"rateLimitPerMin"`
	BanDurationMinutes int `json:"banDurationMinutes"`
	OAuthStartCount     int `json:"oauthStartCount"`
	OAuthStartWindowSec int `json:"oauthStartWindowSec"`
	PkceStoreMaxEntries int `json:"pkceStoreMaxEntries"`
	// audit M4 + M5 — terminal WS knobs. Read deadline is the time
	// allowed between any two inbound frames (including pong replies
	// to our 30 s ping); InputRatePerSec / InputBurst form the per-
	// connection token-bucket cap on user keystroke frames.
	TerminalReadDeadlineSec int `json:"terminalReadDeadlineSec"`
	TerminalInputRatePerSec int `json:"terminalInputRatePerSec"`
	TerminalInputBurst      int `json:"terminalInputBurst"`
	// audit-2026-04-26 LOW-7: hib icon public endpoint tunables.
	IconCacheMaxAgeSec int `json:"iconCacheMaxAgeSec"`
	IconRatePerMin     int `json:"iconRatePerMin"`
}

func (h *SettingsHandler) GetRateLimit(c *gin.Context) {
	thresh, ban := loadAuthRateLimit(h.DB)
	osCount, osWindow := loadOAuthStartRateLimit(h.DB)
	pkceMax := loadPkceStoreMax(h.DB)
	tDl, tRate, tBurst := loadTerminalLimits(h.DB)
	iconCache, iconRate := loadIconLimits(h.DB)
	c.JSON(http.StatusOK, rateLimitDTO{
		RateLimitPerMin:         thresh,
		BanDurationMinutes:      ban,
		OAuthStartCount:         osCount,
		OAuthStartWindowSec:     osWindow,
		PkceStoreMaxEntries:     pkceMax,
		TerminalReadDeadlineSec: tDl,
		TerminalInputRatePerSec: tRate,
		TerminalInputBurst:      tBurst,
		IconCacheMaxAgeSec:      iconCache,
		IconRatePerMin:          iconRate,
	})
}

func (h *SettingsHandler) SetRateLimit(c *gin.Context) {
	var b rateLimitDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.RateLimitPerMin < 1 || b.RateLimitPerMin > 100 {
		apiErr(c, http.StatusBadRequest, "settings.rate_limit_range", "rateLimitPerMin must be 1..100")
		return
	}
	if b.BanDurationMinutes < 1 || b.BanDurationMinutes > 1440 {
		apiErr(c, http.StatusBadRequest, "settings.ban_duration_range", "banDurationMinutes must be 1..1440")
		return
	}
	if b.OAuthStartCount < 1 || b.OAuthStartCount > 1000 {
		apiErr(c, http.StatusBadRequest, "settings.oauth_start_count_range", "oauthStartCount must be 1..1000")
		return
	}
	if b.OAuthStartWindowSec < 30 || b.OAuthStartWindowSec > 3600 {
		apiErr(c, http.StatusBadRequest, "settings.oauth_start_window_range", "oauthStartWindowSec must be 30..3600")
		return
	}
	if b.PkceStoreMaxEntries < 100 || b.PkceStoreMaxEntries > 1000000 {
		apiErr(c, http.StatusBadRequest, "settings.pkce_store_max_range", "pkceStoreMaxEntries must be 100..1000000")
		return
	}
	if b.TerminalReadDeadlineSec < 10 || b.TerminalReadDeadlineSec > 600 {
		apiErr(c, http.StatusBadRequest, "settings.terminal_read_deadline_range",
			"terminalReadDeadlineSec must be 10..600")
		return
	}
	if b.TerminalInputRatePerSec < 1 || b.TerminalInputRatePerSec > 5000 {
		apiErr(c, http.StatusBadRequest, "settings.terminal_input_rate_range",
			"terminalInputRatePerSec must be 1..5000")
		return
	}
	if b.TerminalInputBurst < 1 || b.TerminalInputBurst > 5000 {
		apiErr(c, http.StatusBadRequest, "settings.terminal_input_burst_range",
			"terminalInputBurst must be 1..5000")
		return
	}
	if b.IconCacheMaxAgeSec < 0 || b.IconCacheMaxAgeSec > 86400 {
		apiErr(c, http.StatusBadRequest, "settings.icon_cache_range",
			"iconCacheMaxAgeSec must be 0..86400")
		return
	}
	if b.IconRatePerMin < 1 || b.IconRatePerMin > 1000 {
		apiErr(c, http.StatusBadRequest, "settings.icon_rate_range",
			"iconRatePerMin must be 1..1000")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		save := func(k string, v int) error {
			return tx.Save(&model.Setting{Key: k, Value: strconv.Itoa(v)}).Error
		}
		if err := save(rlKeyAuthThresh, b.RateLimitPerMin); err != nil { return err }
		if err := save(rlKeyAuthBan, b.BanDurationMinutes); err != nil { return err }
		if err := save(rlKeyOAuthStartCount, b.OAuthStartCount); err != nil { return err }
		if err := save(rlKeyOAuthStartWindow, b.OAuthStartWindowSec); err != nil { return err }
		if err := save(sysKeyPkceStoreMax, b.PkceStoreMaxEntries); err != nil { return err }
		if err := save(termKeyReadDeadline, b.TerminalReadDeadlineSec); err != nil { return err }
		if err := save(termKeyInputRatePerSec, b.TerminalInputRatePerSec); err != nil { return err }
		if err := save(termKeyInputBurst, b.TerminalInputBurst); err != nil { return err }
		if err := save(rlKeyIconCacheMaxAge, b.IconCacheMaxAgeSec); err != nil { return err }
		return save(rlKeyIconRatePerMin, b.IconRatePerMin)
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	if h.AuthLimiters != nil {
		h.AuthLimiters.Apply(b.RateLimitPerMin, b.BanDurationMinutes)
		h.AuthLimiters.ApplyOAuthStart(b.OAuthStartCount, b.OAuthStartWindowSec)
	}
	if h.SSOManager != nil {
		h.SSOManager.SetPKCEMaxEntries(b.PkceStoreMaxEntries)
	}
	// terminal.* knobs are read per-connection so no hot-reload hook
	// needed — new WS opens after the Save will pick up new values.
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// loadTerminalLimits returns the three terminal WS settings, falling
// back to documented defaults when rows are missing or out of range.
// Exposed for use by terminal.go which reads them per-WS rather than
// caching in memory (so SetRateLimit takes effect on the next open).
func loadTerminalLimits(db *gorm.DB) (deadlineSec, ratePerSec, burst int) {
	deadlineSec = termDefaultReadDeadline
	ratePerSec = termDefaultInputRatePerSec
	burst = termDefaultInputBurst
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(termKeyReadDeadline); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 10 && n <= 600 {
			deadlineSec = n
		}
	}
	if v := get(termKeyInputRatePerSec); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 5000 {
			ratePerSec = n
		}
	}
	if v := get(termKeyInputBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 5000 {
			burst = n
		}
	}
	return
}

const sysKeyPkceStoreMax = "sso.pkceStoreMaxEntries"

// LoadPkceStoreMax exposes the PKCE store cap so cmd/panel can wire
// it into the SSO manager at startup. Same bounds + default as the
// validator above.
func LoadPkceStoreMax(db *gorm.DB) int { return loadPkceStoreMax(db) }

func loadPkceStoreMax(db *gorm.DB) int {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyPkceStoreMax).Error; err == nil {
		if n, err := strconv.Atoi(s.Value); err == nil && n >= 100 && n <= 1000000 {
			return n
		}
	}
	return 10000
}

func loadIconLimits(db *gorm.DB) (cacheMaxAge, ratePerMin int) {
	cacheMaxAge = rlDefaultIconCacheMaxAge
	ratePerMin = rlDefaultIconRatePerMin
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(rlKeyIconCacheMaxAge); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 86400 {
			cacheMaxAge = n
		}
	}
	if v := get(rlKeyIconRatePerMin); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1000 {
			ratePerMin = n
		}
	}
	return
}

// LoadIconCacheMaxAge exposes the icon cache-control setting for
// GetHibIcon which lives in settings.go.
func LoadIconCacheMaxAge(db *gorm.DB) int {
	c, _ := loadIconLimits(db)
	return c
}

// abortRateLimited writes a 429 with Retry-After populated. Used by all
// callers that touch a Bucket so the response shape is consistent.
func abortRateLimited(c *gin.Context, retryAfter time.Duration) {
	secs := int(retryAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	apiErrWithParams(c, http.StatusTooManyRequests,
		"auth.rate_limited",
		"too many requests; please retry later",
		map[string]any{"retryAfter": secs})
}
