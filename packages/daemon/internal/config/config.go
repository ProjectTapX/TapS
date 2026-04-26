package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Addr          string
	DataDir       string
	Token         string
	RequireDocker bool // refuse to spawn non-docker instances

	// Per-IP throttle for failed token validations on the daemon's
	// HTTP and WebSocket endpoints. Default: 10 failures/min triggers
	// a 10-minute IP ban (per H-2). Both knobs are env-tunable
	// (TAPS_DAEMON_RL_THRESHOLD, TAPS_DAEMON_RL_BAN_MINUTES) so an
	// operator can dial it up/down without rebuilding.
	RateLimitThreshold int
	RateLimitBanMin    int

	// Per-WebSocket-frame ceiling (bytes). Anything bigger than this
	// — typically a giant fs.write payload — is rejected before the
	// daemon allocates the buffer, so a single hostile frame can't
	// OOM the process. Default 16 MiB matches H-7 in the audit; range
	// 1 MiB .. 128 MiB.
	MaxWSFrameBytes int64

	// audit-2026-04-25 H2: per-session cap on concurrently-running
	// dispatch goroutines. handleWS used to `go s.dispatch(...)` for
	// every inbound message with no upper bound — a hostile or buggy
	// panel client could fork-bomb the daemon with one round-trip's
	// worth of messages. We acquire a slot from a buffered channel
	// before launching dispatch; if no slot is available we reply
	// `daemon.busy` immediately so the caller can back off. Default
	// 8192 is generous (way above any real interactive workload),
	// range [1, 65536]. Tunable per-host because a small VPS daemon
	// and a 64-core build node want very different ceilings.
	WSDispatchConcurrency int

	// audit-2026-04-25 MED5: HTTP server slow-loris defences. All
	// four are seconds. Apply to the daemon's TLS listener; gorilla
	// websocket's Hijack() takes the connection out from under
	// http.Server before any of these fire, so terminal/RPC stays
	// alive past WriteTimeout. Range 1..3600s each, defaults
	// 10/60/120/120.
	HTTPReadHeaderTimeoutSec int
	HTTPReadTimeoutSec       int
	HTTPWriteTimeoutSec      int
	HTTPIdleTimeoutSec       int
}

const (
	daemonMaxWSFrameDefault = 16 << 20
	daemonMaxWSFrameMin     = 1 << 20
	daemonMaxWSFrameMax     = 128 << 20

	daemonWSDispatchDefault = 8192
	daemonWSDispatchMin     = 1
	daemonWSDispatchMax     = 65536

	daemonHTTPReadHeaderTimeoutDefault = 10
	daemonHTTPReadTimeoutDefault       = 60
	daemonHTTPWriteTimeoutDefault      = 120
	daemonHTTPIdleTimeoutDefault       = 120
	daemonHTTPTimeoutMin               = 1
	daemonHTTPTimeoutMax               = 3600
)

// fileConfig mirrors Config but every field is a pointer so we can
// tell "admin set this to zero" apart from "admin omitted the key".
// Fields read from <DataDir>/config.json take precedence over env
// vars and the built-in defaults — the file is the most explicit
// statement of operator intent.
type fileConfig struct {
	Addr                     *string `json:"addr,omitempty"`
	RequireDocker            *bool   `json:"requireDocker,omitempty"`
	RateLimitThreshold       *int    `json:"rateLimitThreshold,omitempty"`
	RateLimitBanMin          *int    `json:"rateLimitBanMinutes,omitempty"`
	MaxWSFrameBytes          *int64  `json:"maxWsFrameBytes,omitempty"`
	WSDispatchConcurrency    *int    `json:"wsDispatchConcurrency,omitempty"`
	HTTPReadHeaderTimeoutSec *int    `json:"httpReadHeaderTimeoutSec,omitempty"`
	HTTPReadTimeoutSec       *int    `json:"httpReadTimeoutSec,omitempty"`
	HTTPWriteTimeoutSec      *int    `json:"httpWriteTimeoutSec,omitempty"`
	HTTPIdleTimeoutSec       *int    `json:"httpIdleTimeoutSec,omitempty"`
}

// loadFileConfig reads <dataDir>/config.json if it exists. A missing
// file or any I/O error is logged and treated as "no overrides", so
// a corrupt file can't keep the daemon from starting — env + defaults
// still apply.
func loadFileConfig(dataDir string) fileConfig {
	var fc fileConfig
	path := filepath.Join(dataDir, "config.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: read %s: %v (ignored, falling back to env)", path, err)
		}
		return fc
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		log.Printf("config: parse %s: %v (ignored, falling back to env)", path, err)
		return fc
	}
	log.Printf("config: applied overrides from %s", path)
	return fc
}

