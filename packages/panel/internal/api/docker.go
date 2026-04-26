package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/model"
	"github.com/taps/shared/protocol"
)

type DockerHandler struct {
	Reg *daemonclient.Registry
	DB  *gorm.DB
}

func (h *DockerHandler) call(c *gin.Context, action string, payload any) {
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

// Images returns the daemon's docker image list with panel-side
// display-name aliases merged in (admin-set alias > daemon inspect
// label > empty).
func (h *DockerHandler) Images(c *gin.Context) {
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
	raw, err := cli.Call(context.Background(), protocol.ActionDockerImages, struct{}{})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	var resp protocol.DockerImagesResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		c.Data(http.StatusOK, "application/json", raw)
		return
	}
	// Merge panel-side aliases: admin-set displayName overrides the
	// OCI label the daemon read from docker inspect.
	if h.DB != nil {
		var aliases []model.DockerImageAlias
		h.DB.Where("daemon_id = ?", id).Find(&aliases)
		byRef := map[string]string{}
		for _, a := range aliases {
			byRef[a.ImageRef] = a.DisplayName
		}
		for i := range resp.Images {
			ref := resp.Images[i].Repository + ":" + resp.Images[i].Tag
			if name, ok := byRef[ref]; ok && name != "" {
				resp.Images[i].DisplayName = name
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

type pullBody struct {
	Image string `json:"image" binding:"required"`
}

// Pull streams docker pull progress as Server-Sent Events.
//
// Each SSE message has data being a JSON object:
//
//	{ "type": "line",  "line": "abcd: Downloading [...]" }
//	{ "type": "done",  "error": "" }
func (h *DockerHandler) Pull(c *gin.Context) {
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
	var b pullBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}

	pullID := uuid.NewString()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)

	var mu sync.Mutex
	send := func(obj any) {
		mu.Lock()
		defer mu.Unlock()
		buf, _ := json.Marshal(obj)
		fmt.Fprintf(c.Writer, "data: %s\n\n", buf)
		if flusher != nil {
			flusher.Flush()
		}
	}

	doneCh := make(chan struct{})
	var closeOnce sync.Once

	unsub := cli.Subscribe(pullID, func(action string, payload json.RawMessage) {
		switch action {
		case protocol.ActionDockerPullProgress:
			var ev protocol.DockerPullProgress
			if err := json.Unmarshal(payload, &ev); err == nil {
				send(map[string]any{"type": "line", "line": ev.Line})
			}
		case protocol.ActionDockerPullDone:
			var ev protocol.DockerPullDone
			_ = json.Unmarshal(payload, &ev)
			send(map[string]any{"type": "done", "error": ev.Error})
			closeOnce.Do(func() { close(doneCh) })
		}
	})
	defer unsub()

	// kick off the pull in a goroutine — daemon RPC blocks until done
	go func() {
		_, err := cli.Call(context.Background(), protocol.ActionDockerPull,
			protocol.DockerPullReq{Image: b.Image, PullID: pullID})
		// in case daemon never publishes the done event (RPC error), still close
		if err != nil {
			send(map[string]any{"type": "done", "error": err.Error()})
			closeOnce.Do(func() { close(doneCh) })
		}
	}()

	// initial hello so the client knows the stream is alive
	send(map[string]any{"type": "start", "image": b.Image, "pullId": pullID})

	select {
	case <-doneCh:
	case <-c.Request.Context().Done():
	}
}

func (h *DockerHandler) Remove(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		apiErr(c, http.StatusBadRequest, "common.missing_id", "missing id")
		return
	}
	h.call(c, protocol.ActionDockerRemove, protocol.DockerRemoveReq{ID: id})
}

// ----- image alias CRUD (admin-only) -----

type aliasBody struct {
	DisplayName string `json:"displayName"`
}

// SetAlias creates or updates the panel-side display name for one
// image on one daemon. The ref path param is repository:tag
// (URL-encoded by the client).
func (h *DockerHandler) SetAlias(c *gin.Context) {
	daemonID, _ := strconv.Atoi(c.Param("id"))
	ref := c.Param("ref")
	if ref == "" {
		apiErr(c, http.StatusBadRequest, "common.bad_request", "missing image ref")
		return
	}
	var b aliasBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	name := strings.TrimSpace(b.DisplayName)
	if name == "" {
		// Empty displayName = delete alias
		h.DB.Where("daemon_id = ? AND image_ref = ?", daemonID, ref).Delete(&model.DockerImageAlias{})
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	var row model.DockerImageAlias
	result := h.DB.Where("daemon_id = ? AND image_ref = ?", daemonID, ref).First(&row)
	if result.Error != nil {
		row = model.DockerImageAlias{DaemonID: uint(daemonID), ImageRef: ref, DisplayName: name}
		h.DB.Create(&row)
	} else {
		row.DisplayName = name
		h.DB.Save(&row)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
