package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/taps/panel/internal/daemonclient"
)

func jsonUnmarshal(b []byte, v any) error  { return json.Unmarshal(b, v) }
func bytesNewReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// FilesProxyHandler streams binary uploads/downloads through panel to daemon's HTTP endpoint.
type FilesProxyHandler struct {
	Reg *daemonclient.Registry
	Fs  *FsHandler // re-uses path-scoping rules so users can only touch their own /data/inst-<short>
}

// daemonHTTPCall returns an *http.Client that already enforces the
// daemon's pinned TLS fingerprint. 0 timeout = inherit transport
// default (effectively unlimited — used by file streaming).
func daemonHTTPCall(cli *daemonclient.Client, timeout time.Duration) (*http.Client, error) {
	return cli.HTTPClient(timeout)
}

func (h *FilesProxyHandler) target(c *gin.Context, op string) (*daemonclient.Client, string, bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	cli, ok := h.Reg.Get(uint(id))
	if !ok {
		apiErr(c, http.StatusNotFound, "common.daemon_not_found", "daemon not found")
		return nil, "", false
	}
	// Path-scope check (admin = unrestricted).
	if h.Fs != nil {
		if !h.Fs.guard(c, c.Query("path")) {
			return nil, "", false
		}
	}
	// Forward every incoming query parameter to the daemon, then overlay the
	// daemon's own auth token. This keeps chunked-upload params (seq/total/final)
	// flowing through.
	q := url.Values{}
	for k, vals := range c.Request.URL.Query() {
		if k == "token" {
			continue
		}
		for _, v := range vals {
			q.Add(k, v)
		}
	}
	q.Set("token", cli.Token())
	return cli, "https://" + cli.Address() + "/files/" + op + "?" + q.Encode(), true
}

func (h *FilesProxyHandler) Upload(c *gin.Context) {
	cli, target, ok := h.target(c, "upload")
	if !ok {
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, target, c.Request.Body)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	req.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	if cl := c.GetHeader("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			req.ContentLength = n
		}
	}
	hc, err := daemonHTTPCall(cli, 0)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	resp, err := hc.Do(req)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	defer resp.Body.Close()
	c.Status(resp.StatusCode)
	copySafeDaemonHeaders(c.Writer.Header(), resp.Header)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// UploadInit forwards the chunked-upload init handshake. The browser
// declares total bytes / chunks / target path, the daemon checks
// volume quota and returns an uploadId the subsequent chunk requests
// must echo via ?uploadId=. We do the same path-scope guard here as
// the chunk path so a non-admin can't probe quota on an instance they
// don't own.
func (h *FilesProxyHandler) UploadInit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	cli, ok := h.Reg.Get(uint(id))
	if !ok {
		apiErr(c, http.StatusNotFound, "common.daemon_not_found", "daemon not found")
		return
	}
	// Read body once so we can reuse it for both the guard check
	// (path scope) and the upstream call.
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 64<<10))
	if err != nil {
		apiErr(c, http.StatusBadRequest, "common.read_body_failed", "read body: " + err.Error())
		return
	}
	// Cheap path scoping by re-using FsHandler.guard. Pull `path`
	// off the body so we don't re-decode JSON twice — small struct.
	var probe struct{ Path string `json:"path"` }
	_ = jsonUnmarshal(body, &probe)
	if h.Fs != nil {
		if !h.Fs.guard(c, probe.Path) {
			return
		}
	}
	q := url.Values{}
	q.Set("token", cli.Token())
	target := "https://" + cli.Address() + "/files/upload/init?" + q.Encode()
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, target, bytesNewReader(body))
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	hc, err := daemonHTTPCall(cli, 30*time.Second)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	resp, err := hc.Do(req)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	defer resp.Body.Close()
	c.Status(resp.StatusCode)
	copySafeDaemonHeaders(c.Writer.Header(), resp.Header)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func (h *FilesProxyHandler) Download(c *gin.Context) {
	cli, target, ok := h.target(c, "download")
	if !ok {
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	hc, err := daemonHTTPCall(cli, 0)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	resp, err := hc.Do(req)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	defer resp.Body.Close()
	copySafeDaemonHeaders(c.Writer.Header(), resp.Header)
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}
