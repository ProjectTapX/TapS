package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

type Config struct {
	Addr        string
	DataDir     string
	WebDir      string
	DBPath      string
	JWTSecret   []byte
	AdminUser   string
	AdminPass   string // initial password (only used if no users exist)
	CORSOrigins []string
	TLSCert     string // optional; if both set the server runs HTTPS
	TLSKey      string
}

func Load() *Config {
	dataDir := envOr("TAPS_DATA_DIR", "./data")
	_ = os.MkdirAll(dataDir, 0o755)

	c := &Config{
		Addr:        envOr("TAPS_ADDR", ":24444"),
		DataDir:     dataDir,
		WebDir:      envOr("TAPS_WEB_DIR", "./web"),
		DBPath:      filepath.Join(dataDir, "panel.db"),
		AdminUser:   envOr("TAPS_ADMIN_USER", "admin"),
		AdminPass:   envOr("TAPS_ADMIN_PASS", "admin"),
		CORSOrigins: []string{"*"},
		TLSCert:     envOr("TAPS_TLS_CERT", ""),
		TLSKey:      envOr("TAPS_TLS_KEY", ""),
	}
	c.JWTSecret = loadOrCreateSecret(filepath.Join(dataDir, "jwt.secret"))
	return c
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadOrCreateSecret(path string) []byte {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b
	}
	buf := make([]byte, 48)
	_, _ = rand.Read(buf)
	enc := []byte(hex.EncodeToString(buf))
	_ = os.WriteFile(path, enc, 0o600)
	return enc
}
