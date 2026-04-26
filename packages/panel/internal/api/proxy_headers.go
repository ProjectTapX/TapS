// Daemon-response header passthrough helper used by files-proxy and
// backup-download. Audit-2026-04-24-v3 M1: blanket-copy of every
// daemon header onto the panel-domain reply was a cookie-poisoning /
// CSP-weakening surface (a compromised daemon could plant Set-Cookie
// or override Strict-Transport-Security on the panel origin). The
// allowlist here is the minimal set the SPA actually needs to download
// streamed binary content correctly.
package api

import "net/http"

// daemonProxySafeHeaders is the deny-by-default allowlist applied
// when copying response headers from a daemon HTTP call back to the
// panel client. Add headers here only after verifying they cannot
// be set to attacker-influenced values.
var daemonProxySafeHeaders = map[string]bool{
	"Content-Type":        true, // mime; needed for browser handling
	"Content-Length":      true, // sized download progress bar
	"Content-Disposition": true, // attachment + filename for save dialog
	"Etag":                true, // optional caching support; net/http normalises to Etag
}

// copySafeDaemonHeaders mirrors only the headers in
// daemonProxySafeHeaders from src onto dst. Header keys are compared
// in canonical form (net/http canonicalises on insertion).
func copySafeDaemonHeaders(dst http.Header, src http.Header) {
	for k, vv := range src {
		if !daemonProxySafeHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
