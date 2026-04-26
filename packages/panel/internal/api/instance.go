package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/access"
	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/model"
	"github.com/taps/shared/protocol"
)

type InstanceHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

func (h *InstanceHandler) requirePerm(c *gin.Context, daemonID uint, uuid string, perm uint32) bool {
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if access.HasPerm(h.DB, uid.(uint), role.(model.Role), daemonID, uuid, perm) {
		return true
	}
	apiErr(c, http.StatusForbidden, "auth.missing_permission", "missing permission for this instance")
	return false
}

func (h *InstanceHandler) checkAccess(c *gin.Context, daemonID uint, uuid string) bool {
	return h.requirePerm(c, daemonID, uuid, model.PermControl)
}

func (h *InstanceHandler) client(c *gin.Context) (*daemonclient.Client, bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	cli, ok := h.Reg.Get(uint(id))
	if !ok {
		apiErr(c, http.StatusNotFound, "common.daemon_not_found", "daemon not found")
		return nil, false
	}
	if !cli.Connected() {
		apiErr(c, http.StatusServiceUnavailable, "common.daemon_not_connected", "daemon not connected")
		return nil, false
	}
	return cli, true
}

func (h *InstanceHandler) call(c *gin.Context, action string, payload any) {
	cli, ok := h.client(c)
	if !ok {
		return
	}
	// All panel→daemon synchronous calls get a finite deadline so a
	// stuck WS or unresponsive daemon doesn't pile up gin handlers
	// indefinitely.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, action, payload)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

// List instances on one daemon, filtered by access.
func (h *InstanceHandler) List(c *gin.Context) {
	cli, ok := h.client(c)
	if !ok {
		return
	}
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	allowed, isAdmin := access.AllowedSet(h.DB, uid.(uint), role.(model.Role))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, protocol.ActionInstanceList, struct{}{})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	if isAdmin {
		c.Data(http.StatusOK, "application/json", out)
		return
	}
	var infos []protocol.InstanceInfo
	_ = json.Unmarshal(out, &infos)
	filtered := make([]protocol.InstanceInfo, 0, len(infos))
	did, _ := strconv.Atoi(c.Param("id"))
	for _, info := range infos {
		if _, ok := allowed[access.Key(uint(did), info.Config.UUID)]; ok {
			filtered = append(filtered, info)
		}
	}
	c.JSON(http.StatusOK, filtered)
}

// Create requires admin (only admin can provision new instances).
func (h *InstanceHandler) Create(c *gin.Context) {
	role, _ := c.Get(auth.CtxRole)
	if role != model.RoleAdmin {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "admin only")
		return
	}
	var cfg protocol.InstanceConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if cli, ok := h.Reg.Get(uint(id)); ok && cli.Welcome().RequireDocker && cfg.Type != "docker" {
		apiErr(c, http.StatusBadRequest, "daemon.docker_only", "this node only allows docker instances; set type=docker")
		return
	}
	// Auto-assign a name when the user left it blank — derived from the image
	// when possible so the listing stays readable, otherwise just a random
	// suffix. Daemon enforces uniqueness by UUID, not by name.
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = autoInstanceName(cfg)
	}
	h.call(c, protocol.ActionInstanceCreate, cfg)
}

// autoInstanceName builds a default human name from the image (if docker) or a
// random hex suffix.
func autoInstanceName(cfg protocol.InstanceConfig) string {
	short := randHex(4)
	if cfg.Type == "docker" && cfg.Command != "" {
		// e.g. "eclipse-temurin:21-jre-alpine" → "temurin-21"
		ref := cfg.Command
		if i := strings.LastIndex(ref, "/"); i >= 0 {
			ref = ref[i+1:]
		}
		repo, tag, _ := strings.Cut(ref, ":")
		if dash := strings.Index(repo, "-"); dash >= 0 {
			repo = repo[dash+1:]
		}
		majorTag := tag
		if i := strings.IndexAny(tag, "-."); i >= 0 {
			majorTag = tag[:i]
		}
		base := repo
		if majorTag != "" {
			base = repo + "-" + majorTag
		}
		return base + "-" + short
	}
	return "inst-" + short
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// firstHostPortStr / firstContainerPortStr split a docker -p spec the
// way docker itself does ("host:container", "ip:host:container", or just
// "port"). Returns the relevant half as a string (kept as string so the
// caller can re-concat without losing leading zeros, even though we
// don't expect any). Used by the user-role partial update path.
func firstHostPortStr(spec string) string {
	body := spec
	if i := strings.Index(body, "/"); i >= 0 {
		body = body[:i]
	}
	parts := strings.Split(body, ":")
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return parts[0]
	case 3:
		return parts[1]
	}
	return ""
}
func firstContainerPortStr(spec string) string {
	body := spec
	if i := strings.Index(body, "/"); i >= 0 {
		body = body[:i]
	}
	parts := strings.Split(body, ":")
	return parts[len(parts)-1]
}

