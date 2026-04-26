package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/taps/panel/internal/alerts"
	"github.com/taps/panel/internal/api"
	"github.com/taps/panel/internal/config"
	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/loglimit"
	"github.com/taps/panel/internal/model"
	"github.com/taps/panel/internal/monitorhist"
	"github.com/taps/panel/internal/scheduler"
	"github.com/taps/panel/internal/secrets"
	"github.com/taps/panel/internal/sso"
	"github.com/taps/panel/internal/store"
)

func main() {
	// Subcommand dispatch BEFORE we open the listener. Used for
	// out-of-band recovery from a locked-out config — e.g. an admin
	// switched the panel to oidc-only and then their IdP broke.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "reset-auth-method":
			runResetAuthMethod(os.Args[2:])
			return
		case "-h", "--help", "help":
			printHelp()
			return
		}
	}

	cfg := config.Load()
	db, err := store.Open(cfg)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	reg := daemonclient.NewRegistry(db)
	if err := reg.LoadAll(); err != nil {
		log.Printf("load daemons: %v", err)
	}
	sched := scheduler.New(db, reg)
	if err := sched.Start(); err != nil {
		log.Fatalf("start scheduler: %v", err)
	}
	defer sched.Stop()

	hist := monitorhist.New(reg)
	hist.Start()
	defer hist.Stop()

	logCap := loglimit.New(db)
	logCap.Start()
	defer logCap.Stop()

	al := alerts.New(db)
	// Webhook is admin-only configuration, so the events we ship are
	// admin-relevant infra signals — node going offline, not per-
	// instance crashes (those are user-facing and end up as toasts /
	// status badges in the SPA, plus written to monitor history).
	//
	// Debounce: real outages are typically multi-minute; transient
	// network blips reconnect within a few seconds. Wait 60s after a
	// disconnect before firing — if the daemon reconnects in that
	// window, cancel the pending fire so we don't page on flapping
	// links. Reconnect within the window also issues a "node.online"
	// recovery notification so the admin knows the issue cleared.
	var (
		dbMu       sync.Mutex
		debouncers = map[uint]*time.Timer{}
		notified   = map[uint]bool{}
	)
	const offlineDebounce = 60 * time.Second
	reg.AddDisconnectHook(func(c *daemonclient.Client) {
		dbMu.Lock()
		defer dbMu.Unlock()
		if t, ok := debouncers[c.ID()]; ok {
			t.Stop()
		}
		id := c.ID()
		var d model.Daemon
		_ = db.Select("id", "name", "address", "display_host").First(&d, id).Error
		name := d.Name
		address := d.Address
		debouncers[id] = time.AfterFunc(offlineDebounce, func() {
			dbMu.Lock()
			notified[id] = true
			delete(debouncers, id)
			dbMu.Unlock()
			al.Notify("node.offline", map[string]any{
				"daemonId": id,
				"name":     name,
				"address":  address,
			})
		})
	})
	reg.AddConnectHook(func(c *daemonclient.Client) {
		dbMu.Lock()
		defer dbMu.Unlock()
		id := c.ID()
		if t, ok := debouncers[id]; ok {
			t.Stop()
			delete(debouncers, id)
		}
		// Only send a recovery notice if we previously paged about
		// this node being offline — otherwise a fresh start would
		// send a confusing "back online" without a paired "offline".
		if notified[id] {
			delete(notified, id)
			var d model.Daemon
			_ = db.Select("id", "name", "address").First(&d, id).Error
			al.Notify("node.online", map[string]any{
				"daemonId": id,
				"name":     d.Name,
				"address":  d.Address,
			})
		}
	})

	r, authedGroup, admGroup := api.NewRouter(cfg, db, reg, sched, hist, al, logCap)

	// SSO / OIDC: shared cipher for at-rest client_secret encryption,
	// and a manager that runs the OIDC handshake. Both are also
	// reachable from the api package for admin CRUD endpoints (Day 2).
	sec, err := secrets.LoadOrCreate(cfg.DataDir)
	if err != nil {
		log.Fatalf("secrets cipher: %v", err)
	}
	// SSO state HMAC key — kept separate from cfg.JWTSecret so a JWT
	// secret rotation (or a leak of either key) doesn't compromise both
	// the user-session signing and the OIDC state CSRF/PKCE binding.
	ssoStateKey, err := secrets.LoadOrCreateSSOStateKey(cfg.DataDir)
	if err != nil {
		log.Fatalf("sso state key: %v", err)
	}
	ssoMgr := sso.NewManager(db, sec, ssoStateKey, api.LoadPanelPublicURL(db))
	// Apply DoS-mitigation cap from settings (or default 10000) before
	// any flow can populate the in-memory PKCE store. ApplyOAuthStart
	// is wired the same way via NewAuthLimiters → loadOAuthStartRateLimit.
	ssoMgr.SetPKCEMaxEntries(api.LoadPkceStoreMax(db))
	api.AttachSSO(r, authedGroup, admGroup, db, cfg, ssoMgr, sec)
	// One-shot migration (audit N3): if the legacy plaintext captcha
	// secret is still on disk, encrypt it with the freshly-loaded
	// Cipher and clear the plaintext column. Keyed by sentinel so a
	// re-encrypt on every boot doesn't double-wrap a value that's
	// already in secret_enc form.
	if err := api.MigrateCaptchaSecret(db, sec); err != nil {
		log.Printf("captcha secret migration: %v", err)
	}

	// Tell gin which front-end proxy IPs to trust for X-Forwarded-For /
	// X-Real-IP. Without this, c.ClientIP() returns the immediate hop
	// (= 127.0.0.1 when nginx terminates TLS in front of us) and the
	// rate limiter, audit log, and API-key IP whitelist all break.
	if err := r.SetTrustedProxies(api.LoadTrustedProxies(db)); err != nil {
		log.Printf("set trusted proxies: %v (continuing with gin defaults)", err)
	}

	// Allow the listen port to be overridden by a SystemSettings row
	// the admin can edit from the UI. DB > env > built-in default.
	// Changes only take effect on the next start (process can't rebind
	// itself), so the UI shows a "restart required" hint.
	addr := cfg.Addr
	if dbPort := api.LoadPanelPort(db); dbPort > 0 {
		addr = ":" + itoa(dbPort)
	}

	srv := &http.Server{Addr: addr, Handler: r}
	// audit-2026-04-25 MED5: slow-loris defences. Defaults
	// 10/60/120/120s, admin-tunable via /api/admin/settings/http-timeouts
	// (changes need a panel restart — http.Server timeouts are read by
	// the listener loop and can't be hot-swapped). gorilla websocket
	// hijacks the connection, so terminal sessions are not capped by
	// WriteTimeout / IdleTimeout.
	httpTo := api.LoadHTTPTimeouts(db)
	srv.ReadHeaderTimeout = time.Duration(httpTo.ReadHeaderTimeoutSec) * time.Second
	srv.ReadTimeout = time.Duration(httpTo.ReadTimeoutSec) * time.Second
	srv.WriteTimeout = time.Duration(httpTo.WriteTimeoutSec) * time.Second
	srv.IdleTimeout = time.Duration(httpTo.IdleTimeoutSec) * time.Second

	// audit-2026-04-25 MED4: graceful shutdown. SIGTERM/SIGINT triggers
	// http.Shutdown(30s) which lets in-flight handlers finish; the
	// scheduler / monitorhist / loglimit defers above unwind in
	// reverse order after that. systemd unit pairs with TimeoutStopSec=30s.
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stopCh
		log.Printf("panel: received signal %v, beginning graceful shutdown", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("panel: http shutdown: %v", err)
		}
	}()

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		log.Printf("panel listening on %s (HTTPS)", addr)
		if err := srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		log.Printf("panel: clean shutdown complete")
		return
	}
	log.Printf("panel listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Printf("panel: clean shutdown complete")
}

