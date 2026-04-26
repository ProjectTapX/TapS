// High-level OIDC flow: BuildAuthURL → IdP → Callback → user match /
// create. The matchOrCreateUser logic is the security-critical bit;
// keep it small and audit-friendly.
package sso

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/model"
)

// CallbackError carries a stable error code (matched against the
// frontend i18n table) plus the original wrapped error for
// server-side audit logging. Audit-2026-04-25-v2 MED17: previously
// every Callback failure path emitted a free-form fmt.Errorf string
// that the HTTP layer dumped into both the URL fragment and the
// LoginLog reason — a token-exchange failure could leak fragments
// of the IdP's HTTP response body (occasionally including refresh-
// token snippets) all the way to the user's browser history.
//
// The new contract: caller (sso.go failRedirect) reads .Code and
// emits only that to the URL fragment; .Err.Error() goes to the
// audit log where only admins can see it.
type CallbackError struct {
	Code string
	Err  error
}

func (e *CallbackError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *CallbackError) Unwrap() error { return e.Err }

// cbErr is a tiny constructor so call sites stay readable.
func cbErr(code string, err error) *CallbackError {
	return &CallbackError{Code: code, Err: err}
}

// cbErrMsg wraps a literal string into the same shape — used where
// the failure was detected by us (no upstream error to propagate).
func cbErrMsg(code, msg string) *CallbackError {
	return &CallbackError{Code: code, Err: errors.New(msg)}
}

// BuildAuthURL is step 1 of the login flow. Generates state +
// PKCE + nonce, returns the URL the browser should be 302'd to.
func (m *Manager) BuildAuthURL(ctx context.Context, providerName string) (string, error) {
	if m.PublicURL() == "" {
		return "", errors.New("system.publicUrl not configured; admin must set it before SSO works")
	}
	row, disc, err := m.loadProvider(ctx, providerName)
	if err != nil {
		return "", err
	}
	cfg, err := m.oauth2Config(row, disc)
	if err != nil {
		return "", err
	}
	state, verifier, nonce, err := newState(m.stateKey, m.pkce, providerName)
	if err != nil {
		return "", err
	}
	url := cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oidc.Nonce(nonce),
	)
	return url, nil
}

// CallbackResult is what Callback returns: the matched/created local
// user, plus a flag indicating whether the user was created (so the
// HTTP layer can log appropriately).
type CallbackResult struct {
	User    *model.User
	Created bool // true → newly auto-created
}

