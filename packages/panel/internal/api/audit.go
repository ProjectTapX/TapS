package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/model"
)

// AuditMiddleware records every mutating request (POST/PUT/DELETE) into the
// audit_logs table. Skipped for non-mutating reads to keep volume manageable.
func AuditMiddleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete:
		default:
			return
		}
		uid, _ := c.Get(auth.CtxUserID)
		var username string
		if uidVal, ok := uid.(uint); ok && uidVal != 0 {
			var u model.User
			if err := db.Select("username").First(&u, uidVal).Error; err == nil {
				username = u.Username
			}
		}
		entry := &model.AuditLog{
			Time:       time.Now(),
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			Status:     c.Writer.Status(),
			IP:         c.ClientIP(),
			DurationMs: time.Since(start).Milliseconds(),
			Username:   username,
		}
		if uidVal, ok := uid.(uint); ok {
			entry.UserID = uidVal
		}
		_ = db.Create(entry).Error
	}
}

type AuditHandler struct{ DB *gorm.DB }

func (h *AuditHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	q := h.DB.Order("id desc").Limit(limit).Offset(offset)
	if uname := c.Query("username"); uname != "" {
		q = q.Where("username LIKE ?", "%"+uname+"%")
	}
	if path := c.Query("path"); path != "" {
		q = q.Where("path LIKE ?", "%"+path+"%")
	}
	var rows []model.AuditLog
	if err := q.Find(&rows).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	var total int64
	h.DB.Model(&model.AuditLog{}).Count(&total)
	c.JSON(http.StatusOK, gin.H{"items": rows, "total": total})
}

// ListLogins returns the login attempts table, optionally narrowed to a
// single user via ?userId= or ?username= search. Admin only (gated by the
// route group).
func (h *AuditHandler) ListLogins(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	// Build the same filter chain twice — once for the page, once for the
	// total — so the count matches what the client is paging through.
	apply := func(q *gorm.DB) *gorm.DB {
		if uid := c.Query("userId"); uid != "" {
			q = q.Where("user_id = ?", uid)
		}
		if uname := c.Query("username"); uname != "" {
			q = q.Where("username LIKE ?", "%"+uname+"%")
		}
		switch c.Query("success") {
		case "true":
			q = q.Where("success = ?", true)
		case "false":
			q = q.Where("success = ?", false)
		}
		if reason := c.Query("reason"); reason != "" {
			// exact match on the canonical reason string written by the
			// login handler ("no such user" / "wrong password" / "issue
			// token failed"). Filtering by reason implies success=false.
			q = q.Where("reason = ?", reason).Where("success = ?", false)
		}
		return q
	}

	var rows []model.LoginLog
	if err := apply(h.DB).Order("id desc").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	var total int64
	apply(h.DB.Model(&model.LoginLog{})).Count(&total)
	c.JSON(http.StatusOK, gin.H{"items": rows, "total": total})
}
