package daemonclient

import (
	"encoding/json"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

// EventHook gets called for every event received from any connected daemon.
type EventHook func(daemonID uint, action string, payload []byte)

// ConnectHook gets called once each time a daemon transitions from
// disconnected to connected (initial dial OR every reconnect). Use this to
// re-push panel-owned state (settings, hibernation config, etc.) that the
// daemon doesn't persist itself.
type ConnectHook func(c *Client)

// DisconnectHook fires once whenever an established daemon connection
// drops. The hook runs unconditionally — it is the consumer's job to
// debounce (network blips reconnect within seconds; a true outage is
// usually >60s). Reuses the ConnectHook signature for symmetry.
type DisconnectHook func(c *Client)

// Registry tracks active Daemon connections keyed by panel DB id.
type Registry struct {
	db *gorm.DB

	mu      sync.RWMutex
	clients map[uint]*Client

	hookMu          sync.RWMutex
	hooks           []EventHook
	connectHooks    []ConnectHook
	disconnectHooks []DisconnectHook
}

func NewRegistry(db *gorm.DB) *Registry {
	return &Registry{db: db, clients: map[uint]*Client{}}
}

// AddHook registers a callback fired for every event seen on any daemon.
func (r *Registry) AddHook(h EventHook) {
	r.hookMu.Lock()
	r.hooks = append(r.hooks, h)
	r.hookMu.Unlock()
}

func (r *Registry) callHooks(daemonID uint, action string, payload []byte) {
	r.hookMu.RLock()
	hs := append([]EventHook(nil), r.hooks...)
	r.hookMu.RUnlock()
	for _, h := range hs {
		h(daemonID, action, payload)
	}
}

// AddConnectHook registers a callback fired once per (re)connect of any
// daemon. Hooks run on a fresh goroutine so they may safely block.
func (r *Registry) AddConnectHook(h ConnectHook) {
	r.hookMu.Lock()
	r.connectHooks = append(r.connectHooks, h)
	r.hookMu.Unlock()
}

func (r *Registry) fireConnectHooks(c *Client) {
	r.hookMu.RLock()
	hs := append([]ConnectHook(nil), r.connectHooks...)
	r.hookMu.RUnlock()
	for _, h := range hs {
		go h(c)
	}
}

// AddDisconnectHook registers a callback fired once per drop. Hooks
// run on a fresh goroutine so they may safely block (or sleep, for
// debounce timers).
func (r *Registry) AddDisconnectHook(h DisconnectHook) {
	r.hookMu.Lock()
	r.disconnectHooks = append(r.disconnectHooks, h)
	r.hookMu.Unlock()
}

func (r *Registry) fireDisconnectHooks(c *Client) {
	r.hookMu.RLock()
	hs := append([]DisconnectHook(nil), r.disconnectHooks...)
	r.hookMu.RUnlock()
	for _, h := range hs {
		go h(c)
	}
}

// LoadAll dials all daemons known to the panel DB.
func (r *Registry) LoadAll() error {
	var ds []model.Daemon
	if err := r.db.Find(&ds).Error; err != nil {
		return err
	}
	for _, d := range ds {
		r.Add(d)
	}
	return nil
}

func (r *Registry) Add(d model.Daemon) *Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[d.ID]; ok {
		c.Stop()
		delete(r.clients, d.ID)
	}
	c := New(d.ID, d.Address, d.Token, d.CertFingerprint)
	c.OnConnect = r.fireConnectHooks
	c.OnDisconnect = r.fireDisconnectHooks
	r.clients[d.ID] = c
	go c.Run()
	go r.heartbeat(c)
	go r.subscribeAllEvents(c)
	return c
}

// subscribeAllEvents installs a wildcard subscription so any registered hooks
// see status/output events for every instance on this daemon.
func (r *Registry) subscribeAllEvents(c *Client) {
	c.Subscribe("", func(action string, payload json.RawMessage) {
		r.callHooks(c.ID(), action, []byte(payload))
	})
}

func (r *Registry) Remove(id uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[id]; ok {
		c.Stop()
		delete(r.clients, id)
	}
}

func (r *Registry) Get(id uint) (*Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[id]
	return c, ok
}

// Each invokes fn for each client (snapshot-style; safe to call Get/Add inside).
func (r *Registry) Each(fn func(*Client)) {
	r.mu.RLock()
	cs := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		cs = append(cs, c)
	}
	r.mu.RUnlock()
	for _, c := range cs {
		fn(c)
	}
}

func (r *Registry) heartbeat(c *Client) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for range t.C {
		if !c.Connected() {
			continue
		}
		r.db.Model(&model.Daemon{}).Where("id = ?", c.ID()).Update("last_seen", time.Now())
	}
}
