// State / nonce handling. Stateless across the URL fragment: the
// signed `state` query carries provider name, nonce, expiry, HMAC.
// PKCE verifier is NOT in `state` — see pkce_store.go for why and how
// it lives server-side.
//
// Layout (raw bytes, before base64url):
//
//   payload | hmac
//   payload = providerName(1-byte len + bytes) || nonce(43 base64 chars) || expiresAt(8 BE)
//   hmac    = HMAC-SHA256(stateKey, payload)        (32 bytes)
package sso

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	stateTTL          = 5 * time.Minute
	pkceVerifierBytes = 32 // → 43-char base64url verifier (RFC 7636 §4.1)
	nonceBytes        = 32
	hmacBytes         = 32
)

type statePayload struct {
	Provider string
	Nonce    string // base64url(32 bytes random)
	Expires  time.Time
}

// newState generates a fresh PKCE verifier + nonce, stashes the
// verifier in the server-side store keyed by the nonce, and returns
// (state, verifier, nonce). The verifier is returned only so the
// caller can compute the auth URL's code_challenge — it is never
// embedded in the state token itself.
func newState(stateKey []byte, store *pkceStore, provider string) (state, verifier, nonce string, err error) {
	verRaw := make([]byte, pkceVerifierBytes)
	if _, err = rand.Read(verRaw); err != nil {
		return
	}
	nonceRaw := make([]byte, nonceBytes)
	if _, err = rand.Read(nonceRaw); err != nil {
		return
	}
	verifier = base64.RawURLEncoding.EncodeToString(verRaw)
	nonce = base64.RawURLEncoding.EncodeToString(nonceRaw)
	if err = store.Put(nonce, verifier); err != nil {
		return
	}
	p := statePayload{
		Provider: provider,
		Nonce:    nonce,
		Expires:  time.Now().Add(stateTTL),
	}
	state, err = encodeState(stateKey, p)
	if err != nil {
		return
	}
	return
}

// verifyState parses the state from a callback, checks the HMAC, and
// confirms it hasn't expired. Caller then uses the returned Nonce to
// fetch (and consume) the verifier from the PKCE store before the
// token exchange.
func verifyState(stateKey []byte, state, expectedProvider string) (statePayload, error) {
	p, err := decodeState(stateKey, state)
	if err != nil {
		return statePayload{}, err
	}
	if time.Now().After(p.Expires) {
		return statePayload{}, errors.New("state expired")
	}
	if p.Provider != expectedProvider {
		return statePayload{}, fmt.Errorf("state provider mismatch: %q != %q", p.Provider, expectedProvider)
	}
	return p, nil
}

func encodeState(stateKey []byte, p statePayload) (string, error) {
	if len(p.Provider) > 255 {
		return "", errors.New("provider name too long")
	}
	body := make([]byte, 0, 1+len(p.Provider)+len(p.Nonce)+8)
	body = append(body, byte(len(p.Provider)))
	body = append(body, []byte(p.Provider)...)
	if len(p.Nonce) != base64.RawURLEncoding.EncodedLen(nonceBytes) {
		return "", errors.New("nonce length unexpected")
	}
	body = append(body, []byte(p.Nonce)...)
	expBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(expBuf, uint64(p.Expires.Unix()))
	body = append(body, expBuf...)
	mac := hmac.New(sha256.New, stateKey)
	mac.Write(body)
	signed := append(body, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(signed), nil
}

func decodeState(stateKey []byte, encoded string) (statePayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return statePayload{}, fmt.Errorf("base64: %w", err)
	}
	if len(raw) < 1+0+base64.RawURLEncoding.EncodedLen(nonceBytes)+8+hmacBytes {
		return statePayload{}, errors.New("state too short")
	}
	body, mac := raw[:len(raw)-hmacBytes], raw[len(raw)-hmacBytes:]
	expectedMac := hmac.New(sha256.New, stateKey)
	expectedMac.Write(body)
	if !hmac.Equal(mac, expectedMac.Sum(nil)) {
		return statePayload{}, errors.New("state hmac mismatch")
	}
	off := 0
	provLen := int(body[off])
	off++
	if off+provLen > len(body) {
		return statePayload{}, errors.New("provider length oversize")
	}
	provider := string(body[off : off+provLen])
	off += provLen
	nonceLen := base64.RawURLEncoding.EncodedLen(nonceBytes)
	if off+nonceLen > len(body) {
		return statePayload{}, errors.New("nonce truncated")
	}
	nonce := string(body[off : off+nonceLen])
	off += nonceLen
	if off+8 != len(body) {
		return statePayload{}, errors.New("trailing bytes after expires")
	}
	exp := int64(binary.BigEndian.Uint64(body[off : off+8]))
	return statePayload{
		Provider: provider,
		Nonce:    nonce,
		Expires:  time.Unix(exp, 0),
	}, nil
}

// pkceChallenge is SHA256(verifier) base64url; the IdP receives this
// at /authorize and verifies it against the verifier we send at
// /token (RFC 7636 §4.2 method=S256).
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
