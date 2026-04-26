// Auth-token related settings: JWT TTL and the WebSocket heartbeat
// interval used for live-revocation. Both are admin-tunable from the
// settings page; current values live in DB and are read on each token
// issue / WS connect, so changes take effect for new sessions
// immediately.
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

const (
	tokKeyJWTTTLMin     = "auth.jwtTtlMinutes"
	tokKeyWSHeartbeatMin = "auth.wsHeartbeatMinutes"

	tokDefaultJWTTTLMin     = 60
	tokMinJWTTTLMin         = 5
	tokMaxJWTTTLMin         = 1440

	tokDefaultWSHeartbeatMin = 5
	tokMinWSHeartbeatMin     = 1
	tokMaxWSHeartbeatMin     = 60
)

// LoadAuthTimings reads the current TTL + heartbeat values from the
// settings table, falling back to defaults if rows are missing or out
// of range. Cheap enough to call per-request — single PK lookup.
func LoadAuthTimings(db *gorm.DB) (jwtTTL, wsHeartbeat time.Duration) {
	jwtTTL = time.Duration(tokDefaultJWTTTLMin) * time.Minute
	wsHeartbeat = time.Duration(tokDefaultWSHeartbeatMin) * time.Minute
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(tokKeyJWTTTLMin); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= tokMinJWTTTLMin && n <= tokMaxJWTTTLMin {
			jwtTTL = time.Duration(n) * time.Minute
		}
	}
	if v := get(tokKeyWSHeartbeatMin); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= tokMinWSHeartbeatMin && n <= tokMaxWSHeartbeatMin {
			wsHeartbeat = time.Duration(n) * time.Minute
		}
	}
	return
}

type authTimingsDTO struct {
	JWTTTLMinutes       int `json:"jwtTtlMinutes"`
	WSHeartbeatMinutes  int `json:"wsHeartbeatMinutes"`
}

func (h *SettingsHandler) GetAuthTimings(c *gin.Context) {
	jwtTTL, wsHb := LoadAuthTimings(h.DB)
	c.JSON(http.StatusOK, authTimingsDTO{
		JWTTTLMinutes:      int(jwtTTL / time.Minute),
		WSHeartbeatMinutes: int(wsHb / time.Minute),
	})
}

func (h *SettingsHandler) SetAuthTimings(c *gin.Context) {
	var b authTimingsDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.JWTTTLMinutes < tokMinJWTTTLMin || b.JWTTTLMinutes > tokMaxJWTTTLMin {
		apiErr(c, http.StatusBadRequest, "settings.jwt_ttl_range", "jwtTtlMinutes must be 5..1440")
		return
	}
	if b.WSHeartbeatMinutes < tokMinWSHeartbeatMin || b.WSHeartbeatMinutes > tokMaxWSHeartbeatMin {
		apiErr(c, http.StatusBadRequest, "settings.ws_heartbeat_range", "wsHeartbeatMinutes must be 1..60")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&model.Setting{Key: tokKeyJWTTTLMin, Value: strconv.Itoa(b.JWTTTLMinutes)}).Error; err != nil {
			return err
		}
		return tx.Save(&model.Setting{Key: tokKeyWSHeartbeatMin, Value: strconv.Itoa(b.WSHeartbeatMinutes)}).Error
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
