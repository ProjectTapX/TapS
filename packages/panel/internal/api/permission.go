package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

type PermissionHandler struct{ DB *gorm.DB }

type permListItem struct {
	model.InstancePermission
	Username string `json:"username"`
}

// List all permissions, optionally filtered by ?daemonId=&uuid=.
func (h *PermissionHandler) List(c *gin.Context) {
	q := h.DB.Model(&model.InstancePermission{})
	if did := c.Query("daemonId"); did != "" {
		q = q.Where("daemon_id = ?", did)
	}
	if u := c.Query("uuid"); u != "" {
		q = q.Where("uuid = ?", u)
	}
	if uid := c.Query("userId"); uid != "" {
		q = q.Where("user_id = ?", uid)
	}
	var ps []model.InstancePermission
	if err := q.Find(&ps).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	// fetch usernames
	idMap := map[uint]string{}
	if len(ps) > 0 {
		ids := make([]uint, 0, len(ps))
		for _, p := range ps {
			ids = append(ids, p.UserID)
		}
		var us []model.User
		h.DB.Where("id IN ?", ids).Find(&us)
		for _, u := range us {
			idMap[u.ID] = u.Username
		}
	}
	out := make([]permListItem, 0, len(ps))
	for _, p := range ps {
		out = append(out, permListItem{InstancePermission: p, Username: idMap[p.UserID]})
	}
	c.JSON(http.StatusOK, out)
}

type grantReq struct {
	UserID   uint   `json:"userId" binding:"required"`
	DaemonID uint   `json:"daemonId" binding:"required"`
	UUID     string `json:"uuid" binding:"required"`
	Perms    uint32 `json:"perms"`
}

func (h *PermissionHandler) Grant(c *gin.Context) {
	var b grantReq
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	p := model.InstancePermission{UserID: b.UserID, DaemonID: b.DaemonID, UUID: b.UUID, Perms: b.Perms}
	if err := h.DB.Save(&p).Error; err != nil {
		// fallback to insert-or-update via raw on duplicate key
		if err2 := h.DB.Where("user_id = ? AND daemon_id = ? AND uuid = ?", b.UserID, b.DaemonID, b.UUID).
			Assign(model.InstancePermission{Perms: b.Perms}).
			FirstOrCreate(&p).Error; err2 != nil {
			apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
			return
		}
	}
	c.JSON(http.StatusOK, p)
}

func (h *PermissionHandler) Revoke(c *gin.Context) {
	uid, _ := strconv.Atoi(c.Query("userId"))
	did, _ := strconv.Atoi(c.Query("daemonId"))
	uuid := c.Query("uuid")
	if uid == 0 || did == 0 || uuid == "" {
		apiErr(c, http.StatusBadRequest, "common.missing_params", "missing params")
		return
	}
	if err := h.DB.Where("user_id = ? AND daemon_id = ? AND uuid = ?", uid, did, uuid).
		Delete(&model.InstancePermission{}).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Used by InstanceHandler to delete permissions when an instance is removed.
func DeletePermsFor(db *gorm.DB, daemonID uint, uuid string) {
	db.Where("daemon_id = ? AND uuid = ?", daemonID, uuid).Delete(&model.InstancePermission{})
}
