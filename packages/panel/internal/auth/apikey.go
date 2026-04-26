package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

const apiKeyPrefix = "tps_"

// IssueAPIKey returns the plaintext key (shown once) and persists a row with the hash.
// expiresAt may be nil for never-expiring keys (legacy default).
func IssueAPIKey(db *gorm.DB, userID uint, name, ipWhitelist, scopes string, expiresAt *time.Time) (string, *model.APIKey, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw := apiKeyPrefix + hex.EncodeToString(buf) // 4 + 48 = 52 chars
	row := &model.APIKey{
		UserID:      userID,
		Name:        name,
		KeyHash:     hashKey(raw),
		Prefix:      raw[:12],
		IPWhitelist: ipWhitelist,
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
	}
	if err := db.Create(row).Error; err != nil {
		return "", nil, err
	}
	return raw, row, nil
}

// LookupAPIKey resolves a raw key string to its owner. It also enforces the
// IP whitelist if present. Scope checks are done by the caller per-request.
// Rejects expired (ExpiresAt < now) and revoked (RevokedAt set) keys.
func LookupAPIKey(db *gorm.DB, raw, callerIP string) (*model.User, *model.APIKey, error) {
	if !strings.HasPrefix(raw, apiKeyPrefix) {
		return nil, nil, errors.New("not an api key")
	}
	var k model.APIKey
	if err := db.Where("key_hash = ?", hashKey(raw)).First(&k).Error; err != nil {
		return nil, nil, err
	}
	if k.RevokedAt != nil {
		return nil, nil, errors.New("api key revoked")
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return nil, nil, errors.New("api key expired")
	}
	if !ipAllowed(k.IPWhitelist, callerIP) {
		return nil, nil, errors.New("ip not whitelisted")
	}
	var u model.User
	if err := db.First(&u, k.UserID).Error; err != nil {
		return nil, nil, err
	}
	db.Model(&k).Update("last_used", time.Now())
	return &u, &k, nil
}

// ipAllowed checks comma-separated CIDR or exact IP entries. Empty list = allow all.
func ipAllowed(list, ip string) bool {
	list = strings.TrimSpace(list)
	if list == "" {
		return true
	}
	host := ip
	if h, _, err := net.SplitHostPort(ip); err == nil {
		host = h
	}
	addr := net.ParseIP(host)
	if addr == nil {
		return false
	}
	for _, raw := range strings.Split(list, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, "/") {
			if _, cidr, err := net.ParseCIDR(raw); err == nil && cidr.Contains(addr) {
				return true
			}
		} else if a := net.ParseIP(raw); a != nil && a.Equal(addr) {
			return true
		}
	}
	return false
}

// ScopeMatches returns true when the key's scope CSV is empty (full access)
// or contains the requested scope. Scopes look like "instance.read", "instance.write".
func ScopeMatches(scopes, want string) bool {
	scopes = strings.TrimSpace(scopes)
	if scopes == "" {
		return true
	}
	for _, s := range strings.Split(scopes, ",") {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

func IsAPIKey(raw string) bool { return strings.HasPrefix(raw, apiKeyPrefix) }

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
