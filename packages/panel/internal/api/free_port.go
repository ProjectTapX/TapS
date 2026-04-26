package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// FreePortHandler returns an unused host port on a given daemon. "Unused"
// means no other instance on this daemon has it in its DockerPorts mapping —
// we can't introspect arbitrary services on the host, but this catches the
// common case of two TapS instances colliding.
type FreePortHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

func (h *FreePortHandler) Get(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	cli, ok := h.Reg.Get(uint(id))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusNotFound, "daemon.not_found_or_disconnected", "daemon not found or disconnected")
		return
	}
	raw, err := cli.Call(context.Background(), protocol.ActionInstanceList, struct{}{})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	var infos []protocol.InstanceInfo
	_ = json.Unmarshal(raw, &infos)

	used := map[int]bool{}
	for _, info := range infos {
		for _, p := range info.Config.DockerPorts {
			if hp := parseHostPort(p); hp > 0 {
				used[hp] = true
			}
		}
	}

	// Resolve the allowed range from the daemon record. Defaults to a small
	// window around the canonical Minecraft port so the first instance lands
	// on 25565 unless the admin overrode the range.
	lo, hi := 25565, 25600
	var d model.Daemon
	if h.DB != nil && h.DB.First(&d, id).Error == nil {
		if d.PortMin > 0 {
			lo = d.PortMin
		}
		if d.PortMax > 0 {
			hi = d.PortMax
		}
	}
	if hi < lo {
		hi = lo
	}

	prefer := lo
	if p := c.Query("prefer"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= lo && n <= hi {
			prefer = n
		}
	}
	if !used[prefer] {
		c.JSON(http.StatusOK, gin.H{"port": prefer})
		return
	}
	for n := prefer + 1; n <= hi; n++ {
		if !used[n] {
			c.JSON(http.StatusOK, gin.H{"port": n})
			return
		}
	}
	for n := prefer - 1; n >= lo; n-- {
		if !used[n] {
			c.JSON(http.StatusOK, gin.H{"port": n})
			return
		}
	}
	apiErr(c, http.StatusConflict, "daemon.no_free_port", "no free port in configured range")
}

// parseHostPort pulls the host-side port number out of a docker -p spec like
// "25565:25565", "0.0.0.0:25565:25565", "25565:25565/udp", or "25565".
func parseHostPort(spec string) int {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0
	}
	if i := strings.Index(spec, "/"); i >= 0 {
		spec = spec[:i]
	}
	parts := strings.Split(spec, ":")
	var hostStr string
	switch len(parts) {
	case 1:
		hostStr = parts[0]
	case 2:
		hostStr = parts[0]
	case 3:
		hostStr = parts[1]
	default:
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(hostStr))
	if err != nil {
		return 0
	}
	return n
}
