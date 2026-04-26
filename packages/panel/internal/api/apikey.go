package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
)

type APIKeyHandler struct{ DB *gorm.DB }

func (h *APIKeyHandler) List(c *gin.Context) {
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)
	q := h.DB.Order("id asc")
	if role != model.RoleAdmin {
		q = q.Where("user_id = ?", uid)
	}
	var ks []model.APIKey
	if err := q.Find(&ks).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, ks)
}

type createKeyReq struct {
	Name        string     `json:"name" binding:"required"`
	IPWhitelist string     `json:"ipWhitelist"`
	Scopes      string     `json:"scopes"`
	// ExpiresAt is optional. Nil / zero / past values are treated as
	// "never expires". Any future time becomes the row's ExpiresAt.
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

type createKeyResp struct {
	Key string         `json:"key"` // shown once
	Row *model.APIKey  `json:"row"`
}

func (h *APIKeyHandler) Create(c *gin.Context) {
	var req createKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	uid, _ := c.Get(auth.CtxUserID)
	var exp *time.Time
	if req.ExpiresAt != nil && req.ExpiresAt.After(time.Now()) {
		exp = req.ExpiresAt
	}
	raw, row, err := auth.IssueAPIKey(h.DB, uid.(uint), req.Name, req.IPWhitelist, req.Scopes, exp)
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, createKeyResp{Key: raw, Row: row})
}

func (h *APIKeyHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)

	var k model.APIKey
	if err := h.DB.First(&k, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	if role != model.RoleAdmin && k.UserID != uid.(uint) {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "forbidden")
		return
	}
	if err := h.DB.Delete(&model.APIKey{}, id).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Revoke marks a single key as revoked (RevokedAt = now) without
// removing the row, so the audit trail / last-used metadata stays
// queryable. Authorization is the same as Delete: own keys only;
// admin can revoke anyone's.
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uid, _ := c.Get(auth.CtxUserID)
	role, _ := c.Get(auth.CtxRole)

	var k model.APIKey
	if err := h.DB.First(&k, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	if role != model.RoleAdmin && k.UserID != uid.(uint) {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "forbidden")
		return
	}
	if k.RevokedAt != nil {
		// Idempotent — already revoked.
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	now := time.Now()
	if err := h.DB.Model(&model.APIKey{}).Where("id = ?", id).Update("revoked_at", now).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RevokeAll marks every active (not-yet-revoked) API key the *current
// user* owns as revoked. Admins still only revoke their own keys —
// admin sweep of someone else's keys is via DELETE on individual rows.
func (h *APIKeyHandler) RevokeAll(c *gin.Context) {
	uid, _ := c.Get(auth.CtxUserID)
	now := time.Now()
	res := h.DB.Model(&model.APIKey{}).
		Where("user_id = ? AND revoked_at IS NULL", uid).
		Update("revoked_at", now)
	if res.Error != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", res.Error.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "revoked": res.RowsAffected})
}
