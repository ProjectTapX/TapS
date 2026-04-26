package api

import (
	"context"
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
	"github.com/taps/panel/internal/serverdeploy"
	"github.com/taps/shared/protocol"
)

// ServerDeployHandler exposes the server-deploy provider catalog and
// proxies the deploy kickoff / status to the right daemon. Non-admins
// are allowed but must hold both PermFiles and PermControl on the
// target instance (the operation rewrites /data and replaces the
// launch args).
type ServerDeployHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

// Types lists every supported provider, no upstream calls.
func (h *ServerDeployHandler) Types(c *gin.Context) {
	type providerView struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		HasBuilds   bool   `json:"hasBuilds"`
		NeedsImage  bool   `json:"needsImage"`
	}
	out := []providerView{}
	for _, p := range serverdeploy.All() {
		out = append(out, providerView{
			ID: p.ID(), DisplayName: p.DisplayName(), HasBuilds: p.HasBuilds(),
			NeedsImage: p.NeedsImage(),
		})
	}
	c.JSON(http.StatusOK, out)
}

func (h *ServerDeployHandler) Versions(c *gin.Context) {
	id := c.Query("type")
	p, ok := serverdeploy.Get(id)
	if !ok {
		apiErr(c, http.StatusBadRequest, "settings.captcha_provider_unknown", "unknown provider")
		return
	}
	vs, err := p.Versions()
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"versions": vs})
}

func (h *ServerDeployHandler) Builds(c *gin.Context) {
	id := c.Query("type")
	version := c.Query("version")
	p, ok := serverdeploy.Get(id)
	if !ok || version == "" {
		apiErr(c, http.StatusBadRequest, "deploy.type_version_required", "type + version required")
		return
	}
	bs, err := p.Builds(version)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"builds": bs})
}

type deployStartBody struct {
	Type       string `json:"type"`
	Version    string `json:"version"`
	Build      string `json:"build"`
	AcceptEula bool   `json:"acceptEula"`
}

// Start resolves the upstream URL via the panel-side provider, then
// asks the target daemon to begin the install.
func (h *ServerDeployHandler) Start(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	if !h.gate(c, uint(id), uuid) {
		return
	}
	var b deployStartBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	p, ok := serverdeploy.Get(b.Type)
	if !ok {
		apiErr(c, http.StatusBadRequest, "settings.captcha_provider_unknown", "unknown provider")
		return
	}
	r, err := p.Resolve(b.Version, b.Build)
	if err != nil {
		// Validation rejections (invalid version / build) are user
		// input problems → 400. Anything else from the resolver is
		// upstream noise (DNS / 5xx) → 502.
		if strings.HasPrefix(err.Error(), "invalid version") ||
			strings.HasPrefix(err.Error(), "invalid build") ||
			strings.HasPrefix(err.Error(), "validate ") {
			apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
			return
		}
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	cli, ok := h.Reg.Get(uint(id))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusServiceUnavailable, "common.daemon_not_connected", "daemon not connected")
		return
	}
	req := protocol.DeployStartReq{
		UUID:           uuid,
		ProviderID:     b.Type,
		Version:        b.Version,
		Build:          b.Build,
		DownloadURL:    r.URL,
		DownloadName:   r.FileName,
		PostInstallCmd: r.PostInstallCmd,
		LaunchArgs:     r.LaunchArgs,
		AcceptEula:     b.AcceptEula,
	}
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, protocol.ActionInstanceDeployStart, json.RawMessage(body))
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

// Status proxies the daemon's current deploy state for one uuid.
// Browsers poll this once a second while the modal is open.
func (h *ServerDeployHandler) Status(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	if !h.gate(c, uint(id), uuid) {
		return
	}
	cli, ok := h.Reg.Get(uint(id))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusServiceUnavailable, "common.daemon_not_connected", "daemon not connected")
		return
	}
	body, _ := json.Marshal(protocol.InstanceTarget{UUID: uuid})
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, protocol.ActionInstanceDeployStatus, json.RawMessage(body))
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

// gate enforces PermFiles + PermControl for non-admins. Admins pass.
func (h *ServerDeployHandler) gate(c *gin.Context, daemonID uint, uuid string) bool {
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if access.HasPerm(h.DB, uid.(uint), role.(model.Role), daemonID, uuid, model.PermFiles) &&
		access.HasPerm(h.DB, uid.(uint), role.(model.Role), daemonID, uuid, model.PermControl) {
		return true
	}
	apiErr(c, http.StatusForbidden, "common.deploy_needs_file_control", "deploy needs file + control permission on this instance")
	return false
}
