// Package alerts dispatches simple JSON webhook notifications. The webhook URL
// is read from the panel `setting` table at key "alert_webhook_url" so admins
// can change it from the UI without restarting.
//
// Audit-2026-04-24-v3 H1: SetURL now validates scheme + host shape and
// refuses private / loopback / link-local destinations unless the
// admin has flipped on `webhook.allowPrivate`. The HTTP dialer also
// re-checks the resolved IP at connect time so a DNS-rebinding
// attacker can't bypass the validator that ran at save time.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
	"github.com/taps/panel/internal/netutil"
)

const settingKey = "alert_webhook_url"

// Sentinel errors for SetURL validation. Callers (the SettingsHandler)
// errors.Is against these to map to stable apiErr codes for the
// frontend's i18n. The DNS-failed case is split out from PrivateHost
// so the operator gets a "retry later" hint instead of being told
// their corporate domain is "private" when the resolver is just
// momentarily unhappy.
var (
	ErrWebhookInvalidScheme = errors.New("webhook url must start with http:// or https://")
	ErrWebhookInvalidHost   = errors.New("webhook url must include a host")
	ErrWebhookPrivateHost   = errors.New("webhook url points to a private/loopback/link-local address")
	ErrWebhookDNSFailed     = errors.New("webhook url host failed DNS resolution")
)

type Dispatcher struct {
	db *gorm.DB

	mu           sync.RWMutex
	url          string
	allowPrivate bool
}

func New(db *gorm.DB) *Dispatcher {
	d := &Dispatcher{db: db}
	d.Reload()
	return d
}

// Reload re-reads the webhook URL + allowPrivate flag from settings.
// Called after admin updates either value, and once at startup.
func (d *Dispatcher) Reload() {
	get := func(k string) string {
		var s model.Setting
		if err := d.db.Where("key = ?", k).First(&s).Error; err == nil {
			return s.Value
		}
		return ""
	}
	d.mu.Lock()
	d.url = get(settingKey)
	d.allowPrivate = get("webhook.allowPrivate") == "1"
	d.mu.Unlock()
}

func (d *Dispatcher) URL() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.url
}

func (d *Dispatcher) AllowPrivate() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.allowPrivate
}

// validateURL is exported as a method on Dispatcher so tests can swap
// out the allowPrivate source. Empty URL = "disable webhook" — always
// allowed (no destination = no SSRF surface).
func (d *Dispatcher) validateURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrWebhookInvalidScheme
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrWebhookInvalidScheme
	}
	if u.Host == "" {
		return ErrWebhookInvalidHost
	}
	// Use the trichotomy classifier so DNS failure surfaces as a
	// distinct error (operator sees "DNS lookup failed; check the
	// hostname or retry") rather than being lumped under "private
	// host" (which would confuse anyone using a real public hostname
	// during a transient resolver hiccup). The dial-time DialContext
	// in SafeHTTPClient still treats DNS-failure as private at the
	// network layer — that's defence in depth, not a contradiction.
	if !d.AllowPrivate() {
		switch netutil.ClassifyHost(u.Hostname()) {
		case netutil.HostPrivate:
			return ErrWebhookPrivateHost
		case netutil.HostDNSFailed:
			return ErrWebhookDNSFailed
		}
	}
	return nil
}

// SetURL persists a new webhook destination after validating it. Empty
// `url` clears the configured webhook (no further notifications fire).
// Returns one of the sentinel errors above on validation failure so
// the SettingsHandler can map to a stable apiErr code.
func (d *Dispatcher) SetURL(raw string) error {
	if err := d.validateURL(raw); err != nil {
		return err
	}
	if err := d.db.Save(&model.Setting{Key: settingKey, Value: raw}).Error; err != nil {
		return err
	}
	d.Reload()
	return nil
}

// SetAllowPrivate flips the override that lets `SetURL` accept private
// destinations. Reload picks up the new value on next read; existing
// in-flight Notify calls grab the new value via AllowPrivate() so the
// dialer re-check matches what the admin just saved.
func (d *Dispatcher) SetAllowPrivate(allow bool) error {
	val := "0"
	if allow {
		val = "1"
	}
	if err := d.db.Save(&model.Setting{Key: "webhook.allowPrivate", Value: val}).Error; err != nil {
		return err
	}
	d.Reload()
	return nil
}

// Notify sends a fire-and-forget JSON POST. Returns immediately; failures
// are logged. Uses SafeHTTPClient so a DNS-rebinding attacker can't
// flip the destination IP between save-time validation and actual dial.
func (d *Dispatcher) Notify(event string, payload map[string]any) {
	url := d.URL()
	if url == "" {
		return
	}
	allowPrivate := d.AllowPrivate()
	body := map[string]any{
		"event":     event,
		"timestamp": time.Now().Unix(),
		"payload":   payload,
	}
	go func() {
		buf, _ := json.Marshal(body)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "TapS-Webhook/1.0")
		client := netutil.SafeHTTPClient(allowPrivate, 5*time.Second)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("alert webhook failed: %v", err)
			return
		}
		resp.Body.Close()
	}()
}