func (h *InstanceHandler) Update(c *gin.Context) {
	did, _ := strconv.Atoi(c.Param("id"))
	if !h.checkAccess(c, uint(did), c.Param("uuid")) {
		return
	}
	var cfg protocol.InstanceConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	cfg.UUID = c.Param("uuid")
	role, _ := c.Get(auth.CtxRole)
	if role != model.RoleAdmin {
		// Non-admins may only tweak runtime knobs (the launch command, the
		// stop directive, the output encoding). Fetch the current cfg from
		// the daemon and overlay just those fields so they can't sneak in
		// changes to the image, ports, volumes, or limits.
		cli, ok := h.Reg.Get(uint(did))
		if !ok || !cli.Connected() {
			apiErr(c, http.StatusBadGateway, "daemon.not_available", "daemon not available")
			return
		}
		ctx2, cancel2 := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel2()
		raw, err := cli.Call(ctx2, protocol.ActionInstanceList, struct{}{})
		if err != nil {
			apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
			return
		}
		var infos []protocol.InstanceInfo
		_ = json.Unmarshal(raw, &infos)
		var prev *protocol.InstanceConfig
		for i := range infos {
			if infos[i].Config.UUID == cfg.UUID {
				prev = &infos[i].Config
				break
			}
		}
		if prev == nil {
			apiErr(c, http.StatusNotFound, "instance.not_found", "instance not found")
			return
		}
		merged := *prev
		merged.Name = cfg.Name
		merged.Command = cfg.Command
		merged.Args = cfg.Args
		merged.StopCmd = cfg.StopCmd
		merged.OutputEncoding = cfg.OutputEncoding
		merged.CompletionWords = cfg.CompletionWords
		// Container port: users may swap the in-container port (e.g. their
		// MC server.properties uses 25566) but not the host side. We
		// preserve prev's host port and only adopt the container half from
		// the incoming spec.
		if len(cfg.DockerPorts) > 0 && len(prev.DockerPorts) > 0 {
			hostStr := firstHostPortStr(prev.DockerPorts[0])
			contStr := firstContainerPortStr(cfg.DockerPorts[0])
			if hostStr != "" && contStr != "" {
				merged.DockerPorts = []string{hostStr + ":" + contStr}
			}
		}
		cfg = merged
	}
	h.call(c, protocol.ActionInstanceUpdate, cfg)
}

func (h *InstanceHandler) Start(c *gin.Context) {
	did, _ := strconv.Atoi(c.Param("id"))
	if !h.checkAccess(c, uint(did), c.Param("uuid")) {
		return
	}
	h.call(c, protocol.ActionInstanceStart, protocol.InstanceTarget{UUID: c.Param("uuid")})
}
func (h *InstanceHandler) Stop(c *gin.Context) {
	did, _ := strconv.Atoi(c.Param("id"))
	if !h.checkAccess(c, uint(did), c.Param("uuid")) {
		return
	}
	h.call(c, protocol.ActionInstanceStop, protocol.InstanceTarget{UUID: c.Param("uuid")})
}
func (h *InstanceHandler) Kill(c *gin.Context) {
	did, _ := strconv.Atoi(c.Param("id"))
	if !h.checkAccess(c, uint(did), c.Param("uuid")) {
		return
	}
	h.call(c, protocol.ActionInstanceKill, protocol.InstanceTarget{UUID: c.Param("uuid")})
}

func (h *InstanceHandler) Delete(c *gin.Context) {
	role, _ := c.Get(auth.CtxRole)
	if role != model.RoleAdmin {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "admin only")
		return
	}
	did, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	h.call(c, protocol.ActionInstanceDelete, protocol.InstanceTarget{UUID: uuid})
	// also clean up perms + tasks
	DeletePermsFor(h.DB, uint(did), uuid)
	h.DB.Where("daemon_id = ? AND uuid = ?", did, uuid).Delete(&model.Task{})
}

