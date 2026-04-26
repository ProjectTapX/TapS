// Admin CRUD for OIDC providers. Mounts at /api/admin/sso/providers/*.
// All endpoints require role=admin + scope=admin via the standard
// authed middleware chain.
//
// Secret handling (per Day 2 design):
//   - GET    list / single → secret never returned, hasSecret bool only
//   - POST   create        → secret required
//   - PUT    update        → secret optional: empty = keep, non-empty = replace
//   - DELETE                → cascades sso_identities for this provider
//   - POST   /test         → admin-only probe; returns discovered endpoints
package api

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
	"github.com/taps/panel/internal/secrets"
	"github.com/taps/panel/internal/sso"
)

type SSOAdminHandler struct {
	DB     *gorm.DB
	Mgr    *sso.Manager
	Cipher *secrets.Cipher
}

// providerNameRe restricts the URL slug used in callback paths to a
// safe character set: lowercase letters, digits, hyphen, underscore.
// Keeps the slug usable in path segments without escaping.
var providerNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

type providerView struct {
	ID                   uint        `json:"id"`
	Name                 string      `json:"name"`
	DisplayName          string      `json:"displayName"`
	Enabled              bool        `json:"enabled"`
	Issuer               string      `json:"issuer"`
	ClientID             string      `json:"clientId"`
	HasSecret            bool        `json:"hasSecret"`
	Scopes               string      `json:"scopes"`
	AutoCreate           bool        `json:"autoCreate"`
	DefaultRole          model.Role  `json:"defaultRole"`
	EmailDomains         string      `json:"emailDomains"`
	TrustUnverifiedEmail bool        `json:"trustUnverifiedEmail"`
	CallbackURL          string      `json:"callbackUrl"`
	CreatedAt            time.Time   `json:"createdAt"`
	UpdatedAt            time.Time   `json:"updatedAt"`
}

func (h *SSOAdminHandler) toView(p *model.SSOProvider) providerView {
	return providerView{
		ID:                   p.ID,
		Name:                 p.Name,
		DisplayName:          p.DisplayName,
		Enabled:              p.Enabled,
		Issuer:               p.Issuer,
		ClientID:             p.ClientID,
		HasSecret:            p.ClientSecretEnc != "",
		Scopes:               p.Scopes,
		AutoCreate:           p.AutoCreate,
		DefaultRole:          p.DefaultRole,
		EmailDomains:         p.EmailDomains,
		TrustUnverifiedEmail: p.TrustUnverifiedEmail,
		CallbackURL:          h.Mgr.CallbackURL(p.Name),
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
	}
}

func (h *SSOAdminHandler) List(c *gin.Context) {
	var rows []model.SSOProvider
	if err := h.DB.Order("id asc").Find(&rows).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	out := make([]providerView, 0, len(rows))
	for i := range rows {
		out = append(out, h.toView(&rows[i]))
	}
	c.JSON(http.StatusOK, out)
}

func (h *SSOAdminHandler) Get(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var p model.SSOProvider
	if err := h.DB.First(&p, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	c.JSON(http.StatusOK, h.toView(&p))
}

type providerCreateReq struct {
	Name                 string     `json:"name" binding:"required"`
	DisplayName          string     `json:"displayName" binding:"required"`
	Enabled              *bool      `json:"enabled"`
	Issuer               string     `json:"issuer" binding:"required"`
	ClientID             string     `json:"clientId" binding:"required"`
	ClientSecret         string     `json:"clientSecret" binding:"required"`
	Scopes               string     `json:"scopes"`
	AutoCreate           bool       `json:"autoCreate"`
	DefaultRole          model.Role `json:"defaultRole"`
	EmailDomains         string     `json:"emailDomains"`
	TrustUnverifiedEmail bool       `json:"trustUnverifiedEmail"`
}

func (h *SSOAdminHandler) Create(c *gin.Context) {
	var req providerCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if err := validateProviderInput(req.Name, req.Issuer, req.DefaultRole, req.EmailDomains); err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	if req.DefaultRole == "" {
		req.DefaultRole = model.RoleUser
	}
	if req.Scopes == "" {
		req.Scopes = "openid profile email"
	}
	enc, err := h.Cipher.Encrypt(req.ClientSecret)
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.encrypt_secret", "encrypt secret: " + err.Error())
		return
	}
	p := &model.SSOProvider{
		Name:            strings.ToLower(strings.TrimSpace(req.Name)),
		DisplayName:     strings.TrimSpace(req.DisplayName),
		Enabled:         req.Enabled == nil || *req.Enabled,
		Issuer:          strings.TrimRight(strings.TrimSpace(req.Issuer), "/"),
		ClientID:        strings.TrimSpace(req.ClientID),
		ClientSecretEnc: enc,
		Scopes:          strings.TrimSpace(req.Scopes),
		AutoCreate:      req.AutoCreate,
		DefaultRole:     req.DefaultRole,
		EmailDomains:    strings.TrimSpace(req.EmailDomains),
		TrustUnverifiedEmail: req.TrustUnverifiedEmail,
	}
	if err := h.DB.Create(p).Error; err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	c.JSON(http.StatusOK, h.toView(p))
}

