// Package sso implements OIDC-based single sign-on. The runtime is
// strictly standards-only: provider must expose a discovery document
// at <issuer>/.well-known/openid-configuration and an id_token signed
// by a key in its JWKS. Vendor-specific OAuth2 dialects (Feishu /
// GitHub / WeCom) are out of scope for V1.
//
// Lifecycle:
//
//   manager := sso.NewManager(db, cipher, jwtSecret, publicURL)
//   url    := manager.Start(ctx, "google")               -- 302 to IdP
//   user   := manager.Callback(ctx, "google", code, state) -- after IdP returns
//
// State is HMAC-signed and self-contained — no server-side session
// store — so panel restarts and HA replicas don't strand pending
// flows.
package sso

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/panel/internal/secrets"
)

// CallbackPath is the path component appended to publicURL for the
// IdP's redirect_uri. Each provider gets its own slug ("/<name>") so
// admins can manage one IdP at a time without disturbing others.
const CallbackPath = "/api/oauth/callback"

// Manager owns the runtime state: discovery cache, secret cipher,
// state signer. One instance per panel process; safe for concurrent
// HTTP handlers.
type Manager struct {
	db        *gorm.DB
	cipher    *secrets.Cipher
	stateKey  []byte // HMAC key for OIDC state cookies; loaded from data/sso-state.key, kept disjoint from data/jwt.secret
	publicURL string // e.g. "https://taps.example.com"; trailing slash trimmed
	pkce      *pkceStore

	mu    sync.Mutex
	cache map[string]*cachedProvider // keyed by issuer URL
}

type cachedProvider struct {
	provider  *oidc.Provider
	expiresAt time.Time
}

func NewManager(db *gorm.DB, cipher *secrets.Cipher, stateKey []byte, publicURL string) *Manager {
	return &Manager{
		db:        db,
		cipher:    cipher,
		stateKey:  stateKey,
		publicURL: strings.TrimRight(publicURL, "/"),
		pkce:      newPKCEStore(),
		cache:     map[string]*cachedProvider{},
	}
}

// SetPublicURL updates the panel's outward-facing base URL. Called by
// the settings handler when admin changes system.publicUrl so callback
// URLs reflect the new value without restarting.
func (m *Manager) SetPublicURL(u string) {
	m.mu.Lock()
	m.publicURL = strings.TrimRight(u, "/")
	m.mu.Unlock()
}

// PublicURL returns the current configured base URL (trailing slash
// stripped). Empty when admin hasn't set system.publicUrl yet — the
// HTTP handlers reject SSO start in that case.
func (m *Manager) PublicURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publicURL
}

// CallbackURL is the absolute redirect_uri passed to the IdP. The
// path includes the provider name slug so callbacks are routed
// without ambiguity.
func (m *Manager) CallbackURL(providerName string) string {
	if m.publicURL == "" {
		return ""
	}
	return m.publicURL + CallbackPath + "/" + providerName
}

// InvalidateCache drops the cached *oidc.Provider for the given
// issuer. Admin handlers call this after Update / Delete so the next
// flow re-discovers endpoints (and picks up rotated client_secret /
// auth_url / etc.) instead of riding the stale cache for up to an
// hour. Safe to call with an empty string or unknown issuer — no-op.
func (m *Manager) InvalidateCache(issuer string) {
	if issuer == "" {
		return
	}
	m.mu.Lock()
	delete(m.cache, issuer)
	m.mu.Unlock()
}

// SetPKCEMaxEntries proxies to the underlying pkceStore so admins can
// adjust the DoS-mitigation cap from settings without restart.
func (m *Manager) SetPKCEMaxEntries(n int) {
	if m.pkce == nil {
		return
	}
	m.pkce.SetMaxEntries(n)
}

// loadProvider returns the DB row + a cached *oidc.Provider for the
// given slug. The discovery document is cached for an hour; an issuer
// rotating its JWKS every few minutes is still picked up because
// go-oidc handles JWKS refresh internally on every Verify call.
func (m *Manager) loadProvider(ctx context.Context, name string) (*model.SSOProvider, *oidc.Provider, error) {
	var row model.SSOProvider
	if err := m.db.Where("name = ? AND enabled = ?", name, true).First(&row).Error; err != nil {
		return nil, nil, fmt.Errorf("provider %q not found or disabled", name)
	}
	if row.Issuer == "" {
		return nil, nil, errors.New("provider has no issuer configured")
	}

	m.mu.Lock()
	cp, ok := m.cache[row.Issuer]
	m.mu.Unlock()
	if ok && time.Now().Before(cp.expiresAt) {
		return &row, cp.provider, nil
	}

	disc, err := oidc.NewProvider(ctx, row.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc discovery: %w", err)
	}
	m.mu.Lock()
	m.cache[row.Issuer] = &cachedProvider{provider: disc, expiresAt: time.Now().Add(time.Hour)}
	m.mu.Unlock()
	return &row, disc, nil
}

// oauth2Config builds the standard library oauth2.Config for one
// provider, with our callback URL injected. Caller passes the row +
// discovered endpoints to avoid double DB hits.
func (m *Manager) oauth2Config(row *model.SSOProvider, disc *oidc.Provider) (*oauth2.Config, error) {
	secret, err := m.cipher.Decrypt(row.ClientSecretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt client_secret: %w", err)
	}
	scopes := normaliseScopes(row.Scopes)
	return &oauth2.Config{
		ClientID:     row.ClientID,
		ClientSecret: secret,
		Endpoint:     disc.Endpoint(),
		RedirectURL:  m.CallbackURL(row.Name),
		Scopes:       scopes,
	}, nil
}

// normaliseScopes ensures "openid" is always requested. OIDC spec
// requires it; without it the IdP returns an OAuth2 token without an
// id_token and our verifier blows up.
func normaliseScopes(stored string) []string {
	parts := strings.Fields(stored)
	hasOpenID := false
	for _, p := range parts {
		if p == "openid" {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		parts = append([]string{"openid"}, parts...)
	}
	return parts
}

// TestProvider attempts discovery against the issuer to confirm the
// admin's configuration is reachable. Used by the settings UI's
// "Test connection" button. Returns the resolved endpoints on
// success, or a descriptive error.
func (m *Manager) TestProvider(ctx context.Context, issuer string) (authURL, tokenURL string, err error) {
	disc, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return "", "", err
	}
	ep := disc.Endpoint()
	return ep.AuthURL, ep.TokenURL, nil
}

// TestProviderWithClient runs discovery using the supplied http.Client.
// API handlers wire in a client whose Transport rejects private IPs at
// dial time (DNS-rebinding defense). Falls back to the bare TestProvider
// if client is nil so callers without SSRF concerns stay simple.
func (m *Manager) TestProviderWithClient(ctx context.Context, issuer string, client *http.Client) (authURL, tokenURL string, err error) {
	if client == nil {
		return m.TestProvider(ctx, issuer)
	}
	ctx = oidc.ClientContext(ctx, client)
	disc, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return "", "", err
	}
	ep := disc.Endpoint()
	return ep.AuthURL, ep.TokenURL, nil
}
