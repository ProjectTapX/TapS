// Package tlscert keeps the daemon's self-signed certificate. On first
// boot we generate one (ECDSA P-256, 99-year validity per the Batch #5
// design — daemons are long-lived appliances that never want to rotate
// for "expired cert" reasons), persist cert.pem + key.pem next to the
// token file, and expose the SHA-256 fingerprint so the operator can
// pin it from the panel side.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Material is the in-memory bundle of cert + key + fingerprint we hand
// to the HTTP server and surface in startup logs.
type Material struct {
	Cert        tls.Certificate
	PEM         []byte // cert PEM only (no key) — used by the panel for pinning
	Fingerprint string // SHA-256 lowercase hex with colons, e.g. "ab:cd:..."
}

// LoadOrCreate returns the existing cert at <dir>/cert.pem + key.pem,
// or generates a new self-signed one (and persists it) when either
// file is missing. A boolean reports whether we just created it so the
// caller can log the fingerprint loudly on first boot.
func LoadOrCreate(dir string) (*Material, bool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if _, err1 := os.Stat(certPath); err1 == nil {
		if _, err2 := os.Stat(keyPath); err2 == nil {
			m, err := load(certPath, keyPath)
			if err == nil {
				return m, false, nil
			}
			// Corrupt files — fall through to regen so the daemon doesn't
			// brick itself on a half-written rotation.
		}
	}
	m, err := generate()
	if err != nil {
		return nil, false, err
	}
	if err := persist(certPath, keyPath, m); err != nil {
		return nil, false, fmt.Errorf("persist cert: %w", err)
	}
	return m, true, nil
}

func load(certPath, keyPath string) (*Material, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	fp, err := fingerprintFromPEM(pemBytes)
	if err != nil {
		return nil, err
	}
	return &Material{Cert: cert, PEM: pemBytes, Fingerprint: fp}, nil
}

func generate() (*Material, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	notBefore := time.Now().Add(-1 * time.Hour) // tolerate small clock skew on first connect
	notAfter := notBefore.Add(99 * 365 * 24 * time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "taps-daemon"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		// We pin by fingerprint, not by name, so we don't need a SAN
		// list — but include a couple of harmless defaults so casual
		// hostname-checking tools don't choke.
		DNSNames: []string{"taps-daemon", "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	fp := fingerprintFromDER(der)
	return &Material{Cert: cert, PEM: certPEM, Fingerprint: fp}, nil
}

func persist(certPath, keyPath string, m *Material) error {
	if err := os.WriteFile(certPath, m.PEM, 0o600); err != nil {
		return err
	}
	// We rebuild the key PEM from the in-memory cert.PrivateKey so we
	// don't need to thread the raw bytes around. tls.Certificate stores
	// PrivateKey as crypto.PrivateKey; cast back and re-marshal.
	priv, ok := m.Cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return errors.New("unexpected private key type")
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return os.WriteFile(keyPath, keyPEM, 0o600)
}

// FingerprintFromPEM returns the SHA-256 fingerprint of the first
// certificate block in a PEM-encoded payload. Public so the panel can
// recompute and verify what it stored against what the daemon actually
// presents on the wire.
func FingerprintFromPEM(pemBytes []byte) (string, error) {
	return fingerprintFromPEM(pemBytes)
}

func fingerprintFromPEM(pemBytes []byte) (string, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("not a CERTIFICATE PEM block")
	}
	return fingerprintFromDER(block.Bytes), nil
}

// FingerprintFromDER returns the same colon-hex format from a raw DER
// certificate (handy for verifying the cert presented during a TLS
// handshake — `*tls.ConnectionState`.PeerCertificates[0].Raw).
func FingerprintFromDER(der []byte) string { return fingerprintFromDER(der) }

func fingerprintFromDER(der []byte) string {
	sum := sha256.Sum256(der)
	hexStr := hex.EncodeToString(sum[:])
	var b strings.Builder
	b.Grow(len(hexStr) + len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hexStr[i : i+2])
	}
	return b.String()
}
