// Package captcha verifies a client-supplied captcha token against
// either Cloudflare Turnstile or Google reCAPTCHA.
//
// The panel gates the login endpoint with this package. All other
// endpoints sit behind JWT, so we don't bother captcha-protecting
// them. Misconfiguration (missing keys, wrong site key, etc.) is
// classified via ErrConfig so the login handler can fail-open and
// the admin doesn't lock themselves out by saving wrong settings.
package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider names used in the settings table.
const (
	ProviderNone      = "none"
	ProviderRecaptcha = "recaptcha"
	ProviderTurnstile = "turnstile"
)

// ErrConfig wraps every error that's clearly admin-misconfiguration:
// missing fields, bad secret/site key, hostname not registered, etc.
// The login handler treats these as "captcha effectively off" and
// fails open — otherwise the admin would lock themselves out by
// saving wrong settings, with no way back without SSH.
//
// Token-level errors (token invalid, score too low, expired) and
// network errors (5xx, timeout) stay non-config so they continue to
// block login as configured.
var ErrConfig = errors.New("captcha: misconfigured")

func newConfigErr(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrConfig}, args...)...)
}

// Config is the panel-side captcha configuration. Loaded from the
// settings table on every login (cheap; SQLite read).
type Config struct {
	Provider       string  `json:"provider"`        // none | recaptcha | turnstile
	SiteKey        string  `json:"siteKey"`         // public, sent to browser
	Secret         string  `json:"secret"`          // turnstile secret OR reCAPTCHA secret
	ScoreThreshold float64 `json:"scoreThreshold"`  // reCAPTCHA score threshold (v3 / Enterprise tokens)
}

var httpClient = &http.Client{Timeout: 8 * time.Second}

// Verify checks the supplied token against the configured provider.
// Returns nil on success; a descriptive error on any failure.
//
// remoteIP is forwarded to the provider for risk signals; "" is fine.
func Verify(ctx context.Context, cfg Config, token, remoteIP string) error {
	switch cfg.Provider {
	case "", ProviderNone:
		return nil
	case ProviderTurnstile:
		return verifyTurnstile(ctx, cfg, token, remoteIP)
	case ProviderRecaptcha:
		return verifyRecaptcha(ctx, cfg, token, remoteIP)
	default:
		return fmt.Errorf("captcha: unknown provider %q", cfg.Provider)
	}
}

// Test calls Verify with a deliberately-invalid token. The goal is
// to surface ErrConfig issues at admin save time so they don't get
// caught at the next login. Any other Verify error means "config OK,
// but the dummy token (predictably) failed" — Test returns nil for
// those because that's the success case.
//
// Caveat: neither Cloudflare's siteverify nor Google's siteverify
// receives the Site Key in the request body — it's encoded in the
// real token. So Test cannot detect a wrong Site Key on its own;
// the frontend renders the widget separately to cover that.
//
// For reCAPTCHA the picture is worse than for Turnstile: Google
// short-circuits on the response token before checking the secret,
// so a fake token always returns "invalid-input-response" regardless
// of secret correctness. Use TestWithRealToken when the frontend
// can supply a fresh widget-issued token — that catches both the
// wrong-secret and the secret/site-key-mismatch cases.
func Test(ctx context.Context, cfg Config) error {
	if cfg.Provider == "" || cfg.Provider == ProviderNone {
		return nil
	}
	err := Verify(ctx, cfg, "taps-test-token-not-real", "")
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrConfig) {
		return err
	}
	return nil
}

// TestWithRealToken runs a "real" verify with a fresh widget-issued
// token. Unlike Test(), it can fully validate reCAPTCHA — when both
// site key and secret are right, the upstream returns success=true.
// Any failure is reported back as ErrConfig because we know the
// token is freshly generated (not expired, not duplicate, not user-
// abandoned), so the only realistic causes are wrong keys.
//
// `action` is the action the frontend used for grecaptcha.execute /
// turnstile callback; the Verify path normally enforces "login" but
// in test mode we accept whatever action the test page chose.
func TestWithRealToken(ctx context.Context, cfg Config, token, action string) error {
	if cfg.Provider == "" || cfg.Provider == ProviderNone {
		return nil
	}
	if token == "" {
		return errors.New("captcha: test token required")
	}
	// Override action enforcement for this verify by stashing the
	// expected action via a side channel. Simpler: do the verify
	// inline here since the upstream calls are short.
	switch cfg.Provider {
	case ProviderTurnstile:
		err := verifyTurnstile(ctx, cfg, token, "")
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrConfig) {
			return err
		}
		// With a fresh token, anything else (rejected, score) is also
		// effectively a config issue worth surfacing to the admin.
		return newConfigErr(strings.TrimPrefix(err.Error(), "captcha: "))
	case ProviderRecaptcha:
		return testRecaptcha(ctx, cfg, token, action)
	}
	return fmt.Errorf("captcha: unknown provider %q", cfg.Provider)
}

