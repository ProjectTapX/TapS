// CORS allowed-origin setting (B23). Stored as a single CSV row in the
// `settings` table so admins can edit it from the UI without rebuild.
// Empty / missing setting falls back to the panel's PublicURL so a
// fresh deployment works out of the box (the panel's own SPA reaching
// /api on the same origin is always allowed via the empty-Origin
// shortcut in router.go).
package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
)

const sysKeyAllowedOrigins = "cors.allowedOrigins"

// loadAllowedOrigins returns the parsed list (deduplicated, trimmed)
// or — when no value was set — a single-entry list containing the
// panel's PublicURL origin. That guarantees the panel's own SPA always
// passes CORS even on a fresh DB where admin hasn't touched settings.
func loadAllowedOrigins(db *gorm.DB) []string {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyAllowedOrigins).Error; err == nil && strings.TrimSpace(s.Value) != "" {
		return parseOriginsCSV(s.Value)
	}
	pub := strings.TrimSpace(LoadPanelPublicURL(db))
	if pub == "" {
		return nil
	}
	if u, err := url.Parse(pub); err == nil && u.Scheme != "" && u.Host != "" {
		return []string{u.Scheme + "://" + u.Host}
	}
	return nil
}

func parseOriginsCSV(csv string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range strings.Split(csv, ",") {
		// N1: strip trailing slash so admins who paste
		// "https://example.com/" don't end up with a saved value that
		// never matches the slash-less Origin header browsers actually
		// send. Real browsers normalise scheme+host to lowercase before
		// emitting Origin (RFC 6454), so do the same here on save.
		v := strings.ToLower(strings.TrimRight(strings.TrimSpace(p), "/"))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// validateOriginsCSV rejects entries that aren't well-formed origins
// (scheme://host[:port], no path/query/fragment). Empty CSV is fine —
// it means "fall back to PublicURL".
func validateOriginsCSV(csv string) error {
	for _, o := range parseOriginsCSV(csv) {
		u, err := url.Parse(o)
		if err != nil {
			return strErr("invalid origin: " + o)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return strErr("origin must be http(s)://...: " + o)
		}
		if u.Host == "" {
			return strErr("origin missing host: " + o)
		}
		if u.Path != "" && u.Path != "/" {
			return strErr("origin must not contain a path: " + o)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return strErr("origin must not contain query/fragment: " + o)
		}
	}
	return nil
}

type allowedOriginsDTO struct {
	Origins string `json:"origins"`
}

func (h *SettingsHandler) GetAllowedOrigins(c *gin.Context) {
	var s model.Setting
	val := ""
	if err := h.DB.First(&s, "key = ?", sysKeyAllowedOrigins).Error; err == nil {
		val = s.Value
	}
	c.JSON(http.StatusOK, allowedOriginsDTO{Origins: val})
}

func (h *SettingsHandler) SetAllowedOrigins(c *gin.Context) {
	var b allowedOriginsDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	v := strings.TrimSpace(b.Origins)
	if err := validateOriginsCSV(v); err != nil {
		apiErr(c, http.StatusBadRequest, "settings.cors_origin_invalid", err.Error())
		return
	}
	// Re-emit through parseOriginsCSV so the persisted value is the
	// normalised form (lowercased, trailing-slash stripped) — keeps
	// admin GETs honest and lets the AllowOriginFunc do byte-equality
	// against browser-emitted Origin headers without surprises.
	v = strings.Join(parseOriginsCSV(v), ",")
	if err := h.DB.Save(&model.Setting{Key: sysKeyAllowedOrigins, Value: v}).Error; err != nil {
		apiErrFromDB(c, err)
		return
	}
	// Note: existing CORS middleware reads loadAllowedOrigins per
	// request via the closure in router.go, so the change takes
	// effect immediately without a panel restart.
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
