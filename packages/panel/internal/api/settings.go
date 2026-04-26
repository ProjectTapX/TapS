package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/alerts"
	"github.com/taps/panel/internal/captcha"
	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/loglimit"
	"github.com/taps/panel/internal/model"
	"github.com/taps/panel/internal/secrets"
	"github.com/taps/panel/internal/serverdeploy"
	"github.com/taps/panel/internal/sso"
	"github.com/taps/shared/protocol"
)

type SettingsHandler struct {
	Alerts       *alerts.Dispatcher
	DB           *gorm.DB
	Reg          *daemonclient.Registry
	LogLimit     *loglimit.Manager
	AuthLimiters *AuthLimiters // shared with AuthHandler + auth middleware
	Limits       *LiveLimits   // request-size caps, hot-reloaded
	SSOManager   *sso.Manager  // for publicUrl hot-update
	CSP          *LiveCSP      // CSP domain allowlists, hot-reloaded
	// Cipher is wired via AttachSSO (same lifecycle window as the
	// SSO manager) so captcha admin endpoints can encrypt the captcha
	// provider secret at rest, mirroring how SSO clientSecret works.
	Cipher *secrets.Cipher
}

type webhookBody struct {
	URL          string `json:"url"`
	AllowPrivate bool   `json:"allowPrivate"`
}

func (h *SettingsHandler) GetWebhook(c *gin.Context) {
	c.JSON(http.StatusOK, webhookBody{
		URL:          h.Alerts.URL(),
		AllowPrivate: h.Alerts.AllowPrivate(),
	})
}

