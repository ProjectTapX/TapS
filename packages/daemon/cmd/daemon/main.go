package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/taps/daemon/internal/backup"
	"github.com/taps/daemon/internal/config"
	"github.com/taps/daemon/internal/deploy"
	dfs "github.com/taps/daemon/internal/fs"
	"github.com/taps/daemon/internal/hibernation"
	"github.com/taps/daemon/internal/instance"
	"github.com/taps/daemon/internal/rpc"
	"github.com/taps/daemon/internal/volumes"
	"github.com/taps/shared/tlscert"
)

func main() {
	cfg := config.Load()
	bus := instance.NewBus()
	// All file roots live under <DataDir>/files so the file manager UI, the
	// instance working directory, and the backup module operate on the same
	// tree.
	filesRoot := filepath.Join(cfg.DataDir, "files")
	mgr := instance.NewManager(filesRoot, bus)
	if err := mgr.Load(); err != nil {
		log.Fatalf("load instances: %v", err)
	}
	fs := dfs.New()
	if err := fs.Mount("files", filesRoot, false); err != nil {
		log.Fatalf("mount files: %v", err)
	}
	if err := fs.Mount("data", filepath.Join(cfg.DataDir, "volumes"), true); err != nil {
		log.Fatalf("mount data: %v", err)
	}
	bk := backup.New(filesRoot, filepath.Join(cfg.DataDir, "backups"))
	vm := volumes.New(cfg.DataDir)
	// audit-2026-04-25 MED3: mount synchronously before the listener
	// goes up. The previous `go vm.MountAll()` raced new RPC traffic
	// against partially-mounted volumes — fs.write to /data/<vol>/...
	// would land on the empty mount-point dir instead of the underlying
	// ext4 image, silently shadowing data once the real mount finished.
	// Per-volume failures inside MountAll are already independent and
	// non-fatal; this only orders "all attempts complete" before serving.
	vm.MountAll()
	mgr.SetRequireDocker(cfg.RequireDocker)
	mgr.SetVolumes(vm)
	// audit-2026-04-25 MED1: wire the volumes root so backup.Restore can
	// refuse a workingDir that lives outside both managed roots even
	// when the panel layer was attacker-controlled.
	bk.SetVolumesRoot(vm.Root())
	// Wire the hibernation manager: SLP poller + idle watcher + fake
	// listener for wake-on-connect. Daemon-only feature, completely
	// independent of panel availability.
	hib := hibernation.New(&hibProvider{mgr: mgr})
	mgr.SetHibernation(hib)
	hib.Start()
	// Route backups into the instance's own data volume when one exists, so
	// the zips count against the configured disk limit.
	bk.SetDirResolver(func(uuid string) string {
		it, ok := mgr.Get(uuid)
		if !ok {
			return ""
		}
		c := it.Config()
		if c.ManagedVolume != "" {
			return filepath.Join(vm.Root(), c.ManagedVolume, ".taps-backups")
		}
		if c.AutoDataDir {
			return filepath.Join(vm.Root(), instance.InstanceDirName(uuid), ".taps-backups")
		}
		return ""
	})
	srv := rpc.New(cfg, mgr, fs, bk, vm, hib, deploy.New(mgr, vm))

	// Self-signed TLS material lives next to the token. Generated on
	// first boot, persisted to data/{cert,key}.pem, presented on every
	// connection. The panel pins by SHA-256 fingerprint (TOFU on first
	// add, then enforced) so we never need a real CA.
	cert, fresh, err := tlscert.LoadOrCreate(cfg.DataDir)
	if err != nil {
		log.Fatalf("tls cert: %v", err)
	}
	srv.CertPEM = cert.PEM
	if fresh {
		log.Printf("tls: generated fresh self-signed cert (99-year validity)")
	}
	log.Printf("daemon listening on %s (wss/https)", cfg.Addr)
	log.Printf("daemon mounts: /files=%s  /data=%s", filesRoot, filepath.Join(cfg.DataDir, "volumes"))
	log.Printf("daemon docker-only: %v   volumes available: %v", cfg.RequireDocker, vm.Available())
	log.Printf("daemon token: %s", cfg.Token)
	log.Printf("daemon tls fingerprint: %s", cert.Fingerprint)

	go mgr.AutoStartAll()

	srvHTTP := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert.Cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: time.Duration(cfg.HTTPReadHeaderTimeoutSec) * time.Second,
		ReadTimeout:       time.Duration(cfg.HTTPReadTimeoutSec) * time.Second,
		WriteTimeout:      time.Duration(cfg.HTTPWriteTimeoutSec) * time.Second,
		IdleTimeout:       time.Duration(cfg.HTTPIdleTimeoutSec) * time.Second,
	}

	// audit-2026-04-25 MED4: graceful shutdown. SIGTERM/SIGINT cancels
	// the root context, the HTTP server stops accepting new
	// connections and waits up to 30s for in-flight ones, then we
	// stop the hibernation manager and unmount any managed loopback
	// volumes so a systemctl restart leaves a clean state on disk.
	// systemd unit pairs this with TimeoutStopSec=30s.
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stopCh
		log.Printf("daemon: received signal %v, beginning graceful shutdown", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srvHTTP.Shutdown(ctx); err != nil {
			log.Printf("daemon: http shutdown: %v", err)
		}
	}()

	if err := srvHTTP.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Printf("daemon: http server stopped, tearing down state")
	hib.Shutdown()
	if err := vm.UnmountAll(); err != nil {
		log.Printf("daemon: volumes unmount: %v", err)
	}
	log.Printf("daemon: clean shutdown complete")
}
