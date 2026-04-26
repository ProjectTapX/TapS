// Daemon error responses use the same {error:<code>, message:<english>,
// params?:{}} shape as the panel's apiErr helper so the daemon's panel
// proxy (internal/daemonclient on the panel) can re-emit them through
// the panel's apiErr unchanged. Pre-Phase-3 the daemon emitted
// http.Error()'s text/plain "rate_limited\n" / "missing path\n" / etc.,
// which the panel showed verbatim — no i18n possible.
//
// Codes are dotted lower_snake_case, prefixed by domain (daemon, fs,
// auth). Keep stable; rename = breaking change for any client that
// matches on them (frontend i18n keys, dashboards, alerts).
package rpc

import (
	"encoding/json"
	"net/http"
)

type apiErrorResp struct {
	Code    string         `json:"error"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

// writeJSONError emits the dual-format error response. We set
// Content-Type explicitly because http.Error() sets text/plain and we
// want JSON; using ResponseWriter directly avoids that surprise.
func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	writeJSONErrorWithParams(w, status, code, msg, nil)
}

func writeJSONErrorWithParams(w http.ResponseWriter, status int, code, msg string, params map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorResp{Code: code, Message: msg, Params: params})
}
