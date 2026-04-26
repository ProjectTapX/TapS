package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
)

// queryAuth validates a JWT passed via ?token= for endpoints where the
// browser cannot set Authorization headers (downloads, uploads via
// <form>, websockets). Shares ValidateRevocableJWT with auth.Middleware
// so the live-revocation check (M-6) is honoured on this path too —
// without it a stolen old JWT would keep traversing the file proxy
// after a password change.
//
// Sliding renewal is *not* performed here because the typical caller
// (a download <a href> or a WS upgrade) cannot read response headers
// to consume the new token.
func queryAuth(secret []byte, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t := c.Query("token")
		if t == "" {
			apiErr(c, http.StatusUnauthorized, "auth.missing_token", "missing token")
			return
		}
		claims, role, ok := auth.ValidateRevocableJWT(c, secret, db, t)
		if !ok {
			return
		}
		c.Set(auth.CtxUserID, claims.UserID)
		c.Set(auth.CtxRole, role)
	}
}
