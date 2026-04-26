// Trusted-proxy list for X-Real-IP / X-Forwarded-For consumption.
// When the panel sits behind a reverse proxy (nginx) those headers
// are the only way to recover the real client IP — and Gin only
// honors them when the immediate hop is in this list. Changes here
// take effect on the next start (engine.trustedProxies isn't
// designed to be hot-reloaded).
package api

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

const sysKeyTrustedProxies = "system.trustedProxies"

// loopbackProxies is what we hand to gin when the admin hasn't
// configured anything. Mirrors gin's own default but documented
// here so the wiring is explicit.
var loopbackProxies = []string{"127.0.0.1", "::1"}

// LoadTrustedProxies returns the CSV-parsed list, falling back to
// loopback when the row is missing or empty. Each entry must be a
// plain IP (v4 or v6) or a CIDR — anything else is dropped with a
// warning to the caller (silently here; SetTrustedProxies validates
// at write time so the DB only ever holds clean values).
func LoadTrustedProxies(db *gorm.DB) []string {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyTrustedProxies).Error; err != nil {
		return loopbackProxies
	}
	out := parseProxyList(s.Value)
	if len(out) == 0 {
		return loopbackProxies
	}
	return out
}

func parseProxyList(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if net.ParseIP(p) != nil {
			out = append(out, p)
			continue
		}
		if _, _, err := net.ParseCIDR(p); err == nil {
			out = append(out, p)
			continue
		}
	}
	return out
}

type trustedProxiesDTO struct {
	Proxies string `json:"proxies"` // CSV: "127.0.0.1,::1,10.0.0.0/8"
}

func (h *SettingsHandler) GetTrustedProxies(c *gin.Context) {
	var s model.Setting
	val := strings.Join(loopbackProxies, ",")
	if err := h.DB.First(&s, "key = ?", sysKeyTrustedProxies).Error; err == nil && s.Value != "" {
		val = s.Value
	}
	c.JSON(http.StatusOK, trustedProxiesDTO{Proxies: val})
}

func (h *SettingsHandler) SetTrustedProxies(c *gin.Context) {
	var b trustedProxiesDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	// Validate each entry; reject the whole payload on the first bad
	// one so the admin sees the typo instead of having entries
	// silently dropped.
	parts := strings.Split(b.Proxies, ",")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				apiErr(c, http.StatusBadRequest, "settings.invalid_entry", "invalid entry: " + p + " (must be IP or CIDR)")
				return
			}
		}
		clean = append(clean, p)
	}
	value := strings.Join(clean, ",")
	if err := h.DB.Save(&model.Setting{Key: sysKeyTrustedProxies, Value: value}).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "restartRequired": true})
}
