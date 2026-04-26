package api

import (
	"context"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/model"
	"github.com/taps/shared/protocol"
)

type FsHandler struct {
	Reg    *daemonclient.Registry
	DB     *gorm.DB
	Limits *LiveLimits // request-body cap (RAM-DoS guard for /fs/write)
}

// allowedPrefixes returns the file-tree prefixes the current user may touch.
// Admin returns nil (= unlimited). Non-admins are scoped to the per-instance
// /data/inst-<short> subtrees they hold the Files permission on.
func (h *FsHandler) allowedPrefixes(c *gin.Context) ([]string, bool) {
	role, _ := c.Get(auth.CtxRole)
	if role == model.RoleAdmin {
		return nil, true
	}
	uid, _ := c.Get(auth.CtxUserID)
	daemonID, _ := strconv.Atoi(c.Param("id"))
	var ps []model.InstancePermission
	h.DB.Where("user_id = ? AND daemon_id = ?", uid, daemonID).Find(&ps)
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		// Mirror the access.HasPerm legacy fallback so old (perms=0) rows
		// still grant file access.
		if p.Perms != 0 && p.Perms&model.PermFiles == 0 {
			continue
		}
		out = append(out, "/data/inst-"+shortUUIDStr(p.UUID))
	}
	return out, len(out) > 0
}

func shortUUIDStr(u string) string {
	clean := strings.ReplaceAll(u, "-", "")
	if len(clean) > 12 {
		clean = clean[:12]
	}
	return clean
}

// pathAllowed reports whether `p` is permitted under the user's
// scoped prefixes. We canonicalise via path.Clean (POSIX, since the
// daemon mounts use forward slashes) and reject any literal `..`
// segment up front. After cleaning we also reject any input that
// changed shape — e.g. a payload of `/data/inst-x//../inst-y` would
// have cleaned to `/data/inst-y` and slipped past a naive prefix
// check, but rejecting it pre-clean kills the whole class.
//
// The prefix comparison appends a trailing "/" so `/data/inst-1`
// cannot match `/data/inst-10/...`.
func pathAllowed(p string, prefixes []string) bool {
	if prefixes == nil {
		return true
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	// Reject any segment containing ".." regardless of position;
	// path.Clean would resolve them but the resolved path still
	// gives the user a way to escape if they hit a base-collision
	// edge case. Cheap and explicit defence.
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	cleaned := path.Clean(p)
	// path.Clean on an absolute input keeps it absolute; on relative
	// input it might collapse to "." or strip the leading dot. We
	// only trust absolute paths under /data/inst-<short>; reject
	// anything else.
	if !strings.HasPrefix(cleaned, "/") {
		return false
	}
	for _, pre := range prefixes {
		// Trailing "/" prevents `/data/inst-1` from matching
		// `/data/inst-10/...`; the equality branch handles the
		// "list root of my own dir" case.
		if cleaned == pre || strings.HasPrefix(cleaned, pre+"/") {
			return true
		}
	}
	return false
}

func (h *FsHandler) guard(c *gin.Context, paths ...string) bool {
	prefixes, ok := h.allowedPrefixes(c)
	if !ok {
		apiErr(c, http.StatusForbidden, "auth.no_file_access", "no file access on this daemon")
		return false
	}
	for _, p := range paths {
		if !pathAllowed(p, prefixes) {
			apiErr(c, http.StatusForbidden, "auth.path_out_of_scope", "path outside your instance scope")
			return false
		}
	}
	return true
}

func (h *FsHandler) call(c *gin.Context, action string, payload any) {
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

func (h *FsHandler) List(c *gin.Context) {
	p := c.Query("path")
	if !h.guard(c, p) {
		return
	}
	h.call(c, protocol.ActionFsList, protocol.FsListReq{Path: p})
}
func (h *FsHandler) Read(c *gin.Context) {
	p := c.Query("path")
	if !h.guard(c, p) {
		return
	}
	h.call(c, protocol.ActionFsRead, protocol.FsReadReq{Path: p})
}
func (h *FsHandler) Mkdir(c *gin.Context) {
	p := c.Query("path")
	if !h.guard(c, p) {
		return
	}
	h.call(c, protocol.ActionFsMkdir, protocol.FsPathReq{Path: p})
}
func (h *FsHandler) Delete(c *gin.Context) {
	p := c.Query("path")
	if !h.guard(c, p) {
		return
	}
	h.call(c, protocol.ActionFsDelete, protocol.FsPathReq{Path: p})
}

type writeBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (h *FsHandler) Write(c *gin.Context) {
	if h.Limits != nil {
		max := h.Limits.MaxJSONBody()
		limitJSONBody(c, max)
		var b writeBody
		if err := c.ShouldBindJSON(&b); err != nil {
			// MaxBytesReader trips here when the body exceeds max — the
			// gin binder surfaces it as a binding error string we can
			// match on. Keep the structured 413 path for that case.
			if strings.Contains(err.Error(), "http: request body too large") {
				abortPayloadTooLarge(c, max)
				return
			}
			apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
			return
		}
		h.writeAfter(c, b)
		return
	}
	var b writeBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	h.writeAfter(c, b)
}

func (h *FsHandler) writeAfter(c *gin.Context, b writeBody) {
	if !h.guard(c, b.Path) {
		return
	}
	h.call(c, protocol.ActionFsWrite, protocol.FsWriteReq{Path: b.Path, Content: b.Content})
}

type renameBody struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (h *FsHandler) Rename(c *gin.Context) {
	var b renameBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if !h.guard(c, b.From, b.To) {
		return
	}
	h.call(c, protocol.ActionFsRename, protocol.FsRenameReq{From: b.From, To: b.To})
}

func (h *FsHandler) Copy(c *gin.Context) {
	var b renameBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if !h.guard(c, b.From, b.To) {
		return
	}
	h.call(c, protocol.ActionFsCopy, protocol.FsCopyReq{From: b.From, To: b.To})
}

func (h *FsHandler) Move(c *gin.Context) {
	var b renameBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if !h.guard(c, b.From, b.To) {
		return
	}
	h.call(c, protocol.ActionFsMove, protocol.FsMoveReq{From: b.From, To: b.To})
}

type zipBody struct {
	Paths []string `json:"paths"`
	Dest  string   `json:"dest"`
}

func (h *FsHandler) Zip(c *gin.Context) {
	var b zipBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	check := append([]string{b.Dest}, b.Paths...)
	if !h.guard(c, check...) {
		return
	}
	h.call(c, protocol.ActionFsZip, protocol.FsZipReq{Paths: b.Paths, Dest: b.Dest})
}

type unzipBody struct {
	Src     string `json:"src"`
	DestDir string `json:"destDir"`
}

func (h *FsHandler) Unzip(c *gin.Context) {
	var b unzipBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if !h.guard(c, b.Src, b.DestDir) {
		return
	}
	h.call(c, protocol.ActionFsUnzip, protocol.FsUnzipReq{Src: b.Src, DestDir: b.DestDir})
}

type MonitorHandler struct{ Reg *daemonclient.Registry }

func (h *MonitorHandler) Snapshot(c *gin.Context) {
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
	out, err := cli.Call(context.Background(), protocol.ActionMonitorSnap, struct{}{})
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}