// testRecaptcha is verifyRecaptcha with the action enforcement +
// the success-flag-only-fails-on-config interpretation. Token-level
// errors with a fresh token mean the secret can't decrypt a token
// generated by the configured site key — that's a misconfig.
func testRecaptcha(ctx context.Context, cfg Config, token, action string) error {
	if cfg.SiteKey == "" {
		return newConfigErr("recaptcha needs Site Key")
	}
	if cfg.Secret == "" {
		return newConfigErr("recaptcha needs Secret key")
	}
	form := url.Values{}
	form.Set("secret", cfg.Secret)
	form.Set("response", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.recaptcha.net/recaptcha/api/siteverify",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("captcha: recaptcha request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("captcha: recaptcha network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var r recaptchaResp
	if err := json.Unmarshal(body, &r); err != nil {
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("captcha: recaptcha upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("captcha: recaptcha bad response: %w", err)
	}
	if !r.Success {
		// With a fresh widget token, success=false almost always
		// means: secret doesn't match the site key, or the site
		// key is invalid for our hostname. Either way: misconfig.
		joined := strings.Join(r.ErrorCodes, ",")
		return newConfigErr("recaptcha rejected (likely site key / secret mismatch): %s", joined)
	}
	// Success — keys are coherent. Action mismatch isn't a key
	// problem so we don't reject for it here.
	if action != "" && r.Action != "" && r.Action != action {
		// Just informational; not failing the test.
	}
	return nil
}

// ----- Cloudflare Turnstile -----
//
// https://developers.cloudflare.com/turnstile/get-started/server-side-validation/

type turnstileResp struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	Action      string   `json:"action"`
}

func verifyTurnstile(ctx context.Context, cfg Config, token, remoteIP string) error {
	if cfg.Secret == "" {
		return newConfigErr("turnstile secret not configured")
	}
	if token == "" {
		return errors.New("captcha: empty token")
	}
	form := url.Values{}
	form.Set("secret", cfg.Secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("captcha: turnstile request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("captcha: turnstile network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// Parse the body even on non-2xx — Cloudflare returns HTTP 400
	// with an error-codes JSON body for invalid-input-secret etc.
	var r turnstileResp
	jsonErr := json.Unmarshal(body, &r)
	if jsonErr != nil && resp.StatusCode/100 != 2 {
		return fmt.Errorf("captcha: turnstile upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if jsonErr != nil {
		return fmt.Errorf("captcha: turnstile bad response: %w", jsonErr)
	}
	if !r.Success {
		// Cloudflare returns these codes for admin-misconfig:
		//   invalid-input-secret  - the secret key is wrong
		//   missing-input-secret  - we forgot to send it (panel bug)
		//   sitekey-secret-mismatch - secret belongs to a different widget
		//   hostname-mismatch - panel host not in CF allow-list for this key
		// Anything else (invalid-input-response, timeout-or-duplicate,
		// challenge-failed) is a token-level / user-side issue.
		joined := strings.Join(r.ErrorCodes, ",")
		for _, c := range r.ErrorCodes {
			switch c {
			case "invalid-input-secret", "missing-input-secret", "sitekey-secret-mismatch", "hostname-mismatch":
				return newConfigErr("turnstile rejected: %s", joined)
			}
		}
		return fmt.Errorf("captcha: turnstile rejected: %s", joined)
	}
	return nil
}

// ----- reCAPTCHA (v2 / v3 / Enterprise tokens) -----
//
// All three token types are accepted by the legacy siteverify endpoint:
//   https://www.recaptcha.net/recaptcha/api/siteverify
// .net is used instead of .google.com so installs in mainland China
// can still reach the endpoint. Enterprise keys must be created with
// the "Use legacy keys" option for this to work — that gives admins
// a Secret key in the GCP Console alongside the Key ID.

type recaptchaResp struct {
	Success     bool     `json:"success"`
	Score       float64  `json:"score"`        // present for v3 / Enterprise
	Action      string   `json:"action"`       // present for v3 / Enterprise
	Hostname    string   `json:"hostname"`
	ChallengeTS string   `json:"challenge_ts"`
	ErrorCodes  []string `json:"error-codes"`
}

func verifyRecaptcha(ctx context.Context, cfg Config, token, remoteIP string) error {
	if cfg.SiteKey == "" {
		return newConfigErr("recaptcha needs Site Key")
	}
	if cfg.Secret == "" {
		return newConfigErr("recaptcha needs Secret key")
	}
	if token == "" {
		return errors.New("captcha: empty token")
	}
	form := url.Values{}
	form.Set("secret", cfg.Secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.recaptcha.net/recaptcha/api/siteverify",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("captcha: recaptcha request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("captcha: recaptcha network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var r recaptchaResp
	if err := json.Unmarshal(body, &r); err != nil {
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("captcha: recaptcha upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("captcha: recaptcha bad response: %w", err)
	}
	if !r.Success {
		joined := strings.Join(r.ErrorCodes, ",")
		for _, c := range r.ErrorCodes {
			switch c {
			case "missing-input-secret", "invalid-input-secret":
				return newConfigErr("recaptcha rejected: %s", joined)
			}
		}
		return fmt.Errorf("captcha: recaptcha rejected: %s", joined)
	}
	// v3 / Enterprise tokens carry score + action; v2 doesn't (Score=0).
	// Threshold check applies only when a score is reported.
	if r.Score > 0 || r.Action != "" {
		if r.Action != "" && r.Action != "login" {
			return fmt.Errorf("captcha: recaptcha action mismatch: got %q, want login", r.Action)
		}
		threshold := cfg.ScoreThreshold
		if threshold <= 0 {
			threshold = 0.5
		}
		if r.Score < threshold {
			return fmt.Errorf("captcha: recaptcha score %.2f below threshold %.2f", r.Score, threshold)
		}
	}
	return nil
}