type providerUpdateReq struct {
	DisplayName          *string     `json:"displayName"`
	Enabled              *bool       `json:"enabled"`
	Issuer               *string     `json:"issuer"`
	ClientID             *string     `json:"clientId"`
	ClientSecret         *string     `json:"clientSecret"` // empty string = keep existing
	Scopes               *string     `json:"scopes"`
	AutoCreate           *bool       `json:"autoCreate"`
	DefaultRole          *model.Role `json:"defaultRole"`
	EmailDomains         *string     `json:"emailDomains"`
	TrustUnverifiedEmail *bool       `json:"trustUnverifiedEmail"`
}

func (h *SSOAdminHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var p model.SSOProvider
	if err := h.DB.First(&p, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	var req providerUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	// Apply non-secret fields first; secret has special "empty = keep"
	// semantics so we treat it last.
	if req.DisplayName != nil {
		v := strings.TrimSpace(*req.DisplayName)
		if v == "" {
			apiErr(c, http.StatusBadRequest, "sso.display_name_required", "displayName cannot be empty")
			return
		}
		p.DisplayName = v
	}
	if req.Enabled != nil {
		// Same anti-lockout as Delete: in oidc-only mode, disabling
		// the last enabled provider equals disabling all login.
		if !*req.Enabled && p.Enabled && LoadLoginMethod(h.DB) == LoginMethodOIDCOnly {
			var otherEnabled int64
			h.DB.Model(&model.SSOProvider{}).Where("id != ? AND enabled = ?", p.ID, true).Count(&otherEnabled)
			if otherEnabled == 0 {
				apiErr(c, http.StatusBadRequest, "sso.cannot_disable_last", "cannot disable the last enabled SSO provider while panel is in oidc-only mode")
				return
			}
		}
		p.Enabled = *req.Enabled
	}
	if req.Issuer != nil {
		v := strings.TrimRight(strings.TrimSpace(*req.Issuer), "/")
		if err := validateIssuer(v); err != nil {
			apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
			return
		}
		p.Issuer = v
	}
	if req.ClientID != nil {
		v := strings.TrimSpace(*req.ClientID)
		if v == "" {
			apiErr(c, http.StatusBadRequest, "sso.client_id_required", "clientId cannot be empty")
			return
		}
		p.ClientID = v
	}
	if req.Scopes != nil {
		p.Scopes = strings.TrimSpace(*req.Scopes)
		if p.Scopes == "" {
			p.Scopes = "openid profile email"
		}
	}
	if req.AutoCreate != nil {
		p.AutoCreate = *req.AutoCreate
	}
	if req.DefaultRole != nil {
		if !validRole(*req.DefaultRole) {
			apiErr(c, http.StatusBadRequest, "sso.bad_default_role", "invalid defaultRole")
			return
		}
		p.DefaultRole = *req.DefaultRole
	}
	if req.EmailDomains != nil {
		v := strings.TrimSpace(*req.EmailDomains)
		// B11: Update was previously the only path that skipped this
		// validator, letting an admin save garbage like "evil@x" or
		// "corp.tld, foo bar" — emailDomainAllowed in flow.go would
		// then silently never match those entries, weakening the
		// whitelist without any feedback.
		if err := validateEmailDomainsCSV(v); err != nil {
			apiErr(c, http.StatusBadRequest, "sso.bad_email_domain", err.Error())
			return
		}
		p.EmailDomains = v
	}
	if req.TrustUnverifiedEmail != nil {
		p.TrustUnverifiedEmail = *req.TrustUnverifiedEmail
	}
	if req.ClientSecret != nil && *req.ClientSecret != "" {
		enc, err := h.Cipher.Encrypt(*req.ClientSecret)
		if err != nil {
			apiErr(c, http.StatusInternalServerError, "common.internal", "encrypt secret: "+err.Error())
			return
		}
		p.ClientSecretEnc = enc
	}
	oldIssuer := ""
	if req.Issuer != nil {
		// Save the pre-update issuer so we can drop the cache entry
		// keyed by it; without that, a freshly-rotated issuer / client
		// secret keeps using the stale *oidc.Provider for up to an hour.
		var prev model.SSOProvider
		if err := h.DB.Select("issuer").First(&prev, p.ID).Error; err == nil {
			oldIssuer = prev.Issuer
		}
	}
	if err := h.DB.Save(&p).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	// Bust the per-issuer discovery cache for both the old and new
	// issuer so the next flow picks up updated endpoints / secret /
	// JWKS-URL immediately.
	h.Mgr.InvalidateCache(oldIssuer)
	h.Mgr.InvalidateCache(p.Issuer)
	c.JSON(http.StatusOK, h.toView(&p))
}

func (h *SSOAdminHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var p model.SSOProvider
	if err := h.DB.First(&p, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.not_found", "not found")
		return
	}
	// Anti-lockout: in oidc-only mode every login goes through SSO,
	// so removing the last enabled provider would leave the panel
	// with no working login channel — including for admins. Refuse
	// that. Operator can switch to oidc+password (regains password
	// fallback) and try again, or use the CLI reset subcommand.
	if LoadLoginMethod(h.DB) == LoginMethodOIDCOnly && p.Enabled {
		var otherEnabled int64
		h.DB.Model(&model.SSOProvider{}).Where("id != ? AND enabled = ?", p.ID, true).Count(&otherEnabled)
		if otherEnabled == 0 {
			apiErr(c, http.StatusBadRequest, "sso.cannot_delete_last", "cannot delete the last enabled SSO provider while panel is in oidc-only mode")
			return
		}
	}
	// Cascade-clean: any user previously bound through this IdP
	// loses that linkage. Their local user row stays — the admin
	// has to decide whether to delete those orphaned-from-SSO users
	// separately. We leave audit / login-log rows alone.
	if err := h.DB.Where("provider_id = ?", p.ID).Delete(&model.SSOIdentity{}).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", "cascade clean: "+err.Error())
		return
	}
	if err := h.DB.Delete(&p).Error; err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	// Drop the cached *oidc.Provider entry so the next flow re-discovers
	// from scratch (matters mostly when an admin recreates a provider
	// at the same issuer URL right after deleting one).
	h.Mgr.InvalidateCache(p.Issuer)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type providerTestReq struct {
	Issuer string `json:"issuer" binding:"required"`
}

type providerTestResp struct {
	OK            bool   `json:"ok"`
	AuthURL       string `json:"authUrl,omitempty"`
	TokenURL      string `json:"tokenUrl,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Test probes the issuer's OIDC discovery document. Used by the
// "Test connection" button in the admin UI before saving — admin
// can confirm the issuer URL is reachable + the right shape before
// committing client_id/secret.
func (h *SSOAdminHandler) Test(c *gin.Context) {
	var req providerTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	issuer := strings.TrimRight(strings.TrimSpace(req.Issuer), "/")
	if err := validateIssuerWithDB(issuer, h.DB); err != nil {
		if se, ok := err.(ssoErrT); ok {
			apiErr(c, http.StatusBadRequest, se.Code, se.Msg)
		} else {
			apiErr(c, http.StatusBadRequest, "sso.issuer_required", err.Error())
		}
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	// Run discovery through a private-IP-rejecting client so a DNS
	// rebinding attack between validateIssuerWithDB and oidc.NewProvider
	// can't reopen the SSRF surface that B9 is closing here.
	allowPrivate := loadAllowPrivateIssuers(h.DB)
	authURL, tokenURL, err := h.Mgr.TestProviderWithClient(ctx, issuer, safeHTTPClient(allowPrivate))
	if err != nil {
		c.JSON(http.StatusOK, providerTestResp{OK: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, providerTestResp{OK: true, AuthURL: authURL, TokenURL: tokenURL})
}

// validateProviderInput batches all admin-supplied field checks for
// Create. Update reuses the per-field validators directly because it
// can't pre-default missing fields.
func validateProviderInput(name, issuer string, role model.Role, emailDomains string) error {
	if !providerNameRe.MatchString(strings.ToLower(strings.TrimSpace(name))) {
		return errBadName
	}
	if err := validateIssuer(issuer); err != nil {
		return err
	}
	if role != "" && !validRole(role) {
		return errBadRole
	}
	if err := validateEmailDomainsCSV(emailDomains); err != nil {
		return err
	}
	return nil
}

var (
	errBadName = strErr("name must match [a-z0-9_-]{1,64}")
	errBadRole = strErr("defaultRole must be 'admin' or 'user'")
)

type strErr string

func (s strErr) Error() string { return string(s) }

func validateIssuer(s string) error {
	return validateIssuerWithDB(s, nil)
}

// validateIssuerWithDB is the DB-aware variant: when a non-nil db is
// passed, it consults the sso.allowPrivateIssuers setting and rejects
// hosts pointing at private/loopback/link-local IPs unless that flag
// is explicitly enabled. Pre-B9 the SSO test endpoint accepted
// http://127.0.0.1, http://169.254.169.254 (cloud metadata), and any
// RFC1918 address — turning the panel into a blind SSRF probe for
// anyone holding admin (or anyone exploiting A1+A4 to obtain it).
func validateIssuerWithDB(s string, db *gorm.DB) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return ssoErr("sso.issuer_required", "issuer is required")
	}
	u, err := url.Parse(s)
	if err != nil {
		return ssoErr("sso.issuer_invalid_scheme", "issuer parse: "+err.Error())
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ssoErr("sso.issuer_invalid_scheme", "issuer must be http(s)://...")
	}
	if u.Host == "" {
		return ssoErr("sso.issuer_no_host", "issuer must include a host")
	}
	if db != nil && !loadAllowPrivateIssuers(db) {
		host := u.Hostname()
		if isPrivateHost(host) {
			return ssoErr("sso.issuer_private_blocked",
				"issuer host points to a private/loopback/link-local address; an admin can enable sso.allowPrivateIssuers to override")
		}
	}
	return nil
}

// isPrivateHost returns true if `host` is (or resolves to) a
// loopback / private / link-local / unspecified address. Both literal
// IPs and resolvable hostnames are checked: a hostname like
// "metadata.google.internal" that resolves to 169.254.169.254 has to
// be rejected too.
func isPrivateHost(host string) bool {
	check := func(ip net.IP) bool {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
	}
	if ip := net.ParseIP(host); ip != nil {
		return check(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Conservative: refuse to test against unresolvable hosts. A
		// real IdP must have working DNS; this also stops admins from
		// staging an SSRF by registering a domain that NXDOMAINs at
		// validation time and resolves to 127.0.0.1 later.
		return true
	}
	for _, ip := range ips {
		if check(ip) {
			return true
		}
	}
	return false
}

// safeHTTPClient is the http.Client used for IdP-test discovery. Its
// transport's DialContext re-checks the resolved IP at connect time so
// a DNS-rebinding attacker can't slip an A record swap between
// validateIssuer and the actual TCP open. allowPrivate mirrors the
// setting the validator consulted; passing it via closure keeps the
// two checks in sync.
func safeHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if !allowPrivate && isPrivateHost(host) {
				return nil, ssoErr("sso.issuer_private_blocked",
					"refusing to dial private address "+host)
			}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second}
}

// ssoErr is a typed error that exposes both a stable error code and an
// English message; callers (Test handler) translate it back into an
// apiErr for the response. Avoids leaking string literals all the way
// to the client when a code path can be reached from multiple places.
type ssoErrT struct {
	Code string
	Msg  string
}

func (e ssoErrT) Error() string { return e.Msg }
func ssoErr(code, msg string) ssoErrT { return ssoErrT{Code: code, Msg: msg} }

const sysKeyAllowPrivateIssuers = "sso.allowPrivateIssuers"

func loadAllowPrivateIssuers(db *gorm.DB) bool {
	var s model.Setting
	if err := db.First(&s, "key = ?", sysKeyAllowPrivateIssuers).Error; err != nil {
		return false
	}
	return s.Value == "1" || s.Value == "true"
}

func validateEmailDomainsCSV(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Very loose check — domain shape (a.b at minimum). Real
		// validation is the IdP's responsibility; this just catches
		// obvious typos.
		if !strings.Contains(p, ".") || strings.ContainsAny(p, " \t\r\n@") {
			return strErr("invalid email domain: " + p)
		}
	}
	return nil
}
