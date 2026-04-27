// Package hibernation implements idle-shutdown + wake-on-connect for
// Minecraft instances. The high-level flow:
//
//   1. A 60-second poller pings every running instance's SLP endpoint
//      and caches the player count (the dashboard reuses the cache).
//   2. A watcher consumes the cache, increments an idle counter on each
//      sample where count == 0 (and the ping actually succeeded).
//      Failed pings are skipped — a "weird" SLP state never triggers
//      shutdown. The first 5 minutes after a Start() call are also
//      skipped to give the server time to load.
//   3. Once an instance hits its idle threshold, the manager stops the
//      container, waits for the host port to free up, and binds a fake
//      SLP listener that:
//        - answers status requests with the panel-configured MOTD,
//          favicon, and a "Hibernating" version string,
//        - answers login requests by sending a kick packet then
//          asynchronously starting the real container again.
package hibernation

import (
	"context"
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ProjectTapX/TapS/packages/daemon/internal/minecraft"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// Manager coordinates the SLP poller, the per-instance watchers, and
// the fake-server listeners. Construct one per daemon, call Start() at
// boot, and ConfigUpdate() whenever the panel pushes new defaults.
type Manager struct {
	provider InstanceProvider
	mu       sync.Mutex
	cfg      protocol.HibernationConfig
	// players cache, keyed by instance UUID. Populated by the SLP
	// poller; consumed by the dashboard endpoint and the idle watcher.
	cache map[string]protocol.PlayersBrief
	// per-instance state: idle count + last Start() time + active fake
	// listener (so we can close it on manual Start/Stop or on shutdown).
	state map[string]*instState
	stop  chan struct{}
}

type instState struct {
	idleMinutes int
	lastStart   time.Time
	fake        *fakeServer
}

// InstanceProvider is the slice of instance.Manager the hibernation
// code actually needs. Defining it as an interface keeps the import
// graph one-directional (instance package needs no hibernation import).
type InstanceProvider interface {
	List() []protocol.InstanceInfo
	Get(uuid string) (Instance, bool)
	Start(uuid string) error
	Stop(uuid string) error
	SetStatus(uuid string, status protocol.InstanceStatus)
	PersistField(uuid string, mutate func(*protocol.InstanceConfig))
	BaseDir() string
}

// Instance is the subset of *instance.Instance we need.
type Instance interface {
	Config() protocol.InstanceConfig
	Status() protocol.InstanceStatus
}

func New(provider InstanceProvider) *Manager {
	return &Manager{
		provider: provider,
		cache:    map[string]protocol.PlayersBrief{},
		state:    map[string]*instState{},
		stop:     make(chan struct{}),
		cfg: protocol.HibernationConfig{
			DefaultEnabled:     true,
			DefaultIdleMinutes: 60,
			MOTD:               "§e§l[Hibernating] §r§7Server idle — connect to wake",
			KickMessage:        "§eServer is starting, please retry in ~30s",
		},
	}
}

// Start launches the polling and watcher goroutines. Idempotent.
func (m *Manager) Start() {
	go m.pollLoop()
	// Resume any fake listeners that were active when we shut down last.
	for _, info := range m.provider.List() {
		if info.Config.HibernationActive {
			m.spawnFake(info.Config)
		}
	}
}

// Shutdown stops all goroutines and closes any fake listeners. Called
// during a graceful daemon shutdown.
func (m *Manager) Shutdown() {
	close(m.stop)
	m.mu.Lock()
	for _, st := range m.state {
		if st.fake != nil {
			_ = st.fake.Close()
			st.fake = nil
		}
	}
	m.mu.Unlock()
}

// Players returns the cached SLP snapshot used by the dashboard.
func (m *Manager) Players() []protocol.PlayersBrief {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.PlayersBrief, 0, len(m.cache))
	for _, v := range m.cache {
		out = append(out, v)
	}
	return out
}

// ConfigUpdate replaces the in-memory hibernation config (called when
// the panel saves new system settings).
func (m *Manager) ConfigUpdate(cfg protocol.HibernationConfig) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	// Re-render any active fake servers' status payload so changes
	// propagate without waiting for the next wake event.
	m.mu.Lock()
	for _, st := range m.state {
		if st.fake != nil {
			st.fake.UpdateConfig(cfg)
		}
	}
	m.mu.Unlock()
}

// MarkStarted is called by instance.Manager when a Start() succeeds —
// resets the idle counter and starts the warmup grace period.
func (m *Manager) MarkStarted(uuid string) {
	m.mu.Lock()
	st := m.getStateLocked(uuid)
	st.lastStart = time.Now()
	st.idleMinutes = 0
	if st.fake != nil {
		_ = st.fake.Close()
		st.fake = nil
	}
	m.mu.Unlock()
	m.provider.PersistField(uuid, func(c *protocol.InstanceConfig) {
		c.HibernationActive = false
	})
}

