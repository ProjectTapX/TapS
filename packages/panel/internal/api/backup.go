package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/access"
	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/panel/internal/monitorhist"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// backupNameRe restricts backup file names to a safe character set so a
// crafted name can't escape the per-instance backup directory on the
// daemon (defense in depth — daemon also validates) or sneak control
// chars into shell / log output.
var backupNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// backupNoteMaxLen caps the user-visible note attached to a backup so
// a single backup row can't bloat the DB or terminal display. Hardcoded
// for now; Batch #3 moves this into SystemSettings.
const backupNoteMaxLen = 512

func validateBackupName(c *gin.Context, name string) bool {
	if !backupNameRe.MatchString(name) {
		apiErr(c, http.StatusBadRequest, "backup.name_invalid", "invalid backup name (allowed: A-Z a-z 0-9 . _ -, 1-128 chars)")
		return false
	}
	return true
}

func validateBackupNote(c *gin.Context, note string) (string, bool) {
	if strings.ContainsAny(note, "\r\n") {
		apiErr(c, http.StatusBadRequest, "common.note_no_breaks", "note must not contain line breaks")
		return "", false
	}
	if len(note) > backupNoteMaxLen {
		apiErr(c, http.StatusBadRequest, "common.note_too_long", "note too long")
		return "", false
	}
	return note, true
}

type BackupHandler struct {
	DB   *gorm.DB
	Reg  *daemonclient.Registry
}

func (h *BackupHandler) check(c *gin.Context) (*daemonclient.Client, string, bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if !access.Allowed(h.DB, uid.(uint), role.(model.Role), uint(id), uuid) {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "no access")
		return nil, "", false
	}
	cli, ok := h.Reg.Get(uint(id))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusBadGateway, "daemon.not_available", "daemon not available")
		return nil, "", false
	}
	return cli, uuid, true
}

func (h *BackupHandler) call(c *gin.Context, action string, payload any) {
	cli, _, ok := h.check(c)
	if !ok {
		return
	}
	out, err := cli.Call(context.Background(), action, payload)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

func (h *BackupHandler) List(c *gin.Context) {
	h.call(c, protocol.ActionBackupList, protocol.BackupListReq{UUID: c.Param("uuid")})
}

type backupCreateBody struct {
	Note string `json:"note"`
}

func (h *BackupHandler) Create(c *gin.Context) {
	var b backupCreateBody
	_ = c.ShouldBindJSON(&b)
	note, ok := validateBackupNote(c, b.Note)
	if !ok {
		return
	}
	h.call(c, protocol.ActionBackupCreate, protocol.BackupCreateReq{UUID: c.Param("uuid"), Note: note})
}

type backupNameBody struct {
	Name string `json:"name"`
}

func (h *BackupHandler) Restore(c *gin.Context) {
	var b backupNameBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if !validateBackupName(c, b.Name) {
		return
	}
	h.call(c, protocol.ActionBackupRestore, protocol.BackupRestoreReq{UUID: c.Param("uuid"), Name: b.Name})
}

func (h *BackupHandler) Delete(c *gin.Context) {
	name := c.Query("name")
	if !validateBackupName(c, name) {
		return
	}
	h.call(c, protocol.ActionBackupDelete, protocol.BackupDeleteReq{UUID: c.Param("uuid"), Name: name})
}

// Download streams a backup zip from the daemon's HTTP endpoint to the
// browser. Auth via the same query token the file download endpoint uses.
func (h *BackupHandler) Download(c *gin.Context) {
	cli, uuid, ok := h.check(c)
	if !ok {
		return
	}
	name := c.Query("name")
	if name == "" {
		apiErr(c, http.StatusBadRequest, "common.missing_name", "missing name")
		return
	}
	if !validateBackupName(c, name) {
		return
	}
	q := url.Values{}
	q.Set("uuid", uuid)
	q.Set("name", name)
	q.Set("token", cli.Token())
	target := "https://" + cli.Address() + "/backups/download?" + q.Encode()
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	hc, err := cli.HTTPClient(0)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	resp, err := hc.Do(req)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	defer resp.Body.Close()
	copySafeDaemonHeaders(c.Writer.Header(), resp.Header)
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// ----- monitor history -----

type MonitorHistoryHandler struct {
	Reg  *daemonclient.Registry
	Hist *monitorhist.Collector
}

func (h *MonitorHistoryHandler) History(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	out := h.Hist.History(uint(id))
	if out == nil {
		out = []protocol.MonitorSnapshot{}
	}
	c.JSON(http.StatusOK, out)
}

// ----- per-instance process snapshot -----

type ProcessHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

func (h *ProcessHandler) Snapshot(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	if !access.Allowed(h.DB, uid.(uint), role.(model.Role), uint(id), uuid) {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "no access")
		return
	}
	cli, ok := h.Reg.Get(uint(id))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusBadGateway, "daemon.not_available", "daemon not available")
		return
	}
	out, err := cli.Call(context.Background(), protocol.ActionMonitorProcess, protocol.MonitorProcessReq{UUID: uuid})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}
