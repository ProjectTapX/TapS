// Live, admin-tunable HTTP server slow-loris defences (audit-2026-04-25
// MED5). Stored in the settings table and applied at panel startup.
// Changes only take effect after a panel restart because http.Server's
// timeout fields are read by the listener loop and can't be hot-swapped
// without replacing the listener — the UI shows a "restart required"
// hint, mirroring panelPort.
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

const (
	httpKeyReadHeaderTimeout = "http.readHeaderTimeoutSec"
	httpKeyReadTimeout       = "http.readTimeoutSec"
	httpKeyWriteTimeout      = "http.writeTimeoutSec"
	httpKeyIdleTimeout       = "http.idleTimeoutSec"

	httpReadHeaderTimeoutDefault = 10
	httpReadTimeoutDefault       = 60
	httpWriteTimeoutDefault      = 120
	httpIdleTimeoutDefault       = 120

	httpTimeoutMin = 1
	httpTimeoutMax = 3600
)

type httpTimeoutsDTO struct {
	ReadHeaderTimeoutSec int `json:"readHeaderTimeoutSec"`
	ReadTimeoutSec       int `json:"readTimeoutSec"`
	WriteTimeoutSec      int `json:"writeTimeoutSec"`
	IdleTimeoutSec       int `json:"idleTimeoutSec"`
}

// LoadHTTPTimeouts is called from main() to size the http.Server
// fields before ListenAndServe. Out-of-range stored values silently
// fall back to defaults so a corrupt row can't keep panel from
// starting.
func LoadHTTPTimeouts(db *gorm.DB) httpTimeoutsDTO {
	out := httpTimeoutsDTO{
		ReadHeaderTimeoutSec: httpReadHeaderTimeoutDefault,
		ReadTimeoutSec:       httpReadTimeoutDefault,
		WriteTimeoutSec:      httpWriteTimeoutDefault,
		IdleTimeoutSec:       httpIdleTimeoutDefault,
	}
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	pick := func(k string, def int) int {
		v := get(k)
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < httpTimeoutMin || n > httpTimeoutMax {
			return def
		}
		return n
	}
	out.ReadHeaderTimeoutSec = pick(httpKeyReadHeaderTimeout, httpReadHeaderTimeoutDefault)
	out.ReadTimeoutSec = pick(httpKeyReadTimeout, httpReadTimeoutDefault)
	out.WriteTimeoutSec = pick(httpKeyWriteTimeout, httpWriteTimeoutDefault)
	out.IdleTimeoutSec = pick(httpKeyIdleTimeout, httpIdleTimeoutDefault)
	return out
}

func (h *SettingsHandler) GetHTTPTimeouts(c *gin.Context) {
	c.JSON(http.StatusOK, LoadHTTPTimeouts(h.DB))
}

func (h *SettingsHandler) SetHTTPTimeouts(c *gin.Context) {
	var b httpTimeoutsDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	check := func(v int, field string) bool {
		if v < httpTimeoutMin || v > httpTimeoutMax {
			apiErr(c, http.StatusBadRequest, "settings.timeout_range",
				field+" must be "+strconv.Itoa(httpTimeoutMin)+".."+strconv.Itoa(httpTimeoutMax)+" seconds")
			return false
		}
		return true
	}
	if !check(b.ReadHeaderTimeoutSec, "readHeaderTimeoutSec") {
		return
	}
	if !check(b.ReadTimeoutSec, "readTimeoutSec") {
		return
	}
	if !check(b.WriteTimeoutSec, "writeTimeoutSec") {
		return
	}
	if !check(b.IdleTimeoutSec, "idleTimeoutSec") {
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		save := func(k string, v int) error {
			return tx.Save(&model.Setting{Key: k, Value: strconv.Itoa(v)}).Error
		}
		if err := save(httpKeyReadHeaderTimeout, b.ReadHeaderTimeoutSec); err != nil {
			return err
		}
		if err := save(httpKeyReadTimeout, b.ReadTimeoutSec); err != nil {
			return err
		}
		if err := save(httpKeyWriteTimeout, b.WriteTimeoutSec); err != nil {
			return err
		}
		return save(httpKeyIdleTimeout, b.IdleTimeoutSec)
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "restartRequired": true})
}
