package api

import (
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/shared/tlscert"
)

// daemonFpRe enforces the canonical SHA-256 colon-hex format produced
// by tlscert.FingerprintFromDER: 32 hex pairs separated by 31 colons,
// case-insensitive. Anything else is either a typo or junk and would
// silently brick the daemonclient TLS dialer.
var daemonFpRe = regexp.MustCompile(`^([0-9a-fA-F]{2}:){31}[0-9a-fA-F]{2}$`)

func validDaemonFingerprint(fp string) bool { return daemonFpRe.MatchString(fp) }

type DaemonHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

type daemonView struct {
	daemonDTO
	Connected     bool   `json:"connected"`
	OS            string `json:"os,omitempty"`
	Arch          string `json:"arch,omitempty"`
	Version       string `json:"daemonVersion,omitempty"`
	RequireDocker bool   `json:"requireDocker"`
	DockerReady   bool   `json:"dockerReady"`
}

// toView builds the response DTO. By going through daemonDTO instead
// of embedding model.Daemon directly, any new sensitive field added
// to model.Daemon (Token, future secrets, etc.) is silently dropped
// at this boundary unless dto.go is updated explicitly.
func (h *DaemonHandler) toView(d model.Daemon) daemonView {
	v := daemonView{daemonDTO: daemonToDTO(&d)}
	if c, ok := h.Reg.Get(d.ID); ok {
		v.Connected = c.Connected()
		w := c.Welcome()
		v.OS, v.Arch, v.Version = w.OS, w.Arch, w.Version
		v.RequireDocker = w.RequireDocker
		v.DockerReady = w.DockerReady
	}
	return v
}

func (h *DaemonHandler) List(c *gin.Context) {
	var ds []model.Daemon
	if err := h.DB.Order("id asc").Find(&ds).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	out := make([]daemonView, 0, len(ds))
	for _, d := range ds {
		out = append(out, h.toView(d))
	}
	c.JSON(http.StatusOK, out)
}

type createDaemonReq struct {
	Name            string `json:"name" binding:"required"`
	Address         string `json:"address" binding:"required"` // host:port
	Token           string `json:"token" binding:"required"`
	DisplayHost     string `json:"displayHost"`
	PortMin         int    `json:"portMin"`
	PortMax         int    `json:"portMax"`
	// CertFingerprint is the SHA-256 colon-hex of the daemon's TLS
	// cert that the operator confirmed via the TOFU probe. Required —
	// without it the daemonclient TLS dialer refuses to connect.
	CertFingerprint string `json:"certFingerprint" binding:"required"`
}

func (h *DaemonHandler) Create(c *gin.Context) {
	var req createDaemonReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if !validDaemonFingerprint(req.CertFingerprint) {
		apiErr(c, http.StatusBadRequest, "daemon.bad_fingerprint", "invalid certFingerprint format (expected SHA-256 colon-hex, e.g. aa:bb:...)")
		return
	}
	d := model.Daemon{
		Name:            req.Name,
		Address:         req.Address,
		Token:           req.Token,
		DisplayHost:     req.DisplayHost,
		PortMin:         req.PortMin,
		PortMax:         req.PortMax,
		CertFingerprint: req.CertFingerprint,
	}
	if err := h.DB.Create(&d).Error; err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	h.Reg.Add(d)
	c.JSON(http.StatusOK, h.toView(d))
}

type updateDaemonReq struct {
	Name            string  `json:"name"`
	Address         string  `json:"address"`
	Token           string  `json:"token"`
	DisplayHost     *string `json:"displayHost"`
	PortMin         *int    `json:"portMin"`
	PortMax         *int    `json:"portMax"`
	CertFingerprint *string `json:"certFingerprint"`
}

