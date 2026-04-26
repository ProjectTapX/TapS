// Live, admin-tunable size caps for incoming requests. Both knobs are
// loaded at boot from the `settings` table and re-applied whenever the
// admin saves the limits page; consumers should call the getters on
// every request rather than caching at construction so updates take
// effect immediately.
package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

const (
	limKeyMaxJSONBody    = "limits.maxJsonBodyBytes"
	limKeyMaxWSFrame     = "limits.maxWsFrameBytes"
	limKeyMaxRequestBody = "limits.maxRequestBodyBytes"

	// Defaults match the audit's recommendation (16 MiB). Range stays
	// at 1 MiB ... 128 MiB — anything beyond that should go through the
	// streaming /files/upload endpoint, not a buffered JSON request.
	limDefaultBytes = 16 << 20
	limMinBytes     = 1 << 20
	limMaxBytes     = 128 << 20

	// Global per-request cap applied by BodyLimitMiddleware *before*
	// any handler runs. Protects every admin POST/PUT against a
	// stolen-token RAM-DoS without anyone having to remember to wrap
	// their handler. Handlers that legitimately need bigger bodies
	// (fs/write, file upload, favicon) re-wrap with their own larger
	// limit — http.MaxBytesReader is replaceable.
	//
	// Default 128 KiB is plenty for any structured JSON payload
	// (login, user create, daemon edit, etc.). Range 1 KiB ... 4 MiB.
	limDefaultGlobalBytes = 128 << 10
	limMinGlobalBytes     = 1 << 10
	limMaxGlobalBytes     = 4 << 20
)

// LiveLimits is the in-memory home for the byte caps. Reads/writes
// go through atomic int64s so the SettingsHandler can update them from
// a request goroutine without locks.
type LiveLimits struct {
	maxJSONBody    atomic.Int64
	maxWSFrame     atomic.Int64
	maxRequestBody atomic.Int64
}

func NewLiveLimits(db *gorm.DB) *LiveLimits {
	l := &LiveLimits{}
	jb, ws, rb := loadLimits(db)
	l.maxJSONBody.Store(int64(jb))
	l.maxWSFrame.Store(int64(ws))
	l.maxRequestBody.Store(int64(rb))
	return l
}

func (l *LiveLimits) MaxJSONBody() int64    { return l.maxJSONBody.Load() }
func (l *LiveLimits) MaxWSFrame() int64     { return l.maxWSFrame.Load() }
func (l *LiveLimits) MaxRequestBody() int64 { return l.maxRequestBody.Load() }

func (l *LiveLimits) Apply(jsonBody, wsFrame, requestBody int) {
	l.maxJSONBody.Store(int64(jsonBody))
	l.maxWSFrame.Store(int64(wsFrame))
	l.maxRequestBody.Store(int64(requestBody))
}

func loadLimits(db *gorm.DB) (jsonBody, wsFrame, requestBody int) {
	jsonBody, wsFrame, requestBody = limDefaultBytes, limDefaultBytes, limDefaultGlobalBytes
	get := func(k string) string {
		var s model.Setting
		if err := db.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(limKeyMaxJSONBody); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= limMinBytes && n <= limMaxBytes {
			jsonBody = n
		}
	}
	if v := get(limKeyMaxWSFrame); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= limMinBytes && n <= limMaxBytes {
			wsFrame = n
		}
	}
	if v := get(limKeyMaxRequestBody); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= limMinGlobalBytes && n <= limMaxGlobalBytes {
			requestBody = n
		}
	}
	return
}

