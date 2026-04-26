package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/access"
	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/model"
	"github.com/taps/shared/protocol"
)

type McHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

// Players returns the live player list of a Minecraft instance.
func (h *McHandler) Players(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")

	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if !access.Allowed(h.DB, uid.(uint), role.(model.Role), uint(id), uuid) {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "no access")
		return
	}

	cli, ok := h.Reg.Get(uint(id))
	if !ok {
		apiErr(c, http.StatusNotFound, "common.daemon_not_found", "daemon not found")
		return
	}
	if !cli.Connected() {
		apiErr(c, http.StatusServiceUnavailable, "common.daemon_not_connected", "daemon not connected")
		return
	}
	out, err := cli.Call(context.Background(), protocol.ActionMcPlayers, protocol.McPlayersReq{UUID: uuid})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}
