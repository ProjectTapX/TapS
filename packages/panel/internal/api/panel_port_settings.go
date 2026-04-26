// Panel listen-port setting. The current process can't rebind, so
// changes here only take effect on the next start; the UI shows a
// "restart required" hint after save. DB value (when in 1024..65535)
// wins over the TAPS_ADDR env var, which wins over the built-in
// default (24444).
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
)

const (
	sysKeyPanelPort = "system.panelPort"

	panelPortMin = 1024
	panelPortMax = 65535
)

// LoadPanelPort returns the persisted port (1024..65535) when present
// and valid, else 0 — caller falls back to env / default.
// Lives here so cmd/panel/main.go can call it without importing
// model directly.
func LoadPanelPort(db *gorm.DB) int {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyPanelPort).Error; err != nil {
		return 0
	}
	n, err := strconv.Atoi(s.Value)
	if err != nil || n < panelPortMin || n > panelPortMax {
		return 0
	}
	return n
}

type panelPortDTO struct {
	Port int `json:"port"`
}

func (h *SettingsHandler) GetPanelPort(c *gin.Context) {
	c.JSON(http.StatusOK, panelPortDTO{Port: LoadPanelPort(h.DB)})
}

func (h *SettingsHandler) SetPanelPort(c *gin.Context) {
	var b panelPortDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.Port < panelPortMin || b.Port > panelPortMax {
		apiErr(c, http.StatusBadRequest, "settings.port_range", "port must be 1024..65535")
		return
	}
	if err := h.DB.Save(&model.Setting{Key: sysKeyPanelPort, Value: strconv.Itoa(b.Port)}).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "restartRequired": true})
}