func Load() *Config {
	dataDir := envOr("TAPS_DAEMON_DATA", "./data")
	_ = os.MkdirAll(dataDir, 0o755)

	// Read overrides first; envs and defaults plug the holes.
	fc := loadFileConfig(dataDir)

	c := &Config{
		DataDir:                  dataDir,
		Addr:                     pickStr(fc.Addr, envOr("TAPS_DAEMON_ADDR", ":24445")),
		RequireDocker:            pickBool(fc.RequireDocker, envBool("TAPS_REQUIRE_DOCKER", true)),
		RateLimitThreshold:       pickIntClamped(fc.RateLimitThreshold, envInt("TAPS_DAEMON_RL_THRESHOLD", 10, 1, 1000), 1, 1000),
		RateLimitBanMin:          pickIntClamped(fc.RateLimitBanMin, envInt("TAPS_DAEMON_RL_BAN_MINUTES", 10, 1, 1440), 1, 1440),
		MaxWSFrameBytes:          pickInt64Clamped(fc.MaxWSFrameBytes, int64(envInt("TAPS_DAEMON_MAX_WS_FRAME_BYTES", daemonMaxWSFrameDefault, daemonMaxWSFrameMin, daemonMaxWSFrameMax)), daemonMaxWSFrameMin, daemonMaxWSFrameMax),
		WSDispatchConcurrency:    pickIntClamped(fc.WSDispatchConcurrency, envInt("TAPS_DAEMON_WS_DISPATCH_CONCURRENCY", daemonWSDispatchDefault, daemonWSDispatchMin, daemonWSDispatchMax), daemonWSDispatchMin, daemonWSDispatchMax),
		HTTPReadHeaderTimeoutSec: pickIntClamped(fc.HTTPReadHeaderTimeoutSec, envInt("TAPS_DAEMON_HTTP_READ_HEADER_TIMEOUT_SEC", daemonHTTPReadHeaderTimeoutDefault, daemonHTTPTimeoutMin, daemonHTTPTimeoutMax), daemonHTTPTimeoutMin, daemonHTTPTimeoutMax),
		HTTPReadTimeoutSec:       pickIntClamped(fc.HTTPReadTimeoutSec, envInt("TAPS_DAEMON_HTTP_READ_TIMEOUT_SEC", daemonHTTPReadTimeoutDefault, daemonHTTPTimeoutMin, daemonHTTPTimeoutMax), daemonHTTPTimeoutMin, daemonHTTPTimeoutMax),
		HTTPWriteTimeoutSec:      pickIntClamped(fc.HTTPWriteTimeoutSec, envInt("TAPS_DAEMON_HTTP_WRITE_TIMEOUT_SEC", daemonHTTPWriteTimeoutDefault, daemonHTTPTimeoutMin, daemonHTTPTimeoutMax), daemonHTTPTimeoutMin, daemonHTTPTimeoutMax),
		HTTPIdleTimeoutSec:       pickIntClamped(fc.HTTPIdleTimeoutSec, envInt("TAPS_DAEMON_HTTP_IDLE_TIMEOUT_SEC", daemonHTTPIdleTimeoutDefault, daemonHTTPTimeoutMin, daemonHTTPTimeoutMax), daemonHTTPTimeoutMin, daemonHTTPTimeoutMax),
	}
	c.Token = loadOrCreateToken(filepath.Join(dataDir, "token"))
	// Always (re)write the template file so it stays in sync with
	// whatever fields the current binary supports. Admins copy this
	// to config.json and edit; the template itself is never parsed.
	writeConfigTemplate(dataDir)
	return c
}

// writeConfigTemplate stamps a fresh config.json.template alongside
// the live data files. Idempotent: if the on-disk content already
// matches what we'd write we skip the rewrite to avoid bumping the
// mtime needlessly. The template carries every supported field at
// its built-in default so admins don't have to dig through docs.
func writeConfigTemplate(dataDir string) {
	tmpl := []byte(`{
  "_comment_addr":               "Listen address. Default :24445. Set to 127.0.0.1:24445 to bind only loopback when fronted by a reverse proxy.",
  "addr":                        ":24445",

  "_comment_requireDocker":      "Refuse to start non-docker instances. Set false only on hosts where Docker is intentionally not available.",
  "requireDocker":               true,

  "_comment_rateLimitThreshold": "Failed token validations per minute per IP that trigger a ban. Range 1..1000, default 10.",
  "rateLimitThreshold":          10,

  "_comment_rateLimitBanMinutes":"How long an offending IP stays banned. Range 1..1440 (24h), default 10.",
  "rateLimitBanMinutes":         10,

  "_comment_maxWsFrameBytes":    "Per-WS-frame ceiling (bytes). Anything bigger is rejected before allocating a buffer. Range 1MiB..128MiB, default 16MiB (16777216).",
  "maxWsFrameBytes":             16777216,

  "_comment_wsDispatchConcurrency": "Per-session cap on concurrently-running dispatch goroutines. Excess inbound messages get a daemon.busy reply instead of forking goroutines. Range 1..65536, default 8192.",
  "wsDispatchConcurrency":       8192,

  "_comment_httpTimeouts":       "HTTP server slow-loris defences. WebSocket upgrades hijack the connection so terminal/RPC are unaffected. Each value is seconds, range 1..3600.",
  "httpReadHeaderTimeoutSec":    10,
  "httpReadTimeoutSec":          60,
  "httpWriteTimeoutSec":         120,
  "httpIdleTimeoutSec":          120
}
`)
	path := filepath.Join(dataDir, "config.json.template")
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, tmpl) {
		return // already up to date
	}
	if err := os.WriteFile(path, tmpl, 0o644); err != nil {
		log.Printf("config: write template %s: %v", path, err)
	}
}

func pickStr(file *string, fallback string) string {
	if file != nil && *file != "" {
		return *file
	}
	return fallback
}

func pickBool(file *bool, fallback bool) bool {
	if file != nil {
		return *file
	}
	return fallback
}

// pickIntClamped honors the file value when in [min,max], otherwise
// it falls back. Out-of-range file values silently reset to the env-
// or default-resolved fallback so a typo doesn't ban the whole knob.
func pickIntClamped(file *int, fallback, min, max int) int {
	if file != nil && *file >= min && *file <= max {
		return *file
	}
	return fallback
}

func pickInt64Clamped(file *int64, fallback, min, max int64) int64 {
	if file != nil && *file >= min && *file <= max {
		return *file
	}
	return fallback
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	switch v {
	case "0", "false", "False", "FALSE", "no", "off":
		return false
	}
	return true
}

func envInt(k string, def, min, max int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return def
	}
	return n
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadOrCreateToken(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 16 {
		return string(b)
	}
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	tok := hex.EncodeToString(buf)
	_ = os.WriteFile(path, []byte(tok), 0o600)
	return tok
}