// limitJSONBody wraps the request body in http.MaxBytesReader so a
// gin ShouldBindJSON / ShouldBindWith call can't be tricked into
// buffering an unbounded payload. Returns true when the request was
// already aborted (caller should just return).
//
// Errors from MaxBytesReader surface as a 413 from gin's binder; we
// rewrite to a structured shape so the frontend can show a clear
// message instead of the binder's stringly-typed error.
func limitJSONBody(c *gin.Context, max int64) {
	if max <= 0 {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
}

func abortPayloadTooLarge(c *gin.Context, max int64) {
	apiErrWithParams(c, http.StatusRequestEntityTooLarge,
		"common.payload_too_large",
		"request body exceeds the configured byte limit; use the streaming upload endpoint for large files",
		map[string]any{"maxBytes": max})
}

// ----- HTTP settings handlers (admin-only) -----

type limitsDTO struct {
	MaxJSONBodyBytes    int `json:"maxJsonBodyBytes"`
	MaxWSFrameBytes     int `json:"maxWsFrameBytes"`
	MaxRequestBodyBytes int `json:"maxRequestBodyBytes"`
}

func (h *SettingsHandler) GetLimits(c *gin.Context) {
	jb, ws, rb := loadLimits(h.DB)
	c.JSON(http.StatusOK, limitsDTO{MaxJSONBodyBytes: jb, MaxWSFrameBytes: ws, MaxRequestBodyBytes: rb})
}

func (h *SettingsHandler) SetLimits(c *gin.Context) {
	var b limitsDTO
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.MaxJSONBodyBytes < limMinBytes || b.MaxJSONBodyBytes > limMaxBytes {
		apiErr(c, http.StatusBadRequest, "settings.max_json_body_range", "maxJsonBodyBytes must be 1MiB..128MiB")
		return
	}
	if b.MaxWSFrameBytes < limMinBytes || b.MaxWSFrameBytes > limMaxBytes {
		apiErr(c, http.StatusBadRequest, "settings.max_ws_frame_range", "maxWsFrameBytes must be 1MiB..128MiB")
		return
	}
	if b.MaxRequestBodyBytes < limMinGlobalBytes || b.MaxRequestBodyBytes > limMaxGlobalBytes {
		apiErr(c, http.StatusBadRequest, "settings.max_request_body_range", "maxRequestBodyBytes must be 1KiB..4MiB")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&model.Setting{Key: limKeyMaxJSONBody, Value: strconv.Itoa(b.MaxJSONBodyBytes)}).Error; err != nil {
			return err
		}
		if err := tx.Save(&model.Setting{Key: limKeyMaxWSFrame, Value: strconv.Itoa(b.MaxWSFrameBytes)}).Error; err != nil {
			return err
		}
		return tx.Save(&model.Setting{Key: limKeyMaxRequestBody, Value: strconv.Itoa(b.MaxRequestBodyBytes)}).Error
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	if h.Limits != nil {
		h.Limits.Apply(b.MaxJSONBodyBytes, b.MaxWSFrameBytes, b.MaxRequestBodyBytes)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BodyLimitMiddleware caps every incoming request body at
// LiveLimits.MaxRequestBody. Once http.MaxBytesReader wraps the body
// the cap is sticky (re-wrapping with a bigger ceiling still hits the
// inner reader's limit), so handlers that legitimately need bigger
// payloads — fs/write (up to MaxJSONBody) and the chunked file upload
// path — are listed here as exemptions and apply their own caps.
//
// Defense-in-depth: even on the exempt routes the daemon side and
// handler-side caps are still in force; this just skips the global
// 128 KiB blanket so those handlers can do their own framing.
func BodyLimitMiddleware(limits *LiveLimits) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limits == nil || c.Request.Body == nil {
			c.Next()
			return
		}
		p := c.Request.URL.Path
		// /fs/write — 16 MiB JSON window managed by handler
		// /files/upload — multi-GiB streaming chunks; daemon enforces
		// /favicon, /hibernation/icon — 32-64 KiB; under the global
		//   default anyway, but multipart parsing could miscount, so
		//   leave room
		if strings.HasSuffix(p, "/fs/write") ||
			strings.Contains(p, "/files/upload") ||
			strings.HasSuffix(p, "/brand/favicon") ||
			strings.HasSuffix(p, "/hibernation/icon") {
			c.Next()
			return
		}
		max := limits.MaxRequestBody()
		if max <= 0 {
			c.Next()
			return
		}
		// Cheap pre-check from Content-Length: short-circuit before
		// any body bytes are buffered. Returns a clean 413 the SPA
		// can recognise (instead of letting MaxBytesReader trip
		// later as a generic "invalid body" 400).
		if c.Request.ContentLength > max {
			c.Header("Connection", "close")
			apiErrWithParams(c, http.StatusRequestEntityTooLarge,
				"common.request_too_large",
				"request body exceeds the configured byte limit",
				map[string]any{"maxBytes": max})
			return
		}
		// Wrap the body so chunked transfers (no Content-Length) and
		// any client that lies about Content-Length still get capped.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		c.Next()
	}
}