func (h *SettingsHandler) SetWebhook(c *gin.Context) {
	var b webhookBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	// Persist allowPrivate first so SetURL's validation sees the new
	// value — admin who flips the override and immediately saves a
	// localhost URL gets the expected accept-once flow.
	if err := h.Alerts.SetAllowPrivate(b.AllowPrivate); err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	if err := h.Alerts.SetURL(b.URL); err != nil {
		switch {
		case errors.Is(err, alerts.ErrWebhookInvalidScheme):
			apiErr(c, http.StatusBadRequest, "settings.webhook_invalid_scheme",
				"webhook url must start with http:// or https://")
		case errors.Is(err, alerts.ErrWebhookInvalidHost):
			apiErr(c, http.StatusBadRequest, "settings.webhook_invalid_host",
				"webhook url must include a host")
		case errors.Is(err, alerts.ErrWebhookPrivateHost):
			apiErr(c, http.StatusBadRequest, "settings.webhook_private_blocked",
				"webhook url points to a private/loopback/link-local address; an admin can enable webhook.allowPrivate to override")
		case errors.Is(err, alerts.ErrWebhookDNSFailed):
			apiErr(c, http.StatusBadRequest, "settings.webhook_dns_failed",
				"webhook url host failed DNS resolution; check the hostname or retry once your resolver is healthy")
		default:
			apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SettingsHandler) TestWebhook(c *gin.Context) {
	h.Alerts.Notify("test", map[string]any{"message": "TapS webhook test"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ----- hibernation defaults + favicon -----
//
// All four defaults plus the PNG favicon live in the panel's settings
// table (key/value blob). When admin saves we push the bundle to every
// connected daemon so its in-memory hibernation manager can act
// immediately without waiting for a restart or per-instance push.

const (
	hibKeyEnabled = "hibernation.defaultEnabled"
	hibKeyMinutes = "hibernation.defaultMinutes"
	hibKeyWarmup  = "hibernation.warmupMinutes"
	hibKeyMOTD    = "hibernation.motd"
	hibKeyKick    = "hibernation.kick"
	hibKeyIcon    = "hibernation.iconB64"

	// Server-deploy source: "fastmirror" (default, China-friendly) or
	// "official" (Mojang / PaperMC / etc. — only works when the panel
	// has unrestricted internet to those upstreams).
	deployKeySource = "deploy.source"

	// Captcha settings — public siteKey is exposed via an unauth
	// endpoint so the login page can render the widget; secret stays
	// admin-only.
	captchaKeyProvider   = "captcha.provider" // "none" | "recaptcha" | "turnstile"
	captchaKeySiteKey    = "captcha.siteKey"
	captchaKeySecret     = "captcha.secret"     // legacy plaintext; read-fallback only
	captchaKeySecretEnc  = "captcha.secretEnc"  // base64(nonce || AES-GCM(secret))
	captchaKeyScore      = "captcha.scoreMin"   // recaptcha v3 / Enterprise score threshold

	// Brand: a customizable display name + favicon. Both are read by
	// the public /api/brand endpoint at every page load so the login
	// page can show them before any auth.
	brandKeySiteName    = "brand.siteName"
	brandKeyFaviconB64  = "brand.faviconB64"
	brandKeyFaviconMime = "brand.faviconMime"
)

type hibSettings struct {
	DefaultEnabled bool   `json:"defaultEnabled"`
	DefaultMinutes int    `json:"defaultMinutes"`
	WarmupMinutes  int    `json:"warmupMinutes"`
	MOTD           string `json:"motd"`
	KickMessage    string `json:"kickMessage"`
	HasIcon        bool   `json:"hasIcon"`
}

func (h *SettingsHandler) loadHib() hibSettings {
	out := hibSettings{
		DefaultEnabled: true,
		DefaultMinutes: 60,
		WarmupMinutes:  5,
		MOTD:           "§e§l[休眠中] §r§7服务器无人在线已休眠,连接以唤醒",
		KickMessage:    "§e服务器正在启动中,请约 30 秒后重新连接",
	}
	get := func(k string) string {
		var s model.Setting
		if err := h.DB.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	if v := get(hibKeyEnabled); v != "" {
		out.DefaultEnabled = v == "true"
	}
	if v := get(hibKeyMinutes); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			out.DefaultMinutes = n
		}
	}
	if v := get(hibKeyWarmup); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			out.WarmupMinutes = n
		}
	}
	if v := get(hibKeyMOTD); v != "" {
		out.MOTD = v
	}
	if v := get(hibKeyKick); v != "" {
		out.KickMessage = v
	}
	out.HasIcon = get(hibKeyIcon) != ""
	return out
}

func (h *SettingsHandler) iconBytes() []byte {
	var s model.Setting
	if err := h.DB.First(&s, "key = ?", hibKeyIcon).Error; err != nil {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		return nil
	}
	return b
}

func (h *SettingsHandler) GetHib(c *gin.Context) {
	c.JSON(http.StatusOK, h.loadHib())
}

type hibPutBody struct {
	DefaultEnabled bool   `json:"defaultEnabled"`
	DefaultMinutes int    `json:"defaultMinutes"`
	WarmupMinutes  int    `json:"warmupMinutes"`
	MOTD           string `json:"motd"`
	KickMessage    string `json:"kickMessage"`
}

func (h *SettingsHandler) SetHib(c *gin.Context) {
	var b hibPutBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.DefaultMinutes < 1 || b.DefaultMinutes > 1440 {
		apiErr(c, http.StatusBadRequest, "settings.default_minutes_range", "defaultMinutes must be between 1 and 1440")
		return
	}
	if b.WarmupMinutes < 0 || b.WarmupMinutes > 60 {
		apiErr(c, http.StatusBadRequest, "settings.warmup_minutes_range", "warmupMinutes must be between 0 and 60")
		return
	}
	save := func(k, v string) {
		h.DB.Save(&model.Setting{Key: k, Value: v})
	}
	save(hibKeyEnabled, strconv.FormatBool(b.DefaultEnabled))
	save(hibKeyMinutes, strconv.Itoa(b.DefaultMinutes))
	save(hibKeyWarmup, strconv.Itoa(b.WarmupMinutes))
	save(hibKeyMOTD, b.MOTD)
	save(hibKeyKick, b.KickMessage)
	h.pushAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SetHibIcon accepts a PNG via multipart, validates dimensions
// (Minecraft requires exactly 64×64), persists base64-encoded.
func (h *SettingsHandler) SetHibIcon(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		apiErr(c, http.StatusBadRequest, "fs.missing_file_field", "missing file field")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 32*1024)) // hard cap 32 KB
	if err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		apiErr(c, http.StatusBadRequest, "settings.invalid_png", "not a valid PNG")
		return
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 64 {
		apiErr(c, http.StatusBadRequest, "settings.icon_dimensions", "icon must be exactly 64×64 pixels (Minecraft requirement)")
		return
	}
	h.DB.Save(&model.Setting{Key: hibKeyIcon, Value: base64.StdEncoding.EncodeToString(raw)})
	h.pushAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SettingsHandler) DeleteHibIcon(c *gin.Context) {
	h.DB.Where("key = ?", hibKeyIcon).Delete(&model.Setting{})
	h.pushAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetHibIcon returns the raw PNG (or 404 if no icon set) so the admin UI
// can preview it without going through base64 in JSON.
func (h *SettingsHandler) GetHibIcon(c *gin.Context) {
	b := h.iconBytes()
	if len(b) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	maxAge := LoadIconCacheMaxAge(h.DB)
	c.Header("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
	c.Data(http.StatusOK, "image/png", b)
}

// GetDeploySource returns "fastmirror" (default) or "official".
func (h *SettingsHandler) GetDeploySource(c *gin.Context) {
	v := h.deploySource()
	c.JSON(http.StatusOK, gin.H{"source": v})
}

// SetDeploySource accepts {"source":"fastmirror"|"official"}.
func (h *SettingsHandler) SetDeploySource(c *gin.Context) {
	var b struct {
		Source string `json:"source"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	if b.Source != "fastmirror" && b.Source != "official" {
		apiErr(c, http.StatusBadRequest, "settings.deploy_source_invalid", "source must be fastmirror or official")
		return
	}
	h.DB.Save(&model.Setting{Key: deployKeySource, Value: b.Source})
	serverdeploy.SetSource(b.Source)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// deploySource reads the current selection, defaulting to fastmirror.
func (h *SettingsHandler) deploySource() string {
	var s model.Setting
	if err := h.DB.First(&s, "key = ?", deployKeySource).Error; err == nil && s.Value != "" {
		return s.Value
	}
	return "fastmirror"
}

// SyncDeploySourceFromDB pushes the persisted value into the
// serverdeploy package at panel startup so providers see the right
// backend without waiting for the first SetDeploySource call.
func (h *SettingsHandler) SyncDeploySourceFromDB() {
	serverdeploy.SetSource(h.deploySource())
}

// ----- Brand -----
//
// brand.siteName is the human display name shown in the browser
// title, login hero, and sidebar. brand.faviconB64 is the raw
// favicon bytes base64-encoded; brand.faviconMime is its MIME type
// (image/png / image/x-icon / image/svg+xml).

func (h *SettingsHandler) brandSiteName() string {
	var s model.Setting
	if err := h.DB.First(&s, "key = ?", brandKeySiteName).Error; err == nil && s.Value != "" {
		return s.Value
	}
	return "TapS"
}

func (h *SettingsHandler) brandFavicon() (mime string, raw []byte) {
	var m model.Setting
	if err := h.DB.First(&m, "key = ?", brandKeyFaviconMime).Error; err == nil {
		mime = m.Value
	}
	var b model.Setting
	if err := h.DB.First(&b, "key = ?", brandKeyFaviconB64).Error; err == nil && b.Value != "" {
		if dec, err := base64.StdEncoding.DecodeString(b.Value); err == nil {
			raw = dec
		}
	}
	return mime, raw
}

// PublicBrand is open to anyone — used by the login page (pre-auth)
// and by the SPA at boot to set <title> + <link rel="icon"> before
// any user interaction.
func (h *SettingsHandler) PublicBrand(c *gin.Context) {
	mime, _ := h.brandFavicon()
	c.JSON(http.StatusOK, gin.H{
		"siteName":    h.brandSiteName(),
		"hasFavicon":  mime != "",
		"faviconMime": mime,
	})
}

// PublicBrandFavicon serves the actual favicon bytes. Open URL so
// <link rel="icon"> works and the browser can cache normally.
func (h *SettingsHandler) PublicBrandFavicon(c *gin.Context) {
	mime, raw := h.brandFavicon()
	if mime == "" || len(raw) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, mime, raw)
}

type brandPutBody struct {
	SiteName string `json:"siteName"`
}

// SetBrandSiteName saves just the display name. Validation matches
// what the settings page enforces client-side: only letters, digits,
// CJK characters, and common ASCII / fullwidth punctuation are
// allowed. Total weight ≤ 16 where a CJK rune counts as 2 (so the
// limit is effectively 16 ASCII chars or 8 Chinese chars).
func (h *SettingsHandler) SetBrandSiteName(c *gin.Context) {
	var b brandPutBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	name := strings.TrimSpace(b.SiteName)
	if name == "" {
		name = "TapS"
	}
	if !validSiteName(name) {
		apiErr(c, http.StatusBadRequest, "settings.site_name_charset", "siteName: only letters / digits / CJK / common punctuation allowed")
		return
	}
	if siteNameWeight(name) > 16 {
		apiErr(c, http.StatusBadRequest, "settings.site_name_too_long", "siteName too long (max 16 weight, where 1 CJK = 2)")
		return
	}
	h.DB.Save(&model.Setting{Key: brandKeySiteName, Value: name})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// validSiteName whitelists the character classes the UI exposes:
//   - letters (A-Za-z, plus any Unicode letter, covering CJK)
//   - digits
//   - common ASCII punctuation
//   - fullwidth Chinese punctuation
//   - whitespace inside (trimmed at boundaries) — single spaces only
func validSiteName(s string) bool {
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			continue
		case r == ' ', r == '-', r == '_', r == '.', r == ',', r == '!', r == '?',
			r == ':', r == ';', r == '(', r == ')', r == '[', r == ']',
			r == '@', r == '#', r == '+', r == '*', r == '/', r == '\\',
			r == '\'', r == '"', r == '|', r == '&':
			continue
		case r == '·', r == '~', r == '`', r == '<', r == '>', r == '=':
			continue
		// fullwidth CJK punctuation block (U+3000..U+303F) and the
		// commonly-used CJK symbols.
		case r >= 0x3000 && r <= 0x303F:
			continue
		case r == '。', r == '，', r == '！', r == '？', r == '：', r == '；',
			r == '（', r == '）', r == '【', r == '】',
			r == '“', r == '”', r == '‘', r == '’',
			r == '《', r == '》', r == '、':
			continue
		default:
			return false
		}
	}
	return true
}

// siteNameWeight counts CJK runes as 2 and everything else as 1. The
// goal is a visually-consistent width budget: 16 ASCII chars and 8
// Chinese chars take roughly the same horizontal space in the UI.
func siteNameWeight(s string) int {
	w := 0
	for _, r := range s {
		if isCJK(r) {
			w += 2
		} else {
			w += 1
		}
	}
	return w
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
		return true
	case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana (still wide)
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // Hangul Syllables
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // Halfwidth + Fullwidth Forms
		return true
	case r >= 0x3000 && r <= 0x303F: // CJK punctuation
		return true
	}
	return false
}

// SetBrandFavicon accepts a multipart upload. We accept PNG / ICO
// only (audit-2026-04-25 MED9 dropped SVG: stored SVG would be
// served back via PublicBrandFavicon and a hostile SVG can carry
// inline <script>/<foreignObject>/onload handlers; favicon contexts
// don't need vector graphics, so the simplest defence is to refuse
// the format outright). Client-supplied Content-Type is ignored —
// we always sniff the first bytes ourselves.
func (h *SettingsHandler) SetBrandFavicon(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		apiErr(c, http.StatusBadRequest, "fs.missing_file_field", "missing file field")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 64*1024))
	if err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	// Always sniff — never trust the client's Content-Type. http
	// .DetectContentType reads the first 512 bytes and matches
	// against the WHATWG MIME-sniffing tables, so a renamed
	// `.svg → favicon.png` upload still resolves to image/svg+xml
	// and gets rejected.
	sniffed := http.DetectContentType(raw)
	switch sniffed {
	case "image/png", "image/x-icon", "image/vnd.microsoft.icon":
	default:
		apiErr(c, http.StatusBadRequest, "settings.favicon_format",
			"favicon must be PNG or ICO (SVG is not supported for security reasons)")
		return
	}
	h.DB.Save(&model.Setting{Key: brandKeyFaviconMime, Value: sniffed})
	h.DB.Save(&model.Setting{Key: brandKeyFaviconB64, Value: base64.StdEncoding.EncodeToString(raw)})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteBrandFavicon clears the favicon back to the SPA default.
