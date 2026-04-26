// Public URL of this panel installation (e.g. "https://taps.example.com").
// Required for OIDC: the IdP's redirect_uri must point to a host the
// browser can reach, and the panel can't reliably guess that from
// gin's request headers (X-Forwarded-Host can lie). Admin sets this
// once after install.
package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
)

const sysKeyPanelPublicURL = "system.publicUrl"

// LoadPanelPublicURL returns the configured public URL, or "" if not
// set. Trailing slash stripped. Validators ensure scheme+host are
// present at write time so callers can rely on the format.
func LoadPanelPublicURL(db *gorm.DB) string {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyPanelPublicURL).Error; err != nil {
		return ""
	}
	return strings.TrimRight(s.Value, "/")
}

type panelPublicURLDTO struct {
	URL string `json:"url"`
}

func (h *SettingsHandler) GetPanelPublicURL(c *gin.Context) {
	c.JSON(http.StatusOK, panelPublicURLDTO{URL: LoadPanelPublicURL(h.DB)})
}

func (h *SettingsHandler) SetPanelPublicURL(c *gin.Context) {
	var b panelPublicURLDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	v := strings.TrimSpace(b.URL)
	if v == "" {
		// Clearing is allowed — but admin who clears can't use SSO.
		h.DB.Save(&model.Setting{Key: sysKeyPanelPublicURL, Value: ""})
		if h.SSOManager != nil {
			h.SSOManager.SetPublicURL("")
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		apiErr(c, http.StatusBadRequest, "settings.public_url_format", "publicUrl must be http(s)://host[:port]")
		return
	}
	clean := strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
	h.DB.Save(&model.Setting{Key: sysKeyPanelPublicURL, Value: clean})
	if h.SSOManager != nil {
		h.SSOManager.SetPublicURL(clean)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "url": clean})
}
