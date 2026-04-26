package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/model"
)

// EnforcePasswordChange blocks every authenticated request when the calling
// user has MustChangePassword=true, except the change-password endpoint itself
// and /auth/me.
func EnforcePasswordChange(db *gorm.DB) gin.HandlerFunc {
	allowed := map[string]bool{
		"/api/auth/me":             true,
		"/api/auth/me/password":    true,
		"/api/auth/login":          true,
	}
	return func(c *gin.Context) {
		if allowed[c.Request.URL.Path] {
			c.Next()
			return
		}
		// any websocket/file upload that already validates token via query
		// param uses paths that aren't under /api/* and won't reach this handler.
		uid, _ := c.Get(auth.CtxUserID)
		if uid == nil {
			c.Next()
			return
		}
		var u model.User
		if err := db.Select("must_change_password").First(&u, uid).Error; err == nil && u.MustChangePassword {
			// allow GETs but block writes; strict mode would block all
			if strings.EqualFold(c.Request.Method, http.MethodGet) {
				c.Next()
				return
			}
			apiErr(c, http.StatusPreconditionRequired, "auth.password_change_required", "must change password before continuing")
			return
		}
		c.Next()
	}
}