// tiny helper so we don't pull strconv just for this
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [16]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func printHelp() {
	fmt.Fprintln(os.Stderr, `taps-panel — TapS control panel

Usage:
  taps-panel                              run the panel server (default)
  taps-panel reset-auth-method --to MODE  rewrite auth.loginMethod and exit
                                          MODE = password-only | oidc+password | oidc-only
  taps-panel help                         show this help

Recovery example (admin locked out by oidc-only):
  taps-panel reset-auth-method --to password-only`)
}

// runResetAuthMethod rewrites the auth.loginMethod setting from the
// shell. Bypasses the Q1+ guard (which is the whole point — the guard
// is what locks you out if your IdP breaks). Use with care; it leaves
// the SSO providers in place so re-enabling later is one click.
func runResetAuthMethod(args []string) {
	fs := flag.NewFlagSet("reset-auth-method", flag.ExitOnError)
	to := fs.String("to", "", "target mode: password-only | oidc+password | oidc-only")
	// B12: production deployments place the SQLite DB under
	// /var/lib/taps/panel via systemd's Environment=TAPS_DATA_DIR=...
	// The CLI used to swallow that env entirely (it called
	// config.Load() which only honors flags + a different default),
	// silently opening a *new empty* DB at ./data and reporting a
	// successful no-op rewrite. Operators following the docs to
	// recover from an oidc-only lockout would see SUCCESS and reboot
	// only to find the live setting unchanged. Honor TAPS_DATA_DIR
	// when --data-dir isn't given, and echo the resolved directory so
	// the operator can spot a wrong path before it commits.
	dataDirDefault := os.Getenv("TAPS_DATA_DIR")
	if dataDirDefault == "" {
		dataDirDefault = "./data"
	}
	dataDir := fs.String("data-dir", dataDirDefault,
		"panel data directory (defaults to $TAPS_DATA_DIR or ./data)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	switch *to {
	case api.LoginMethodPasswordOnly, api.LoginMethodOIDCPassword, api.LoginMethodOIDCOnly:
	default:
		fmt.Fprintln(os.Stderr, "reset-auth-method: --to must be password-only / oidc+password / oidc-only")
		os.Exit(2)
	}
	cfg := config.Load()
	cfg.DataDir = *dataDir
	cfg.DBPath = filepath.Join(*dataDir, "panel.db")
	fmt.Fprintf(os.Stderr, "reset-auth-method using data dir: %s (db: %s)\n", cfg.DataDir, cfg.DBPath)
	db, err := store.Open(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	prev := api.LoadLoginMethod(db)
	if err := db.Save(&model.Setting{Key: "auth.loginMethod", Value: *to}).Error; err != nil {
		fmt.Fprintf(os.Stderr, "write setting: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("auth.loginMethod: %s -> %s (no panel restart required; takes effect on next login)\n", prev, *to)
}
