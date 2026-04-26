package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/shared/protocol"
)

type VolumeHandler struct{ Reg *daemonclient.Registry }

func (h *VolumeHandler) call(c *gin.Context, action string, payload any) {
	id, _ := strconv.Atoi(c.Param("id"))
	cli, ok := h.Reg.Get(uint(id))
	if !ok {
		apiErr(c, http.StatusNotFound, "common.daemon_not_found", "daemon not found")
		return
	}
	if !cli.Connected() {
		apiErr(c, http.StatusServiceUnavailable, "common.daemon_not_connected", "daemon not connected")
		return
	}
	out, err := cli.Call(context.Background(), action, payload)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

func (h *VolumeHandler) List(c *gin.Context) {
	h.call(c, protocol.ActionVolumeList, struct{}{})
}

type volumeCreateBody struct {
	Name      string `json:"name" binding:"required"`
	SizeBytes int64  `json:"sizeBytes" binding:"required"`
	FsType    string `json:"fsType"`
}

func (h *VolumeHandler) Create(c *gin.Context) {
	var b volumeCreateBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	h.call(c, protocol.ActionVolumeCreate, protocol.VolumeCreateReq{
		Name: b.Name, SizeBytes: b.SizeBytes, FsType: b.FsType,
	})
}

func (h *VolumeHandler) Remove(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		apiErr(c, http.StatusBadRequest, "common.missing_name", "missing name")
		return
	}
	h.call(c, protocol.ActionVolumeRemove, protocol.VolumeRemoveReq{Name: name})
}
