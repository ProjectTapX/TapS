package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/access"
	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/config"
	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// makeWSUpgrader builds the gorilla Upgrader with a CheckOrigin pinned
// to the panel's configured publicURL (audit M3). publicURL must be
// set; if it isn't we refuse the upgrade entirely so the operator
// notices and configures it before exposing the terminal cross-domain.
// Non-browser clients (curl, custom WS clients) typically send no
// Origin header and are accepted unconditionally — CORS / WS Origin
// checks are a browser defense, not a server-to-server one.
func makeWSUpgrader(db *gorm.DB) (websocket.Upgrader, string) {
	pub := strings.TrimRight(LoadPanelPublicURL(db), "/")
	return websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return strings.TrimRight(strings.ToLower(origin), "/") == strings.ToLower(pub)
	}}, pub
}

type TerminalHandler struct {
	Cfg    *config.Config
	Reg    *daemonclient.Registry
	DB     *gorm.DB
	Limits *LiveLimits // WS read-frame cap (RAM-DoS guard)
}

// Browser sends frames as plain text:  user keystrokes go to instance stdin.
// Server pushes the daemon's instance.output payloads as text.

type wsInbound struct {
	Type string `json:"type"` // "input" | "resize"
	Data string `json:"data"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (h *TerminalHandler) Handle(c *gin.Context) {
	tok := c.Query("token")
	if tok == "" {
		apiErr(c, http.StatusUnauthorized, "auth.missing_token", "missing token")
		return
	}
	// Same revocation check as queryAuth: a JWT whose iat is older
	// than the user's TokensInvalidBefore must not be allowed to open
	// a fresh WS — the periodic heartbeat below handles already-open
	// sessions.
	claims, _, ok := auth.ValidateRevocableJWT(c, h.Cfg.JWTSecret, h.DB, tok)
	if !ok {
		return
	}
	daemonID, _ := strconv.Atoi(c.Param("id"))
	uuid := c.Param("uuid")
	// Read access (PermView or anything stronger) is the bar to OPEN
	// the terminal — read-only viewers can stream output but their
	// stdin is silently dropped below. Write access requires
	// PermTerminal (or PermControl as a superset, since PermControl
	// already grants stdin via the REST /input endpoint).
	if !access.HasPerm(h.DB, claims.UserID, claims.Role, uint(daemonID), uuid, model.PermView) {
		apiErr(c, http.StatusForbidden, "common.no_view_access", "no view access")
		return
	}
	canWrite := access.HasPerm(h.DB, claims.UserID, claims.Role, uint(daemonID), uuid, model.PermTerminal) ||
		access.HasPerm(h.DB, claims.UserID, claims.Role, uint(daemonID), uuid, model.PermControl)

	cli, ok := h.Reg.Get(uint(daemonID))
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusServiceUnavailable, "common.daemon_not_connected", "daemon not connected")
		return
	}

	// Audit M3: refuse the upgrade when admin hasn't configured a
	// publicURL — without it we can't pin Origin meaningfully and the
	// terminal would be open to any browser tab that has a JWT.
	upgrader, pub := makeWSUpgrader(h.DB)
	if pub == "" {
		apiErr(c, http.StatusServiceUnavailable, "settings.public_url_required",
			"system.publicUrl must be configured before opening terminal sessions")
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("terminal upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Cap inbound frame size: a terminal user only ever sends short
	// keystrokes / resize events, so anything beyond a few KiB is
	// either malformed or hostile. Use the admin-tuned cap (default
	// 16 MiB, way more than any reasonable interactive session) so we
	// can't be RAM-DoS'd by a giant input frame.
	if h.Limits != nil {
		if max := h.Limits.MaxWSFrame(); max > 0 {
			conn.SetReadLimit(max)
		}
	}
	// Audit M4: pong handler + read deadline. Without it ReadMessage
	// blocks forever on a half-open TCP, leaking goroutines until fd
	// exhaustion. The 30s ping ticker below stays as-is; client must
	// pong within wsReadDeadlineSec or we tear down.
	deadlineSec, ratePerSec, burst := loadTerminalLimits(h.DB)
	deadline := time.Duration(deadlineSec) * time.Second
	_ = conn.SetReadDeadline(time.Now().Add(deadline))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(deadline))
	})
	// Audit M5: per-connection token bucket on inbound frames so a
	// runaway / hostile WS client can't fork-bomb the daemon RPC loop.
	// Frames over the rate are silently dropped (not closed) — a
	// real human at 200 fps + 50 burst won't notice; a flood gets
	// throttled to the configured ceiling.
	inputBucket := newTokenBucket(float64(ratePerSec), float64(burst))

	var writeM sync.Mutex
	writeText := func(s string) error {
		writeM.Lock()
		defer writeM.Unlock()
		return conn.WriteMessage(websocket.TextMessage, []byte(s))
	}

	// Replay the daemon-side scrollback first so a fresh browser tab (or a
	// reload that lost the in-memory cache) immediately sees recent output.
	// There is a small race window between this snapshot and the
	// subscription below where bytes could be missed; the daemon side could
	// be tightened later, but for an interactive console the gap is small
	// enough not to matter in practice.
	if raw, err := cli.Call(context.Background(), protocol.ActionInstanceOutputHistory,
		protocol.InstanceTarget{UUID: uuid}); err == nil {
		var hr protocol.InstanceOutputHistoryResp
		if json.Unmarshal(raw, &hr) == nil && hr.History != "" {
			_ = writeText(hr.History)
		}
	}

	unsub := cli.Subscribe(uuid, func(action string, payload json.RawMessage) {
		switch action {
		case protocol.ActionInstanceOutput:
			var ev protocol.InstanceOutputEvent
			if err := json.Unmarshal(payload, &ev); err == nil {
				_ = writeText(ev.Data)
			}
		case protocol.ActionInstanceStatus:
			_ = writeText("\r\n[[33mstatus[0m] " + string(payload) + "\r\n")
		}
	})
	defer unsub()

	// ping ticker
	pingDone := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-t.C:
				writeM.Lock()
				_ = conn.WriteMessage(websocket.PingMessage, nil)
				writeM.Unlock()
			}
		}
	}()
	defer close(pingDone)

	// Live revocation heartbeat. WebSockets bypass the auth middleware
	// after the initial query-token check, so without this they'd
	// stay valid until the user closes the tab. Every wsHeartbeat
	// minutes we re-read the user's TokensInvalidBefore — if the
	// admin (or the user themselves via password change) bumped it
	// past the token's iat, drop the connection.
	revokeDone := make(chan struct{})
	go func() {
		_, hb := LoadAuthTimings(h.DB)
		if hb <= 0 {
			return
		}
		t := time.NewTicker(hb)
		defer t.Stop()
		for {
			select {
			case <-revokeDone:
				return
			case <-t.C:
				var u model.User
				if err := h.DB.Select("id", "tokens_invalid_before").First(&u, claims.UserID).Error; err != nil {
					_ = conn.Close()
					return
				}
				if !u.TokensInvalidBefore.IsZero() && claims.IssuedAt != nil && !claims.IssuedAt.Time.After(u.TokensInvalidBefore) {
					_ = writeText("\r\n[[31mtoken revoked — please re-login[0m]\r\n")
					_ = conn.Close()
					return
				}
			}
		}
	}()
	defer close(revokeDone)

	for {
		mt, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage {
			continue
		}
		// View-only users may receive output but cannot send anything
		// upstream — silently drop every inbound frame. Resize is
		// also gated since it can confuse other observers' panes.
		if !canWrite {
			continue
		}
		// audit M5: cheap per-connection cap on inbound frames.
		// Drop silently when over budget (closing would hurt a real
		// user with sticky-key autorepeat; 200 fps default is far
		// above any human typing rate).
		if !inputBucket.Take() {
			continue
		}
		var msg wsInbound
		if err := json.Unmarshal(raw, &msg); err != nil {
			// fall back to raw input
			_, _ = cli.Call(context.Background(), protocol.ActionInstanceInput,
				protocol.InstanceInputReq{UUID: uuid, Data: string(raw)})
			continue
		}
		if msg.Type == "input" {
			_, _ = cli.Call(context.Background(), protocol.ActionInstanceInput,
				protocol.InstanceInputReq{UUID: uuid, Data: msg.Data})
		} else if msg.Type == "resize" {
			_, _ = cli.Call(context.Background(), protocol.ActionInstanceResize,
				protocol.InstanceResizeReq{UUID: uuid, Cols: msg.Cols, Rows: msg.Rows})
		}
	}
}