func (h *SettingsHandler) DeleteBrandFavicon(c *gin.Context) {
	h.DB.Where("key = ?", brandKeyFaviconMime).Delete(&model.Setting{})
	h.DB.Where("key = ?", brandKeyFaviconB64).Delete(&model.Setting{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// LoadCaptcha returns the current captcha config (used by the login
// handler on every login attempt — cheap SQLite reads).
func (h *SettingsHandler) LoadCaptcha() captcha.Config {
	get := func(k string) string {
		var s model.Setting
		if err := h.DB.First(&s, "key = ?", k).Error; err == nil {
			return s.Value
		}
		return ""
	}
	// Prefer the encrypted secret if present (post-N3). Fall back to
	// the legacy plaintext column for one upgrade window so a panel
	// restarting before the migration sentinel runs still finds the
	// secret. The migration in store.go re-encrypts and clears the
	// plaintext on first boot of the new binary.
	secret := ""
	if enc := get(captchaKeySecretEnc); enc != "" && h.Cipher != nil {
		if pt, err := h.Cipher.Decrypt(enc); err == nil {
			secret = pt
		}
	}
	if secret == "" {
		secret = get(captchaKeySecret)
	}
	c := captcha.Config{
		Provider: get(captchaKeyProvider),
		SiteKey:  get(captchaKeySiteKey),
		Secret:   secret,
	}
	if c.Provider == "" {
		c.Provider = captcha.ProviderNone
	}
	// audit-2026-04-25 MED8: distinguish "row missing" (apply default
	// 0.5) from "row stored as 0" (admin explicitly disabled the
	// score gate so every reCAPTCHA token passes regardless of risk
	// score). The previous `<= 0 → 0.5` collapse silently overrode
	// the admin's choice.
	if v := get(captchaKeyScore); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			c.ScoreThreshold = f
		} else {
			c.ScoreThreshold = 0.5
		}
	} else {
		c.ScoreThreshold = 0.5
	}
	return c
}

// MigrateCaptchaSecret runs the one-shot N3 migration: encrypts any
// existing plaintext captcha secret with the supplied Cipher, writes
// it to captcha.secretEnc, and clears the legacy plaintext column.
// Idempotent via a settings-table sentinel so re-runs (panel restart)
// don't double-wrap an already-encrypted value.
func MigrateCaptchaSecret(db *gorm.DB, cipher *secrets.Cipher) error {
	const sentinel = "migration.captcha_secret_encrypted"
	var marker model.Setting
	if err := db.First(&marker, "key = ?", sentinel).Error; err == nil {
		return nil // already migrated
	}
	var plain model.Setting
	if err := db.First(&plain, "key = ?", captchaKeySecret).Error; err != nil || plain.Value == "" {
		// Nothing to migrate; still drop the sentinel so we skip the
		// lookup on every subsequent boot.
		db.Save(&model.Setting{Key: sentinel, Value: "1"})
		return nil
	}
	enc, err := cipher.Encrypt(plain.Value)
	if err != nil {
		return err
	}
	db.Save(&model.Setting{Key: captchaKeySecretEnc, Value: enc})
	db.Save(&model.Setting{Key: captchaKeySecret, Value: ""})
	db.Save(&model.Setting{Key: sentinel, Value: "1"})
	return nil
}

// captchaAdminView is what the admin GET endpoint returns. Mirrors
// the SSO providerView pattern: never echo the secret back, just a
// boolean so the form can render "leave blank to keep existing".
type captchaAdminView struct {
	Provider       string  `json:"provider"`
	SiteKey        string  `json:"siteKey"`
	HasSecret      bool    `json:"hasSecret"`
	ScoreThreshold float64 `json:"scoreThreshold"`
}

// GetCaptchaConfig (admin only) returns the form-friendly view: all
// non-secret fields plus a hasSecret bool. The secret itself is
// encrypted at rest and never returned over the wire — admins who
// need to rotate it submit a new value via PUT (an empty Secret
// keeps the existing one, matching the SSO clientSecret pattern).
func (h *SettingsHandler) GetCaptchaConfig(c *gin.Context) {
	cfg := h.LoadCaptcha()
	c.JSON(http.StatusOK, captchaAdminView{
		Provider:       cfg.Provider,
		SiteKey:        cfg.SiteKey,
		HasSecret:      cfg.Secret != "",
		ScoreThreshold: cfg.ScoreThreshold,
	})
}

// PublicCaptchaConfig is unauthenticated — the login page hits this
// before any token exists. Only ships the public bits.
func (h *SettingsHandler) PublicCaptchaConfig(c *gin.Context) {
	cfg := h.LoadCaptcha()
	c.JSON(http.StatusOK, gin.H{
		"provider": cfg.Provider,
		"siteKey":  cfg.SiteKey,
	})
}

type captchaPutBody struct {
	Provider string `json:"provider"`
	SiteKey  string `json:"siteKey"`
	Secret   string `json:"secret"`
	// audit-2026-04-25 MED8: pointer so the request can distinguish
	// "field omitted (keep current)" from "explicit 0 (disable
	// score gate, accept any reCAPTCHA token)". The previous
	// non-pointer form collapsed both cases into "use default 0.5".
	ScoreThreshold *float64 `json:"scoreThreshold"`
}

func (h *SettingsHandler) SetCaptchaConfig(c *gin.Context) {
	var b captchaPutBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	switch b.Provider {
	case captcha.ProviderNone, captcha.ProviderRecaptcha, captcha.ProviderTurnstile:
	default:
		apiErr(c, http.StatusBadRequest, "settings.captcha_provider_invalid", "provider must be none / recaptcha / turnstile")
		return
	}
	if b.Provider == captcha.ProviderRecaptcha {
		if b.ScoreThreshold != nil && (*b.ScoreThreshold < 0 || *b.ScoreThreshold > 1) {
			apiErr(c, http.StatusBadRequest, "settings.score_threshold_range", "scoreThreshold must be in [0, 1]")
			return
		}
	}
	// For non-none providers we need a SiteKey at minimum. The Secret
	// is only required when the operator hasn't already saved one
	// (mirroring SSO clientSecret: "leave blank to keep existing").
	current := h.LoadCaptcha()
	if b.Provider != captcha.ProviderNone {
		if b.SiteKey == "" {
			apiErr(c, http.StatusBadRequest, "settings.captcha_keys_required", "siteKey and secret are required when provider != none")
			return
		}
		if b.Secret == "" && current.Secret == "" {
			apiErr(c, http.StatusBadRequest, "settings.captcha_keys_required", "siteKey and secret are required when provider != none")
			return
		}
	}
	// audit-2026-04-25 H1: when the admin switches to a *different*
	// provider, the previously-stored secret was issued by a different
	// service and is meaningless under the new provider. Refusing the
	// PUT until they supply a fresh secret prevents two failure modes:
	//   1. The new provider keeps "verifying" with an alien secret —
	//      verify always fails but the admin sees "captcha enabled" in
	//      the UI without knowing why logins broke.
	//   2. The old (potentially leaked) secret remains at-rest in
	//      captcha.secretEnc forever even though no provider can use
	//      it — sensitive material that should have been wiped lingers.
	// We always require a fresh secret on a real provider change. The
	// frontend mirrors this by clearing hasSecret when the provider
	// select diverges from the loaded value.
	providerChanged := b.Provider != current.Provider && b.Provider != captcha.ProviderNone
	if providerChanged && b.Secret == "" {
		apiErr(c, http.StatusBadRequest, "settings.captcha_secret_required_on_provider_change",
			"changing captcha provider requires supplying a fresh secret for the new provider")
		return
	}
	// Pre-encrypt outside the transaction so the cipher cost doesn't
	// hold the SQLite write lock.
	var encrypted string
	if b.Secret != "" {
		if h.Cipher == nil {
			apiErr(c, http.StatusInternalServerError, "common.encrypt_secret",
				"cipher not initialised; cannot persist captcha secret")
			return
		}
		enc, err := h.Cipher.Encrypt(b.Secret)
		if err != nil {
			apiErr(c, http.StatusInternalServerError, "common.encrypt_secret", "encrypt secret: "+err.Error())
			return
		}
		encrypted = enc
	}
	// audit-2026-04-25 H1 + M11: wrap the multi-row settings write in a
	// transaction so a mid-flight crash leaves provider/siteKey/secret
	// consistent with each other (no half-state where the new provider
	// is set but its secret never landed).
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		save := func(k, v string) error {
			return tx.Save(&model.Setting{Key: k, Value: v}).Error
		}
		if err := save(captchaKeyProvider, b.Provider); err != nil {
			return err
		}
		if err := save(captchaKeySiteKey, b.SiteKey); err != nil {
			return err
		}
		// Wipe the old secret material on a real provider change even
		// if the operator's PUT also includes a fresh secret below —
		// belt-and-suspenders so the previous provider's at-rest blob
		// never coexists with the new provider's settings, even for
		// the duration of the transaction.
		if providerChanged {
			if err := save(captchaKeySecretEnc, ""); err != nil {
				return err
			}
		}
		if encrypted != "" {
			if err := save(captchaKeySecretEnc, encrypted); err != nil {
				return err
			}
			// Clear any leftover plaintext from a pre-N3 panel so a
			// manual SQL dump no longer leaks it.
			if err := save(captchaKeySecret, ""); err != nil {
				return err
			}
		}
		if b.ScoreThreshold != nil {
			if err := save(captchaKeyScore, strconv.FormatFloat(*b.ScoreThreshold, 'f', 2, 64)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", "captcha save: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestCaptchaConfig hits the configured upstream and reports back:
// ok=true means the keys reach the provider and are coherent.
//
// The body uses the same shape as SetCaptchaConfig plus an optional
// `token` (a fresh widget-issued token from the admin's browser).
// When `token` is supplied we run a real verify which catches the
// reCAPTCHA case where Google can only validate the secret with a
// real token. Without a token we fall back to the dummy probe
// (good enough for Turnstile, weak for reCAPTCHA).
func (h *SettingsHandler) TestCaptchaConfig(c *gin.Context) {
	var b struct {
		captchaPutBody
		Token  string `json:"token"`
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	cfg := captcha.Config{
		Provider: b.Provider,
		SiteKey:  b.SiteKey,
		Secret:   b.Secret,
	}
	// audit-2026-04-25 MED8: nil threshold = use default; explicit
	// 0 = disable score gate. Map both into the captcha.Config the
	// upstream verifier sees.
	if b.ScoreThreshold != nil {
		cfg.ScoreThreshold = *b.ScoreThreshold
	} else {
		cfg.ScoreThreshold = 0.5
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var err error
	if b.Token != "" {
		err = captcha.TestWithRealToken(ctx, cfg, b.Token, b.Action)
	} else {
		err = captcha.Test(ctx, cfg)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "reason": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}


// so its in-memory hibernation manager rebuilds its fake-server's MOTD
// / favicon / kick message right away (no per-instance push needed).
func (h *SettingsHandler) pushAll() {
	if h.Reg == nil {
		return
	}
	body := h.hibConfigBody()
	h.Reg.Each(func(cli *daemonclient.Client) {
		if !cli.Connected() {
			return
		}
		// Best-effort fire-and-forget; we don't block on responses.
		go func(c *daemonclient.Client) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = c.Call(ctx, protocol.ActionHibernationConfig, json.RawMessage(body))
		}(cli)
	})
}

// PushTo sends the latest hib config to one specific client. Used as the
// daemon (re)connect hook so a freshly-started daemon picks up the
// panel's hib settings immediately, not only after the next user save.
func (h *SettingsHandler) PushTo(cli *daemonclient.Client) {
	if cli == nil {
		return
	}
	body := h.hibConfigBody()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = cli.Call(ctx, protocol.ActionHibernationConfig, json.RawMessage(body))
}

func (h *SettingsHandler) hibConfigBody() []byte {
	hs := h.loadHib()
	cfg := protocol.HibernationConfig{
		DefaultEnabled:     hs.DefaultEnabled,
		DefaultIdleMinutes: hs.DefaultMinutes,
		WarmupMinutes:      hs.WarmupMinutes,
		MOTD:               hs.MOTD,
		KickMessage:        hs.KickMessage,
		IconPNG:            h.iconBytes(),
	}
	body, _ := json.Marshal(cfg)
	return body
}

// ----- Log limits -----
//
// audit_logs and login_logs grow forever otherwise. Admin sets a row
// cap per table; loglimit.Manager runs a background trim every 60s to
// keep them bounded. Defaults: 1M rows each.

type logLimitsBody struct {
	AuditMaxRows int64 `json:"auditMaxRows"`
	LoginMaxRows int64 `json:"loginMaxRows"`
}

func (h *SettingsHandler) GetLogLimits(c *gin.Context) {
	if h.LogLimit == nil {
		c.JSON(http.StatusOK, logLimitsBody{
			AuditMaxRows: loglimit.DefaultAuditMaxRows,
			LoginMaxRows: loglimit.DefaultLoginMaxRows,
		})
		return
	}
	lim := h.LogLimit.Get()
	c.JSON(http.StatusOK, logLimitsBody{
		AuditMaxRows: lim.AuditMaxRows,
		LoginMaxRows: lim.LoginMaxRows,
	})
}

func (h *SettingsHandler) SetLogLimits(c *gin.Context) {
	var b logLimitsBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	// Hard floor at 1k rows so admins can't accidentally torch their
	// own audit history; hard ceiling at 100M to avoid silly inputs.
	if b.AuditMaxRows < 1_000 || b.AuditMaxRows > 100_000_000 ||
		b.LoginMaxRows < 1_000 || b.LoginMaxRows > 100_000_000 {
		apiErr(c, http.StatusBadRequest, "settings.row_caps_range", "row caps must be in [1000, 100000000]")
		return
	}
	auditKey, loginKey := loglimit.Keys()
	h.DB.Save(&model.Setting{Key: auditKey, Value: strconv.FormatInt(b.AuditMaxRows, 10)})
	h.DB.Save(&model.Setting{Key: loginKey, Value: strconv.FormatInt(b.LoginMaxRows, 10)})
	if h.LogLimit != nil {
		h.LogLimit.Reload()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
