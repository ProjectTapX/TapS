// API error responses use a stable {error:<code>, message:<english>}
// shape so the frontend can show localized text via i18n while older
// clients (and curl users) still see a plain English message.
//
//   apiErr(c, http.StatusBadRequest, "user.email_taken", "Email already in use")
//
// Codes are dotted lower_snake_case grouped by domain. Treat the code
// as a stable contract: never repurpose, only deprecate. If a message
// needs runtime values, pass them via apiErrWithParams so the frontend
// can interpolate translated strings without re-parsing the message.
package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type apiErrorResp struct {
	Code    string         `json:"error"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

func apiErr(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, apiErrorResp{Code: code, Message: msg})
}

func apiErrWithParams(c *gin.Context, status int, code, msg string, params map[string]any) {
	c.AbortWithStatusJSON(status, apiErrorResp{Code: code, Message: msg, Params: params})
}

// apiErrFromDB classifies a gorm error and emits the matching stable
// code, so handlers can stop pasting raw err.Error() (typically
// "constraint failed: UNIQUE constraint failed: users.email (2067)")
// into the response — that leaks SQLite's internals and gives the
// frontend nothing to translate. Falls back to common.internal /
// common.bad_request for anything we don't recognise so the caller
// stays one-line.
//
// Recognition order (gorm 1.25+ typed sentinels first, then string
// match for older drivers / wrapped errors):
//   - gorm.ErrRecordNotFound          → 404 common.not_found
//   - gorm.ErrDuplicatedKey           → 409 common.conflict
//   - gorm.ErrForeignKeyViolated      → 409 common.fk_violation
//   - gorm.ErrCheckConstraintViolated → 400 common.check_failed
//   - SQLite "UNIQUE constraint failed: t.col" string fallback
//   - SQLite "FOREIGN KEY constraint failed" string fallback
//   - anything else                   → 500 common.internal (raw msg
//     stays out of the client; logged server-side via log.Printf)
func apiErrFromDB(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		apiErrWithParams(c, http.StatusConflict, "common.conflict",
			"a record with this value already exists",
			map[string]any{"field": ""})
		return
	}
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		apiErr(c, http.StatusConflict, "common.fk_violation",
			"operation violates a database foreign-key constraint")
		return
	}
	if errors.Is(err, gorm.ErrCheckConstraintViolated) {
		apiErr(c, http.StatusBadRequest, "common.check_failed",
			"value violates a database check constraint")
		return
	}
	msg := err.Error()
	// String-fallback for older driver versions or third-party wrappers
	// that don't translate to gorm's typed sentinels. Field name is
	// preserved in params for the UI when we can extract it.
	if strings.Contains(msg, "UNIQUE constraint failed") {
		field := ""
		if i := strings.Index(msg, "UNIQUE constraint failed: "); i >= 0 {
			rest := msg[i+len("UNIQUE constraint failed: "):]
			if j := strings.IndexAny(rest, " \t("); j > 0 {
				field = rest[:j]
			} else {
				field = rest
			}
		}
		apiErrWithParams(c, http.StatusConflict, "common.conflict",
			"a record with this value already exists",
			map[string]any{"field": field})
		return
	}
	if strings.Contains(msg, "FOREIGN KEY constraint failed") {
		apiErr(c, http.StatusConflict, "common.fk_violation",
			"operation violates a database foreign-key constraint")
		return
	}
	// Default: never echo the raw driver message (audit M2 finding —
	// SQLite internals like "(2067)" / "near \"WHERE\"" leak schema
	// and parser state). Static "internal error" goes to the client;
	// the original error rides into journalctl for ops to grep.
	log.Printf("[apiErrFromDB] unclassified: %v", err)
	apiErr(c, http.StatusInternalServerError, "common.internal", "internal error")
}