// MarkStopped is called when an instance leaves the running state for
// reasons OTHER than the hibernation watcher itself (manual stop,
// crash). Clears any active fake listener.
func (m *Manager) MarkStopped(uuid string) {
	m.mu.Lock()
	if st, ok := m.state[uuid]; ok {
		if st.fake != nil {
			_ = st.fake.Close()
			st.fake = nil
		}
		st.idleMinutes = 0
	}
	m.mu.Unlock()
	m.provider.PersistField(uuid, func(c *protocol.InstanceConfig) {
		c.HibernationActive = false
	})
}

// IsHibernating reports whether a fake listener currently owns the host
// port for an instance.
func (m *Manager) IsHibernating(uuid string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.state[uuid]
	return ok && st.fake != nil
}

// CloseFake synchronously closes the fake SLP listener for one instance
// (if any) so the host port is free for the real container to bind. It
// does NOT touch persisted config or idle counters — the caller is
// expected to follow up with a real Start, whose MarkStarted hook will
// reset state and clear HibernationActive.
//
// Unlike MarkStopped this only takes hib.mu, so it's safe to call from a
// path that already holds an instance's i.mu (e.g. Instance.Start).
func (m *Manager) CloseFake(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.state[uuid]; ok && st.fake != nil {
		_ = st.fake.Close()
		st.fake = nil
	}
}

func (m *Manager) getStateLocked(uuid string) *instState {
	st, ok := m.state[uuid]
	if !ok {
		st = &instState{}
		m.state[uuid] = st
	}
	return st
}

// pollLoop is the 5-second SLP poller + idle watcher loop. Both jobs
// share the same tick because they share the same data. Idle minute
// counters still increment once per minute — see tick() for the divisor.
func (m *Manager) pollLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	// First sweep on startup so the dashboard has data to show.
	m.tick()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.tick()
		}
	}
}

func (m *Manager) tick() {
	infos := m.provider.List()
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	// ----- ping pass -----
	type pingResult struct {
		uuid string
		brief protocol.PlayersBrief
		ok   bool
	}
	results := make([]pingResult, 0, len(infos))
	var resMu sync.Mutex
	var wg sync.WaitGroup
	for _, info := range infos {
		c := info.Config
		if info.Status != protocol.StatusRunning {
			continue
		}
		host, port := mcAddressFor(c)
		if port == 0 {
			continue
		}
		wg.Add(1)
		go func(uuid, host string, port int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()
			r, err := minecraft.Ping(ctx, host, port)
			ok := err == nil && r.Online
			resMu.Lock()
			defer resMu.Unlock()
			if ok {
				results = append(results, pingResult{
					uuid: uuid, ok: true,
					brief: protocol.PlayersBrief{UUID: uuid, Online: true, Count: r.Count, Max: r.Max},
				})
			} else {
				results = append(results, pingResult{uuid: uuid, ok: false})
			}
		}(c.UUID, host, port)
	}
	wg.Wait()

	// ----- merge cache + idle counters -----
	m.mu.Lock()
	// Drop cache entries for instances no longer running so dashboard
	// doesn't hold stale data forever.
	live := map[string]bool{}
	for _, info := range infos {
		live[info.Config.UUID] = true
	}
	for k := range m.cache {
		if !live[k] {
			delete(m.cache, k)
		}
	}
	for _, r := range results {
		if r.ok {
			m.cache[r.uuid] = r.brief
		} else {
			m.cache[r.uuid] = protocol.PlayersBrief{UUID: r.uuid, Online: false}
		}
	}
	// Compute who needs to hibernate, while holding the lock so we don't
	// race the manual Start/Stop path.
	type toHib struct {
		uuid string
		c    protocol.InstanceConfig
	}
	var hibs []toHib
	for _, info := range infos {
		if info.Status != protocol.StatusRunning {
			continue
		}
		c := info.Config
		// Resolve effective enabled / threshold for this instance.
		enabled := cfg.DefaultEnabled
		if c.HibernationEnabled != nil {
			enabled = *c.HibernationEnabled
		}
		if !enabled {
			delete(m.state, c.UUID)
			continue
		}
		idleMin := c.HibernationIdleMinutes
		if idleMin <= 0 {
			idleMin = cfg.DefaultIdleMinutes
		}
		if idleMin <= 0 {
			idleMin = 60
		}
		st := m.getStateLocked(c.UUID)
		// Warmup grace period from last Start. Panel pushes its current
		// hibernation config on every (re)connect, so cfg.WarmupMinutes
		// reflects the user's choice — 0 explicitly means "no warmup".
		warmup := time.Duration(cfg.WarmupMinutes) * time.Minute
		if !st.lastStart.IsZero() && warmup > 0 && time.Since(st.lastStart) < warmup {
			st.idleMinutes = 0
			continue
		}
		// Find this instance's ping result.
		var pr *pingResult
		for i := range results {
			if results[i].uuid == c.UUID {
				pr = &results[i]
				break
			}
		}
		if pr == nil || !pr.ok {
			// SLP failed → don't increment, don't reset; "weird state"
			// is treated as inactive.
			continue
		}
		if pr.brief.Count > 0 {
			st.idleMinutes = 0
			continue
		}
		st.idleMinutes++
		// idleMinutes is now a tick counter (one tick = 5s). Convert the
		// user's idle-minutes threshold to ticks before comparing.
		if st.idleMinutes >= idleMin*12 {
			hibs = append(hibs, toHib{uuid: c.UUID, c: c})
			st.idleMinutes = 0
		}
	}
	m.mu.Unlock()

	for _, h := range hibs {
		go m.hibernate(h.uuid, h.c)
	}
}