type inputBody struct {
	Data string `json:"data"`
}

func (h *InstanceHandler) Input(c *gin.Context) {
	did, _ := strconv.Atoi(c.Param("id"))
	if !h.checkAccess(c, uint(did), c.Param("uuid")) {
		return
	}
	var b inputBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	h.call(c, protocol.ActionInstanceInput, protocol.InstanceInputReq{UUID: c.Param("uuid"), Data: b.Data})
}

// DockerStats proxies the daemon's instance.dockerStats action: live mem/cpu
// for one container. Access-checked like the other per-instance ops.
func (h *InstanceHandler) DockerStats(c *gin.Context) {
	did, _ := strconv.Atoi(c.Param("id"))
	if !h.checkAccess(c, uint(did), c.Param("uuid")) {
		return
	}
	h.call(c, protocol.ActionInstanceDockerStats, protocol.InstanceTarget{UUID: c.Param("uuid")})
}

// DockerStatsAll returns stats for every running container on a daemon in
// one shot. Used by the dashboard to populate N instance cards without
// firing N separate `docker stats` calls. We filter the response down to
// just the containers the caller is allowed to see.
func (h *InstanceHandler) DockerStatsAll(c *gin.Context) {
	cli, ok := h.client(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, protocol.ActionInstanceDockerStatsAll, struct{}{})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if role == model.RoleAdmin {
		c.Data(http.StatusOK, "application/json", out)
		return
	}
	allowed, _ := access.AllowedSet(h.DB, uid.(uint), role.(model.Role))
	did, _ := strconv.Atoi(c.Param("id"))
	var resp protocol.DockerStatsAllResp
	_ = json.Unmarshal(out, &resp)
	filtered := make([]protocol.DockerStatsResp, 0, len(resp.Items))
	for _, it := range resp.Items {
		if !strings.HasPrefix(it.Name, "taps-") {
			continue
		}
		uuid := it.Name[len("taps-"):]
		if _, ok := allowed[access.Key(uint(did), uuid)]; ok {
			filtered = append(filtered, it)
		}
	}
	c.JSON(http.StatusOK, protocol.DockerStatsAllResp{Items: filtered})
}

// PlayersAll proxies the daemon's instance.playersAll batch SLP-ping. Each
// item is filtered against the caller's instance permissions so a non-admin
// only sees player counts for instances they can already access.
func (h *InstanceHandler) PlayersAll(c *gin.Context) {
	cli, ok := h.client(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, protocol.ActionInstancePlayersAll, struct{}{})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if role == model.RoleAdmin {
		c.Data(http.StatusOK, "application/json", out)
		return
	}
	allowed, _ := access.AllowedSet(h.DB, uid.(uint), role.(model.Role))
	did, _ := strconv.Atoi(c.Param("id"))
	var resp protocol.PlayersAllResp
	_ = json.Unmarshal(out, &resp)
	filtered := make([]protocol.PlayersBrief, 0, len(resp.Items))
	for _, it := range resp.Items {
		if _, ok := allowed[access.Key(uint(did), it.UUID)]; ok {
			filtered = append(filtered, it)
		}
	}
	c.JSON(http.StatusOK, protocol.PlayersAllResp{Items: filtered})
}

// AggregateList walks every connected daemon, filtered by access.
func (h *InstanceHandler) AggregateList(c *gin.Context) {
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	allowed, isAdmin := access.AllowedSet(h.DB, uid.(uint), role.(model.Role))

	type row struct {
		DaemonID uint                  `json:"daemonId"`
		Info     protocol.InstanceInfo `json:"info"`
	}
	out := []row{}
	ids := []uint{}
	h.Reg.Each(func(c *daemonclient.Client) { ids = append(ids, c.ID()) })
	for _, id := range ids {
		cli, ok := h.Reg.Get(id)
		if !ok || !cli.Connected() {
			continue
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		raw, err := cli.Call(ctx, protocol.ActionInstanceList, struct{}{})
		cancel()
		if err != nil {
			continue
		}
		var infos []protocol.InstanceInfo
		_ = json.Unmarshal(raw, &infos)
		for _, info := range infos {
			if !isAdmin {
				if _, ok := allowed[access.Key(id, info.Config.UUID)]; !ok {
					continue
				}
			}
			out = append(out, row{DaemonID: id, Info: info})
		}
	}
	c.JSON(http.StatusOK, out)
}