func (h *DaemonHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var d model.Daemon
	if err := h.DB.First(&d, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	var req updateDaemonReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if req.Name != "" {
		d.Name = req.Name
	}
	if req.Address != "" {
		d.Address = req.Address
	}
	if req.Token != "" {
		d.Token = req.Token
	}
	if req.DisplayHost != nil {
		d.DisplayHost = *req.DisplayHost
	}
	if req.PortMin != nil {
		d.PortMin = *req.PortMin
	}
	if req.PortMax != nil {
		d.PortMax = *req.PortMax
	}
	if req.CertFingerprint != nil {
		if !validDaemonFingerprint(*req.CertFingerprint) {
			apiErr(c, http.StatusBadRequest, "daemon.bad_fingerprint", "invalid certFingerprint format (expected SHA-256 colon-hex, e.g. aa:bb:...)")
			return
		}
		d.CertFingerprint = *req.CertFingerprint
	}
	if err := h.DB.Save(&d).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	h.Reg.Add(d) // re-dial
	c.JSON(http.StatusOK, h.toView(d))
}

func (h *DaemonHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	// audit-2026-04-25-v2 MED15: pull the daemon from the in-memory
	// registry first; this can't be rolled back from the DB
	// transaction below but is idempotent — if the SQLite write fails
	// the next Reg.LoadAll on panel restart will re-add the row, and a
	// connect hook re-registers it. Doing it before the txn keeps the
	// happy path simple (no live RPC against a row about to vanish).
	h.Reg.Remove(uint(id))
	// One transaction wraps the daemon row and every cascade so a
	// mid-flight crash leaves no orphans:
	//   - InstancePermission rows referencing this daemon
	//   - Task rows scheduled against this daemon
	//   - NodeGroupMember rows that put this daemon in a group
	//     (previously missed; left dangling group members that
	//     pickFromGroup would silently skip but that the admin UI
	//     would still display in the membership count).
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.Daemon{}, id).Error; err != nil {
			return err
		}
		if err := tx.Where("daemon_id = ?", id).Delete(&model.InstancePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("daemon_id = ?", id).Delete(&model.Task{}).Error; err != nil {
			return err
		}
		if err := tx.Where("daemon_id = ?", id).Delete(&model.NodeGroupMember{}).Error; err != nil {
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

// PublicView returns the subset of daemon metadata safe to share with
// non-admin users (just the name + display host, used to render game
// addresses on the instance detail page). Authenticated only — no
// per-instance permission check, since exposing a hostname/port is harmless.
func (h *DaemonHandler) PublicView(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var d model.Daemon
	if err := h.DB.First(&d, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	c.JSON(http.StatusOK, daemonToPublicDTO(&d))
}

// ProbeFingerprint dials the daemon over TLS without verifying the
// chain (it's self-signed) and returns the SHA-256 fingerprint of the
// cert it presented, plus the cert PEM. The "add daemon" wizard uses
// this to show the operator what they're about to trust before
// persisting it. Admin-only.
type probeReq struct {
	Address string `json:"address" binding:"required"` // host:port
}
type probeResp struct {
	Fingerprint string `json:"fingerprint"`
	CertPEM     string `json:"certPem"`
}

func (h *DaemonHandler) ProbeFingerprint(c *gin.Context) {
	var req probeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	fp, pemBytes, err := probeDaemonCert(req.Address, 8*time.Second)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, probeResp{Fingerprint: fp, CertPEM: string(pemBytes)})
}

// RefetchFingerprint is the same probe but for an existing daemon row.
// Used by the "rotate / re-accept" button in the daemon edit page when
// the operator regenerates the cert on the daemon side. Returns the
// freshly-observed fingerprint without persisting it; the operator
// must explicitly confirm via PUT /api/daemons/:id with the new value.
func (h *DaemonHandler) RefetchFingerprint(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var d model.Daemon
	if err := h.DB.First(&d, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	fp, pemBytes, err := probeDaemonCert(d.Address, 8*time.Second)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"fingerprint":     fp,
		"currentPinned":   d.CertFingerprint,
		"matches":         tlsFPEqual(fp, d.CertFingerprint),
		"certPem":         string(pemBytes),
	})
}

// probeDaemonCert dials :addr over TLS without chain verification and
// returns the SHA-256 fingerprint + PEM of the leaf cert. Helper kept
// here (rather than in tlscert) so it can use the panel's net/http
// stack and timeout shape.
func probeDaemonCert(addr string, timeout time.Duration) (string, []byte, error) {
	d := &net.Dialer{Timeout: timeout}
	rawConn, err := d.Dial("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	conn := tls.Client(rawConn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := conn.Handshake(); err != nil {
		return "", nil, fmt.Errorf("tls handshake: %w", err)
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return "", nil, errors.New("daemon presented no certificate")
	}
	leaf := state.PeerCertificates[0]
	fp := tlscert.FingerprintFromDER(leaf.Raw)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})

	// Cross-check by hitting /cert over the same TLS link — guards
	// against a hypothetical mid-handshake swap and surfaces a clear
	// "daemon reachable, talking taps protocol" signal in the wizard.
	hc := &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}},
	}
	resp, herr := hc.Get("https://" + addr + "/cert")
	if herr == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if servedFP, ferr := tlscert.FingerprintFromPEM(body); ferr == nil && !tlsFPEqual(servedFP, fp) {
			return "", nil, fmt.Errorf("daemon /cert (%s) disagrees with handshake (%s)", servedFP, fp)
		}
	}
	return fp, pemBytes, nil
}

func tlsFPEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
