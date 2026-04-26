package auth

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
	"github.com/taps/shared/ratelimit"
)

const (
	CtxUserID = "uid"
	CtxRole   = "role"
	// CtxScopes is set ONLY when the request authenticated via an API
	// key. JWT-authenticated sessions don't carry it. Empty string
	// means "full access" (legacy / unscoped key); a CSV like
	// "instance.read,files" means the holder is restricted to
	// exactly those route groups.
	CtxScopes = "scopes"
	// CtxIssuedAt carries the `iat` claim of the JWT that authenticated
	// the current request, exposed so handlers like ChangePassword can
	// surgically revoke *older* sessions while leaving the caller's own
	// token alive (set TokensInvalidBefore = iat - 1s). Only set on JWT
	// requests; API-key requests have no iat — handlers must check
	// existence and fall back appropriately.
	CtxIssuedAt = "iat"

	// HdrRefreshedToken carries a freshly-issued JWT when the auth
	// middleware decides to slide the session forward. Frontend
	// interceptors look for this header and stash the new token in
	// localStorage so the next request uses it.
	HdrRefreshedToken = "X-Refreshed-Token"
)

// writeAPIError emits the {error:<code>, message:<english>} response
// shape that the api package's apiErr helper uses. Duplicated here so
// the auth middleware (a separate package) doesn't have to import api
// — that would close a cycle since api imports auth. Keep the JSON
// keys in sync with internal/api/errors.go.
func writeAPIError(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code, "message": msg})
}

// TimingsLoader returns the current JWT TTL on demand. Indirection lets
// the middleware refresh tokens at the admin-tuned TTL without a hot
// import cycle into the api package. Pass nil to disable sliding renewal.
type TimingsLoader func() (jwtTTL time.Duration)

// ValidateRevocableJWT parses a Bearer/query JWT and applies the
// live-revocation check that M-6 introduced. Both the header-style
// auth.Middleware and the query-token queryAuth helper call this so
// the two code paths can't drift on what counts as a valid token.
//
// Returns the parsed claims plus the user's *current* role from DB
// (which the caller may choose to use over claims.Role). The ok bool
// is false when the request was already aborted with 401; the caller
// should just return.
func ValidateRevocableJWT(c *gin.Context, secret []byte, db *gorm.DB, raw string) (*Claims, model.Role, bool) {
	claims, err := ParseToken(secret, raw)
	if err != nil {
		writeAPIError(c, http.StatusUnauthorized, "auth.invalid_token", "invalid token")
		return nil, "", false
	}
	role := claims.Role
	if db != nil {
		var u model.User
		if err := db.Select("id", "role", "tokens_invalid_before").First(&u, claims.UserID).Error; err != nil {
			writeAPIError(c, http.StatusUnauthorized, "auth.user_not_found", "user not found")
			return nil, "", false
		}
		if !u.TokensInvalidBefore.IsZero() && claims.IssuedAt != nil && !claims.IssuedAt.Time.After(u.TokensInvalidBefore) {
			writeAPIError(c, http.StatusUnauthorized, "auth.token_revoked", "token revoked")
			return nil, "", false
		}
		role = u.Role
	}
	return claims, role, true
}

// Middleware accepts both Bearer JWTs and Bearer "tps_..." API keys.
// API key lookups need the DB; pass nil to disable API key auth.
// apiKeyLimit, when non-nil, throttles per-IP repeated API-key failures
// (defense against attackers brute-forcing the 192-bit key space —
// astronomically unlikely to find a match, but the throttle stops the
// attempt itself from acting as a CPU/log DoS).
//
// jwtTTL, when non-nil, drives sliding renewal: any session whose
// remaining lifetime is below half the current TTL gets a fresh JWT in
// the X-Refreshed-Token response header. Passing nil disables renewal
// (used in tests).
func Middleware(secret []byte, db *gorm.DB, apiKeyLimit *ratelimit.Bucket, jwtTTL TimingsLoader) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeAPIError(c, http.StatusUnauthorized, "auth.missing_token", "missing token")
			return
		}
		raw := strings.TrimPrefix(h, "Bearer ")

		if db != nil && IsAPIKey(raw) {
			ip := c.ClientIP()
			if apiKeyLimit != nil {
				if ok, retry := apiKeyLimit.Check(ip); !ok {
					secs := int(retry.Seconds())
					if secs < 1 {
						secs = 1
					}
					c.Header("Retry-After", strconv.Itoa(secs))
					// Mirror the api package's apiErrWithParams shape so
					// frontend formatApiError() can interpolate retryAfter.
					c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"error":   "auth.rate_limited",
						"message": "too many requests; please retry later",
						"params":  gin.H{"retryAfter": secs},
					})
					return
				}
			}
			u, k, err := LookupAPIKey(db, raw, ip)
			if err != nil {
				if apiKeyLimit != nil {
					_, backoff := apiKeyLimit.Fail(ip)
					if backoff > 0 {
						time.Sleep(backoff)
					}
				}
				writeAPIError(c, http.StatusUnauthorized, "auth.invalid_api_key", "invalid api key: "+err.Error())
				return
			}
			if apiKeyLimit != nil {
				apiKeyLimit.Reset(ip)
			}
			c.Set(CtxUserID, u.ID)
			c.Set(CtxRole, u.Role)
			c.Set(CtxScopes, k.Scopes)
			c.Next()
			return
		}

		claims, role, ok := ValidateRevocableJWT(c, secret, db, raw)
		if !ok {
			return
		}
		c.Set(CtxRole, role)
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxScopes, "")
		if claims.IssuedAt != nil {
			c.Set(CtxIssuedAt, claims.IssuedAt.Time)
		}

		// Sliding renewal: when the token is past its halfway point
		// hand back a fresh one in the response header so the active
		// user never sees an expired session. Skipped when the loader
		// is nil (test mode) or claims are missing required fields.
		if jwtTTL != nil && claims.ExpiresAt != nil && claims.IssuedAt != nil {
			ttl := jwtTTL()
			if ttl > 0 {
				remaining := time.Until(claims.ExpiresAt.Time)
				if remaining > 0 && remaining < ttl/2 {
					if newTok, err := IssueToken(secret, claims.UserID, role, ttl); err == nil {
						c.Header(HdrRefreshedToken, newTok)
					}
				}
			}
		}

		c.Next()
	}
}

func RequireRole(roles ...model.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		r, _ := c.Get(CtxRole)
		role, _ := r.(model.Role)
		for _, want := range roles {
			if role == want {
				c.Next()
				return
			}
		}
		writeAPIError(c, http.StatusForbidden, "auth.forbidden", "forbidden")
	}
}

// RequireScope gates a route by API-key scope. JWT sessions and
// unscoped API keys (CtxScopes == "") pass through unchanged — scope
// only meaningfully restricts keys that explicitly listed scopes at
// creation. Each scope is an opaque label; the route layer documents
// which scope it requires.
func RequireScope(want string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, _ := c.Get(CtxScopes)
		s, _ := v.(string)
		if !ScopeMatches(s, want) {
			writeAPIError(c, http.StatusForbidden, "auth.api_key_missing_scope", "api key missing scope: "+want)
			return
		}
		c.Next()
	}
}