func (m *Manager) hibernate(uuid string, c protocol.InstanceConfig) {
	log.Printf("hibernation: idle threshold reached for %s, stopping", uuid)
	if err := m.provider.Stop(uuid); err != nil {
		log.Printf("hibernation: stop failed for %s: %v", uuid, err)
		return
	}
	// Wait 3s for docker to release the host port.
	time.Sleep(3 * time.Second)
	m.spawnFake(c)
}

func (m *Manager) spawnFake(c protocol.InstanceConfig) {
	host, port := mcAddressFor(c)
	if port == 0 {
		log.Printf("hibernation: no host port for %s, cannot spawn fake", c.UUID)
		return
	}
	// Bind 0.0.0.0:<port> so any external connection wakes us, not just
	// 127.0.0.1. host from cfg may be 127.0.0.1 (loopback).
	_ = host
	addr := ":" + strconv.Itoa(port)
	m.mu.Lock()
	cfg := m.cfg
	st := m.getStateLocked(c.UUID)
	m.mu.Unlock()
	fs, err := newFakeServer(addr, cfg, c.UUID, func(uuid string) {
		log.Printf("hibernation: wake-on-connect for %s", uuid)
		// Close listener first so docker can grab the port back, then
		// trigger Start.
		m.mu.Lock()
		if st2, ok := m.state[uuid]; ok && st2.fake != nil {
			_ = st2.fake.Close()
			st2.fake = nil
		}
		m.mu.Unlock()
		if err := m.provider.Start(uuid); err != nil {
			log.Printf("hibernation: wake-start failed for %s: %v", uuid, err)
		}
	})
	if err != nil {
		log.Printf("hibernation: bind %s failed: %v", addr, err)
		return
	}
	m.mu.Lock()
	if st.fake != nil {
		_ = st.fake.Close()
	}
	st.fake = fs
	m.mu.Unlock()
	m.provider.SetStatus(c.UUID, protocol.StatusHibernating)
	m.provider.PersistField(c.UUID, func(cfg *protocol.InstanceConfig) {
		cfg.HibernationActive = true
	})
	log.Printf("hibernation: %s now hibernating on %s", c.UUID, addr)
}

// HibernateNow lets external callers (e.g. an admin button) put an
// instance to sleep right now without waiting for the idle counter.
// Currently unused by the UI but exposed for completeness.
func (m *Manager) HibernateNow(uuid string) error {
	it, ok := m.provider.Get(uuid)
	if !ok {
		return errors.New("not found")
	}
	go m.hibernate(uuid, it.Config())
	return nil
}

// mcAddressFor copy from rpc/server.go — kept here to avoid an import
// cycle (rpc → hibernation → rpc would loop). The function is small.
func mcAddressFor(cfg protocol.InstanceConfig) (string, int) {
	host := cfg.MinecraftHost
	if host == "" {
		host = "127.0.0.1"
	}
	if cfg.MinecraftPort > 0 {
		return host, cfg.MinecraftPort
	}
	for _, spec := range cfg.DockerPorts {
		body := spec
		if i := strings.Index(body, "/"); i >= 0 {
			body = body[:i]
		}
		parts := strings.Split(body, ":")
		var hostStr string
		switch len(parts) {
		case 1:
			hostStr = parts[0]
		case 2:
			hostStr = parts[0]
		case 3:
			hostStr = parts[1]
		default:
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(hostStr)); err == nil && n > 0 {
			return host, n
		}
	}
	return host, 0
}

// quiet the unused import linter when we add net later.
var _ = net.Listen
