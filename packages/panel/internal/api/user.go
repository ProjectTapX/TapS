package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
)

type UserHandler struct {
	DB *gorm.DB
}

// validRole rejects anything outside the documented enum so an admin
// can't set Role to a typo / unknown value that would silently slip
// past auth.RequireRole checks.
func validRole(r model.Role) bool {
	return r == model.RoleAdmin || r == model.RoleUser
}

// adminCount returns the number of admins in the system, optionally
// excluding one user ID (used to check whether demoting/deleting that
// user would leave the system with zero admins).
func (h *UserHandler) adminCount(excludeID uint) (int64, error) {
	var n int64
	q := h.DB.Model(&model.User{}).Where("role = ?", model.RoleAdmin)
	if excludeID != 0 {
		q = q.Where("id != ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}

func (h *UserHandler) List(c *gin.Context) {
	var us []model.User
	if err := h.DB.Order("id asc").Find(&us).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, usersToDTOs(us))
}

type createUserReq struct {
	Username string     `json:"username" binding:"required"`
	Password string     `json:"password" binding:"required"`
	Email    string     `json:"email"`
	Role     model.Role `json:"role"`
}

// normalizeEmail trims and lowercases — keeps the unique index honest
// across "Foo@x.com" vs "foo@x.com". Empty stays empty (email is
// optional for password-only accounts).
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normalizeUsername mirrors normalizeEmail for usernames (audit-2026-
// 04-25 H3): case-insensitive matching across the panel + DB. Login,
// Create, and the SSO usernameTaken probe all funnel through this so
// "Admin" / "admin" / "ADMIN" never coexist.
func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// looksLikeEmail is a deliberately loose check: contains exactly one
// "@", non-empty parts, no spaces. We don't validate deliverability —
// admins set this for SSO matching, not for us to send mail to.
func looksLikeEmail(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') || at == len(s)-1 {
		return false
	}
	return true
}

func (h *UserHandler) Create(c *gin.Context) {
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if req.Role == "" {
		req.Role = model.RoleUser
	}
	if !validRole(req.Role) {
		apiErr(c, http.StatusBadRequest, "user.role_invalid", "invalid role")
		return
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		apiErr(c, http.StatusBadRequest, "user.username_invalid", "username is required")
		return
	}
	email := normalizeEmail(req.Email)
	if email != "" && !looksLikeEmail(email) {
		apiErr(c, http.StatusBadRequest, "user.email_invalid", "invalid email format")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	// B22 pre-check: catch the duplicate-username / duplicate-email case
	// before the INSERT so the response is a clean conflict code rather
	// than a leaky "UNIQUE constraint failed: users.email (2067)" string
	// that the apiErrFromDB fallback would also catch but with less
	// targeted messaging. Pre-checking also keeps DB error logs cleaner.
	{
		var dup model.User
		if err := h.DB.Where("username = ?", username).Take(&dup).Error; err == nil {
			apiErr(c, http.StatusConflict, "user.username_taken", "username is already in use")
			return
		}
	}
	if email != "" {
		var dup model.User
		if err := h.DB.Where("email = ?", email).Take(&dup).Error; err == nil {
			apiErr(c, http.StatusConflict, "user.email_taken", "email is already in use")
			return
		}
	}
	u := &model.User{Username: username, PasswordHash: hash, Email: email, Role: req.Role}
	if err := h.DB.Create(u).Error; err != nil {
		apiErrFromDB(c, err)
		return
	}
	c.JSON(http.StatusOK, userToDTO(u))
}

type updateUserReq struct {
	Password string     `json:"password"`
	Role     model.Role `json:"role"`
	// Email is a *string so the caller can distinguish "don't touch"
	// (omit field) from "set to empty" (send "" to clear). The other
	// fields use zero-value-means-skip; email is special because a
	// blank value is a real, semantically-meaningful state.
	Email *string `json:"email"`
}

func (h *UserHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req updateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	// Pre-hash outside the transaction so the bcrypt cost (~100ms)
	// doesn't hold a row lock; we still validate format here so the
	// transaction body only does cheap checks.
	var hashed string
	if req.Password != "" {
		h2, err := auth.HashPassword(req.Password)
		if err != nil {
			apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
			return
		}
		hashed = h2
	}
	if req.Role != "" && !validRole(req.Role) {
		apiErr(c, http.StatusBadRequest, "user.role_invalid", "invalid role")
		return
	}
	var emPtr *string
	if req.Email != nil {
		em := normalizeEmail(*req.Email)
		if em != "" && !looksLikeEmail(em) {
			apiErr(c, http.StatusBadRequest, "user.email_invalid", "invalid email format")
			return
		}
		emPtr = &em
	}

	// audit-2026-04-24-v3 H6: wrap the entire load → guard → mutate →
	// save sequence in one transaction so two concurrent PUTs each
	// demoting a *different* admin can't both observe "n>=1 remaining
	// admins" and commit, leaving the system with zero. Mirrors the
	// User.Delete pattern from B7. Locking{UPDATE} is a no-op on SQLite
	// (file-level write serialisation already guarantees exclusivity)
	// but documents intent for any future MySQL/PostgreSQL switch.
	var saved model.User
	var sentinelErr error
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var u model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, id).Error; err != nil {
			sentinelErr = errUserNotFound
			return sentinelErr
		}
		bumpTokens := false
		if hashed != "" {
			u.PasswordHash = hashed
			u.HasPassword = true
			bumpTokens = true
		}
		if req.Role != "" {
			if u.Role == model.RoleAdmin && req.Role != model.RoleAdmin {
				// adminCount runs INSIDE the transaction so a
				// concurrent demote/delete can't slip in between
				// the count and our Save.
				var n int64
				q := tx.Model(&model.User{}).
					Clauses(clause.Locking{Strength: "UPDATE"}).
					Where("role = ? AND id != ?", model.RoleAdmin, u.ID)
				if err := q.Count(&n).Error; err != nil {
					return err
				}
				if n < 1 {
					sentinelErr = errLastAdminDemote
					return sentinelErr
				}
			}
			if u.Role != req.Role {
				bumpTokens = true
			}
			u.Role = req.Role
		}
		if emPtr != nil {
			em := *emPtr
			if em != "" && em != u.Email {
				var dup model.User
				if err := tx.Where("email = ? AND id != ?", em, u.ID).Take(&dup).Error; err == nil {
					sentinelErr = errEmailTaken
					return sentinelErr
				}
			}
			u.Email = em
		}
		if bumpTokens {
			u.TokensInvalidBefore = time.Now()
		}
		if err := tx.Save(&u).Error; err != nil {
			return err
		}
		saved = u
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(sentinelErr, errUserNotFound):
			apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		case errors.Is(sentinelErr, errLastAdminDemote):
			apiErr(c, http.StatusBadRequest, "user.cannot_demote_last_admin", "cannot demote the last admin")
		case errors.Is(sentinelErr, errEmailTaken):
			apiErr(c, http.StatusConflict, "user.email_taken", "email is already in use")
		default:
			apiErrFromDB(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, userToDTO(&saved))
}

var (
	errUserNotFound    = errors.New("user not found")
	errLastAdminDemote = errors.New("cannot demote last admin")
	errEmailTaken      = errors.New("email taken")
)

func (h *UserHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	uid, _ := c.Get(auth.CtxUserID)
	if uint(id) == uid.(uint) {
		apiErr(c, http.StatusBadRequest, "user.cannot_delete_self", "cannot delete self")
		return
	}
	var target model.User
	if err := h.DB.First(&target, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	// Refuse to delete the last remaining admin (see Update for the
	// same check on demotion).
	if target.Role == model.RoleAdmin {
		n, err := h.adminCount(target.ID)
		if err != nil {
			apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
			return
		}
		if n < 1 {
			apiErr(c, http.StatusBadRequest, "user.cannot_delete_last_admin", "cannot delete the last admin")
			return
		}
	}
	// Wrap the delete + every cascade in one transaction so a partial
	// failure (e.g. SSOIdentity delete errors) doesn't leave a torso
	// where the User row is gone but instance permissions / API keys
	// linger as orphans. Sharing the transaction with SetLoginMethod's
	// guard also prevents the "delete last bound admin" race against a
	// concurrent oidc-only switch — see B4 in the audit.
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.User{}, id).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.APIKey{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.SSOIdentity{}).Error; err != nil {
			return err
		}
		// InstancePermission cascade — without this, a future user
		// inserted with the same numeric ID (e.g. via SQL restore)
		// would silently inherit the deleted user's per-instance
		// permissions. The other cascades above were already there;
		// this row got missed.
		if err := tx.Where("user_id = ?", id).Delete(&model.InstancePermission{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