// Callback completes the flow: validate state, exchange code for
// tokens, verify id_token, run user match logic.
func (m *Manager) Callback(ctx context.Context, providerName, code, state string) (*CallbackResult, error) {
	if code == "" {
		return nil, cbErrMsg("sso.missing_code", "missing code")
	}
	if state == "" {
		return nil, cbErrMsg("sso.missing_state", "missing state")
	}
	sp, err := verifyState(m.stateKey, state, providerName)
	if err != nil {
		return nil, cbErr("sso.state_invalid", err)
	}
	// Pull the PKCE verifier from the server-side store. One-shot:
	// Take() deletes the entry, so even if the same callback URL is
	// re-played later (browser back button, captured access log,
	// stolen referrer) the second attempt errors out cleanly.
	verifier, err := m.pkce.Take(sp.Nonce)
	if err != nil {
		return nil, cbErr("sso.pkce_not_found", err)
	}
	row, disc, err := m.loadProvider(ctx, providerName)
	if err != nil {
		return nil, cbErr("sso.provider_load_failed", err)
	}
	cfg, err := m.oauth2Config(row, disc)
	if err != nil {
		return nil, cbErr("sso.provider_load_failed", err)
	}
	tok, err := cfg.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return nil, cbErr("sso.token_exchange_failed", err)
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, cbErrMsg("sso.id_token_missing", "id_token missing from token response")
	}
	idVerifier := disc.Verifier(&oidc.Config{ClientID: row.ClientID})
	idToken, err := idVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, cbErr("sso.id_token_verify_failed", err)
	}
	if idToken.Nonce != sp.Nonce {
		return nil, cbErrMsg("sso.nonce_mismatch", "id_token nonce mismatch")
	}
	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     *bool  `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
		// Logto (and a few other IdPs) ship the user's handle under
		// the non-standard "username" claim instead of (or alongside)
		// "preferred_username". We accept either.
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, cbErr("sso.id_token_decode_failed", err)
	}
	if claims.Sub == "" {
		return nil, cbErrMsg("sso.no_sub_claim", "id_token has no sub claim")
	}
	preferred := claims.PreferredUsername
	if preferred == "" {
		preferred = claims.Username
	}
	// audit-2026-04-25 H3: normalise email to lowercase before *any*
	// matching. Without this an attacker who controls the IdP can vary
	// the case ("Admin@x.com" vs "admin@x.com") to bypass the
	// "existing local user with this email" guard (admin auto-bind
	// refusal in matchOrCreateUser).
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	// email_verified is *bool because the claim is optional in OIDC and
	// some IdPs omit it entirely. Treat omitted as "not verified" so the
	// default policy stays safe; per-provider TrustUnverifiedEmail lets
	// admins of IdPs that never emit the claim opt in explicitly.
	emailVerified := claims.EmailVerified != nil && *claims.EmailVerified
	return m.matchOrCreateUser(row, claims.Sub, email, preferred, emailVerified)
}

// matchOrCreateUser is the identity binding policy:
//
//   1. (provider, sub) already in sso_identities → reuse that user
//   2. otherwise validate email_domains
//   3. user with that email exists → bind (auto-link) and return it
//   4. no user with that email + auto_create=true → create + bind
//   5. no user + auto_create=false → reject
//
// All paths update sso_identities.last_used_at so admins can spot
// abandoned accounts in audit views.
func (m *Manager) matchOrCreateUser(row *model.SSOProvider, sub, email, preferredUsername string, emailVerified bool) (*CallbackResult, error) {
	now := time.Now()
	// email_verified gate. Required by default; provider-level opt-out
	// for IdPs that don't ship the claim. Applies to *all* paths below
	// (existing binding included): an attacker who managed to plant a
	// (provider, sub) row through some other defect still can't ride it
	// in once the IdP marks the email as unverified.
	if email != "" && !emailVerified && !row.TrustUnverifiedEmail {
		return nil, cbErrMsg("sso.email_not_verified", "IdP did not assert email_verified=true; refusing to bind")
	}
	// Step 1: existing binding
	var ident model.SSOIdentity
	err := m.db.Where("provider_id = ? AND subject = ?", row.ID, sub).First(&ident).Error
	if err == nil {
		// Re-check the email-domain whitelist on every login, even for
		// already-bound identities. Without this, an admin who tightens
		// EmailDomains later sees old users from now-disallowed domains
		// keep logging in indefinitely — the original guard only ran in
		// Step 2 (the "first login" path).
		if email != "" && !emailDomainAllowed(email, row.EmailDomains) {
			return nil, cbErr("sso.email_domain_not_allowed", fmt.Errorf("email %q no longer in allowed domains", email))
		}
		var u model.User
		if uerr := m.db.First(&u, ident.UserID).Error; uerr == nil {
			m.db.Model(&ident).Updates(map[string]any{
				"last_used_at": now,
				"email":        email,
			})
			return &CallbackResult{User: &u, Created: false}, nil
		} else if errors.Is(uerr, gorm.ErrRecordNotFound) {
			// Orphan binding: the local user this identity pointed at
			// got deleted (admin removed them, or DB hand-edit). Drop
			// the dead row and fall through to the email-match / auto
			// -create path so the user lands on a fresh account.
			m.db.Delete(&ident)
		} else {
			return nil, cbErr("sso.user_load_failed", fmt.Errorf("load bound user %d: %w", ident.UserID, uerr))
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, cbErr("sso.identity_lookup_failed", err)
	}
	// Step 2: email domain check
	if email == "" {
		return nil, cbErrMsg("sso.no_email_claim", "IdP provided no email; cannot match local account")
	}
	if !emailDomainAllowed(email, row.EmailDomains) {
		return nil, cbErr("sso.email_domain_not_allowed", fmt.Errorf("email %q not in allowed domains", email))
	}
	// Step 3: existing local user with same email → bind
	var existing model.User
	err = m.db.Where("email = ?", email).First(&existing).Error
	if err == nil {
		// Hard refusal for admin-role accounts: silently auto-binding an
		// IdP identity to a local admin is the canonical SSO account-
		// takeover chain. Even with email_verified=true the IdP-side
		// account may belong to someone else (org with open registration,
		// stale ex-employee, social-engineered IdP support). Force the
		// admin to log in locally first and bind from the account page.
		if existing.Role == model.RoleAdmin {
			return nil, cbErrMsg("sso.admin_auto_bind_blocked", "an admin account exists with this email; auto-binding disabled for admins")
		}
		bind := &model.SSOIdentity{
			UserID:     existing.ID,
			ProviderID: row.ID,
			Subject:    sub,
			Email:      email,
			LinkedAt:   now,
			LastUsedAt: now,
		}
		if err := m.db.Create(bind).Error; err != nil {
			return nil, cbErr("sso.bind_user_failed", err)
		}
		return &CallbackResult{User: &existing, Created: false}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, cbErr("sso.user_lookup_failed", err)
	}
	// Step 4 / 5: create or reject
	if !row.AutoCreate {
		return nil, cbErrMsg("sso.no_local_match", "no local account matches this email; ask an admin to create one and bind it")
	}
	username, err := m.pickUsername(preferredUsername, email)
	if err != nil {
		return nil, cbErr("sso.username_pick_failed", err)
	}
	// We need a valid bcrypt hash for the password column (NOT NULL).
	// Generate a long random "ssh-only" password the user never knows
	// — they log in via SSO. Admin can later reset it from the user
	// edit page if they want to enable password fallback.
	randomPw := make([]byte, 32)
	if _, err := rand.Read(randomPw); err != nil {
		return nil, cbErr("sso.create_user_failed", err)
	}
	hash, err := auth.HashPassword(hex.EncodeToString(randomPw))
	if err != nil {
		return nil, cbErr("sso.create_user_failed", err)
	}
	newUser := &model.User{
		Username:     username,
		PasswordHash: hash,
		Email:        email,
		Role:         row.DefaultRole,
		// PasswordHash is a throw-away random value; let the change-
		// password endpoint know the user can claim a real one without
		// being asked for "the old password" they never had.
		HasPassword:  false,
	}
	if err := m.db.Create(newUser).Error; err != nil {
		return nil, cbErr("sso.create_user_failed", err)
	}
	bind := &model.SSOIdentity{
		UserID:     newUser.ID,
		ProviderID: row.ID,
		Subject:    sub,
		Email:      email,
		LinkedAt:   now,
		LastUsedAt: now,
	}
	if err := m.db.Create(bind).Error; err != nil {
		// Best-effort: don't roll back user, the admin can clean up.
		return nil, cbErr("sso.bind_user_failed", err)
	}
	return &CallbackResult{User: newUser, Created: true}, nil
}

// pickUsername builds the candidate chain:
//
//   1. preferred_username from the IdP, if non-empty and free
//   2. full email, if free
//   3. fall back to candidate #1 + "-<short>" suffix until free
//
// Note: we deliberately do NOT use the email's local-part (the bit
// before "@") as a fallback. That was confusing in practice — a user
// "yingxi" on the IdP whose email is cnyingxi@... would land in TapS
// as "cnyingxi", which doesn't match any of their identities. The
// full email is uglier but unambiguous.
//
// All candidates are normalised to lowercase + stripped of whitespace
// so case-insensitive collisions don't slip through SQLite's default
// case-sensitive index.
func (m *Manager) pickUsername(preferred, email string) (string, error) {
	candidates := []string{}
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return
		}
		candidates = append(candidates, s)
	}
	add(preferred)
	add(email)
	for _, c := range candidates {
		if !m.usernameTaken(c) {
			return c, nil
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("IdP returned neither preferred_username/username nor email; cannot pick a TapS username")
	}
	// Last resort: append a random suffix.
	base := candidates[0]
	for i := 0; i < 5; i++ {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		c := base + "-" + hex.EncodeToString(buf)
		if !m.usernameTaken(c) {
			return c, nil
		}
	}
	return "", errors.New("could not allocate unique username after 5 attempts")
}

func (m *Manager) usernameTaken(s string) bool {
	var n int64
	m.db.Model(&model.User{}).Where("username = ?", s).Count(&n)
	return n > 0
}

// emailDomainAllowed honors the CSV whitelist on the provider row.
// Empty whitelist = no restriction. Comparison is case-insensitive
// on the domain part; localpart casing is preserved for display but
// doesn't affect routing.
func emailDomainAllowed(email, csv string) bool {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range strings.Split(csv, ",") {
		if strings.ToLower(strings.TrimSpace(d)) == domain {
			return true
		}
	}
	return false
}
