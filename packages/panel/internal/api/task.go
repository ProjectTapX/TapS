package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/access"
	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/panel/internal/scheduler"
)

type TaskHandler struct {
	DB    *gorm.DB
	Sched *scheduler.Scheduler
}

// gate enforces per-instance permission. Read uses PermView; create
// /update / delete (which schedule cron-driven actions equivalent to
// console input or start/stop) require PermControl. Without these,
// any authed user could schedule a `command` task on any instance,
// fully bypassing per-instance perms.
func (h *TaskHandler) gate(c *gin.Context, perm uint32) (uint, string, bool) {
	daemonID, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if !access.HasPerm(h.DB, uid.(uint), role.(model.Role), uint(daemonID), uuid, perm) {
		apiErr(c, http.StatusForbidden, "auth.missing_permission", "missing permission for this instance")
		return 0, "", false
	}
	return uint(daemonID), uuid, true
}

// gateForTask resolves the (daemonId, uuid) from an existing task row
// (looked up by :taskId) and runs the same permission check. Used by
// Update / Delete where the path doesn't carry the instance uuid.
func (h *TaskHandler) gateForTask(c *gin.Context, perm uint32) (*model.Task, bool) {
	id, _ := strconv.Atoi(c.Param("taskId"))
	var t model.Task
	if err := h.DB.First(&t, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return nil, false
	}
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if !access.HasPerm(h.DB, uid.(uint), role.(model.Role), t.DaemonID, t.UUID, perm) {
		apiErr(c, http.StatusForbidden, "auth.missing_permission", "missing permission for this instance")
		return nil, false
	}
	return &t, true
}

func (h *TaskHandler) List(c *gin.Context) {
	daemonID, uuid, ok := h.gate(c, model.PermView)
	if !ok {
		return
	}
	var ts []model.Task
	if err := h.DB.Where("daemon_id = ? AND uuid = ?", daemonID, uuid).Order("id asc").Find(&ts).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, ts)
}

type taskBody struct {
	Name    string `json:"name"`
	Cron    string `json:"cron" binding:"required"`
	Action  string `json:"action" binding:"required"` // command|start|stop|restart
	Data    string `json:"data"`
	Enabled *bool  `json:"enabled"`
}

func (h *TaskHandler) Create(c *gin.Context) {
	daemonID, uuid, ok := h.gate(c, model.PermControl)
	if !ok {
		return
	}
	var b taskBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	t := model.Task{
		DaemonID: daemonID, UUID: uuid,
		Name: b.Name, Cron: b.Cron, Action: model.TaskAction(b.Action), Data: b.Data,
		Enabled: b.Enabled == nil || *b.Enabled,
	}
	if err := h.DB.Create(&t).Error; err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	if err := h.Sched.Upsert(t); err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *TaskHandler) Update(c *gin.Context) {
	t, ok := h.gateForTask(c, model.PermControl)
	if !ok {
		return
	}
	// Defence in depth: refuse to let the body smuggle the row to a
	// different (daemonId, uuid) — keep the original instance binding.
	originalDaemon := t.DaemonID
	originalUUID := t.UUID
	var b taskBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.Name != "" {
		t.Name = b.Name
	}
	if b.Cron != "" {
		t.Cron = b.Cron
	}
	if b.Action != "" {
		t.Action = model.TaskAction(b.Action)
	}
	t.Data = b.Data
	if b.Enabled != nil {
		t.Enabled = *b.Enabled
	}
	t.DaemonID = originalDaemon
	t.UUID = originalUUID
	if err := h.DB.Save(t).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	if err := h.Sched.Upsert(*t); err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *TaskHandler) Delete(c *gin.Context) {
	t, ok := h.gateForTask(c, model.PermControl)
	if !ok {
		return
	}
	h.Sched.Remove(t.ID)
	if err := h.DB.Delete(&model.Task{}, t.ID).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
