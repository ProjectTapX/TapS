// Package secrets handles symmetric encryption of admin-supplied
// secrets that have to be persisted in the panel DB (currently:
// SSO client secrets). The key lives at <DataDir>/secret-encryption.key,
// 32 random bytes, mode 0600, generated on first start.
//
// Crypto: AES-256-GCM. The output of Encrypt is base64(stdEncoding) of
// nonce(12) || ciphertext || tag, so it round-trips through GORM's
// `text` columns without further escaping.
//
// We deliberately keep this key separate from data/jwt.secret so an
// admin who rotates one (e.g. JWT secret) doesn't accidentally
// invalidate the other.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	keyFile         = "secret-encryption.key"
	ssoStateKeyFile = "sso-state.key"
	keySize         = 32 // AES-256
)

// Cipher caches an aead built from the key file. Safe for concurrent
// use; AEAD operations don't share state.
type Cipher struct {
	aead cipher.AEAD
}

// LoadOrCreate reads <dataDir>/secret-encryption.key, generating a
// fresh 32-byte key on first call. Returns a ready-to-use Cipher.
func LoadOrCreate(dataDir string) (*Cipher, error) {
	path := filepath.Join(dataDir, keyFile)
	key, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("%s has %d bytes, expected %d", path, len(key), keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// LoadOrCreateSSOStateKey reads <dataDir>/sso-state.key, generating a
// fresh 32-byte key on first call. Used as the HMAC key for OIDC state
// cookies. Kept separate from data/jwt.secret so JWT-secret rotation
// doesn't invalidate in-flight OIDC states (and vice versa), and so a
// leak of one doesn't compromise both. On first start of an upgraded
// deployment, any OIDC flows already in flight (state issued under the
// old shared JWT secret) will fail to verify on callback — users just
// click the SSO button again to obtain a fresh state.
func LoadOrCreateSSOStateKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, ssoStateKeyFile)
	key, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate sso state key: %w", err)
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("%s has %d bytes, expected %d", path, len(key), keySize)
	}
	return key, nil
}

// Encrypt returns base64(nonce || ciphertext) of plaintext. Empty
// plaintext yields empty output (avoids storing useless padding for
// admins who haven't filled the secret yet).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt is the inverse. Empty input → empty output (mirrors
// Encrypt's empty-pass-through), so callers don't have to special-
// case "secret never set" rows.
func (c *Cipher) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	nsz := c.aead.NonceSize()
	if len(raw) < nsz {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:nsz], raw[nsz:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("aead open: %w", err)
	}
	return string(pt), nil
}
