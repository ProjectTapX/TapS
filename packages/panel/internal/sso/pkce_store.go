// Server-side store for PKCE verifiers, keyed by the OIDC nonce.
//
// Pre-B8 the verifier rode along inside the signed `state` URL
// parameter. That HMAC stops *forgery*, but it does nothing about
// *exposure*: any IdP-side referrer leak (analytics pixel, CDN
// redirect, error page loading third-party assets) carries the full
// authorize URL — verifier and all — out to a third party. With the
// verifier exposed, an attacker who also captures the `code`
// (commonly trivial via redirect_uri tricks or log scraping) can
// complete the token exchange themselves; PKCE protection collapses.
//
// Keeping the verifier in process memory closes that window. The
// trade-off is that an SSO flow in flight when the panel restarts
// loses its verifier and has to retry — same UX cost as the A5 SSO
// state-key swap and judged acceptable for a single-process panel.
//
// The map is intentionally tiny and unbounded: each entry is ~100
// bytes, the TTL is 10 minutes, and the janitor runs every minute.
// Even a sustained 100 logins/sec for 10 minutes caps at ~6MB.
package sso

import (
	"errors"
	"log"
	"sync"
	"time"
)

const (
	pkceTTL                = 10 * time.Minute
	defaultPKCEMaxEntries  = 10000
)

// ErrPKCENotFound is returned by Take when the nonce was never stored
// or has already been consumed/expired. Callers should treat this as a
// "the OIDC flow is no longer valid; ask the user to retry" signal.
var ErrPKCENotFound = errors.New("pkce verifier not found for nonce")

// ErrPKCEStoreFull is returned by Put when the per-process cap has
// been hit. In normal operation the store stays below the cap by a
// wide margin (each entry lives 10 min, real users open SSO flows at
// best a few times per minute). Hitting it means an attacker is
// flooding /api/oauth/start to fill memory; we refuse the new entry
// rather than evict an in-flight legitimate flow.
var ErrPKCEStoreFull = errors.New("pkce store is at capacity")

type pkceEntry struct {
	verifier  string
	expiresAt time.Time
}

type pkceStore struct {
	mu         sync.Mutex
	rows       map[string]pkceEntry
	stop       chan struct{}
	maxEntries int
}

func newPKCEStore() *pkceStore {
	s := &pkceStore{
		rows:       make(map[string]pkceEntry),
		stop:       make(chan struct{}),
		maxEntries: defaultPKCEMaxEntries,
	}
	go s.janitor()
	return s
}

// SetMaxEntries lets the panel hot-swap the cap when an admin edits
// the setting; <=0 falls back to the default. Safe to call at runtime.
func (s *pkceStore) SetMaxEntries(n int) {
	s.mu.Lock()
	if n <= 0 {
		s.maxEntries = defaultPKCEMaxEntries
	} else {
		s.maxEntries = n
	}
	s.mu.Unlock()
}

// Put records the verifier for a fresh OIDC flow. Returns
// ErrPKCEStoreFull when the cap is reached so the caller can surface
// it to the user (the redirect handler maps it to an oauth-error
// fragment indistinguishable from rate limiting — attackers learn
// nothing).
func (s *pkceStore) Put(nonce, verifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rows) >= s.maxEntries {
		// Loud log so ops can spot the flood in journalctl. Keep it
		// terse and avoid printing the nonce — that's a secret that
		// belongs only in the (now refused) caller's URL.
		log.Printf("[pkce] store at capacity (%d entries); refusing new flow", len(s.rows))
		return ErrPKCEStoreFull
	}
	s.rows[nonce] = pkceEntry{
		verifier:  verifier,
		expiresAt: time.Now().Add(pkceTTL),
	}
	return nil
}

// Take is one-shot: it returns the verifier for `nonce` and
// immediately deletes the entry, so a captured callback URL cannot be
// replayed even if the verifier itself somehow leaks downstream.
// Returns ErrPKCENotFound for both "never stored" and "already
// consumed", giving an attacker no information either way.
func (s *pkceStore) Take(nonce string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.rows[nonce]
	if !ok {
		return "", ErrPKCENotFound
	}
	delete(s.rows, nonce)
	if time.Now().After(e.expiresAt) {
		return "", ErrPKCENotFound
	}
	return e.verifier, nil
}

// Stop ends the janitor goroutine. Called from tests; production
// panels live for the process lifetime so leak is moot.
func (s *pkceStore) Stop() { close(s.stop) }

func (s *pkceStore) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			s.mu.Lock()
			for k, e := range s.rows {
				if now.After(e.expiresAt) {
					delete(s.rows, k)
				}
			}
			s.mu.Unlock()
		}
	}
}
