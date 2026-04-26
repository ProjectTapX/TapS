package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// DeployHandler implements one-click Minecraft server deployment using the
// itzg/minecraft-server Docker image, which bundles the JRE and a launcher
// that auto-downloads PaperMC / Vanilla / Forge / Fabric on first start
// based on env vars. No host JRE required, fully sandboxed.
type DeployHandler struct{ Reg *daemonclient.Registry }

type templateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

var builtinTemplates = []templateInfo{
	{ID: "paper", Name: "PaperMC (Docker)", Description: "Paper inside itzg/minecraft-server. JRE bundled, auto-downloads jar on first launch.", Type: "minecraft"},
	{ID: "vanilla", Name: "Vanilla (Docker)", Description: "Vanilla Minecraft inside itzg/minecraft-server.", Type: "minecraft"},
	{ID: "forge", Name: "Forge (Docker)", Description: "Forge inside itzg/minecraft-server. Pick a Minecraft version supported by Forge.", Type: "minecraft"},
}

func (h *DeployHandler) Templates(c *gin.Context) {
	c.JSON(http.StatusOK, builtinTemplates)
}

// Hardcoded common Minecraft versions that itzg/minecraft-server is known to
// support. We no longer call PaperMC's own API since the deployment now goes
// through the docker image which does its own version resolution.
var supportedVersions = []string{
	"LATEST",
	"1.21.4", "1.21.3", "1.21.1", "1.21",
	"1.20.6", "1.20.4", "1.20.2", "1.20.1",
	"1.19.4", "1.18.2", "1.17.1", "1.16.5",
	"1.12.2", "1.8.9",
}

func (h *DeployHandler) PaperVersions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"versions": supportedVersions, "fallback": false})
}

type deployReq struct {
	Template     string `json:"template" binding:"required"`
	Version      string `json:"version"`
	InstanceName string `json:"instanceName" binding:"required"`
	MaxMemory    string `json:"maxMemory"`  // e.g. "2G"
	HostPort     int    `json:"hostPort"`   // host port to map to container 25565
}

func (h *DeployHandler) Deploy(c *gin.Context) {
	role, _ := c.Get(auth.CtxRole)
	if role != model.RoleAdmin {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "admin only")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	cli, ok := h.Reg.Get(uint(id))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusBadGateway, "daemon.not_available", "daemon not available")
		return
	}
	var req deployReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if req.MaxMemory == "" {
		req.MaxMemory = "2G"
	}
	if req.HostPort == 0 {
		req.HostPort = 25565
	}
	if req.Version == "" {
		req.Version = "LATEST"
	}

	// Map our template id to itzg/minecraft-server's TYPE env var.
	var serverType string
	switch req.Template {
	case "paper":
		serverType = "PAPER"
	case "vanilla":
		serverType = "VANILLA"
	case "forge":
		serverType = "FORGE"
	default:
		apiErr(c, http.StatusBadRequest, "deploy.unknown_template", "unknown template")
		return
	}

	cfg := protocol.InstanceConfig{
		Name:           req.InstanceName,
		Type:           "docker",
		Command:        "itzg/minecraft-server:latest",
		StopCmd:        "stop",
		MinecraftHost:  "127.0.0.1",
		MinecraftPort:  req.HostPort,
		OutputEncoding: "utf-8",
		DockerEnv: []string{
			"EULA=TRUE",
			"TYPE=" + serverType,
			"VERSION=" + req.Version,
			"MEMORY=" + req.MaxMemory,
		},
		DockerPorts: []string{
			fmt.Sprintf("%d:25565", req.HostPort),
		},
	}

	infoBytes, err := cli.Call(c.Request.Context(), protocol.ActionInstanceCreate, cfg)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	var info protocol.InstanceInfo
	_ = json.Unmarshal(infoBytes, &info)
	c.JSON(http.StatusOK, gin.H{"ok": true, "instance": info})
}
