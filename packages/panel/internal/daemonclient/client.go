package daemonclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
	"github.com/ProjectTapX/TapS/packages/shared/tlscert"
)

// callTimeout is a safety net so Calls without an explicit ctx deadline don't
// hang forever. Long-running RPCs (e.g. docker.pull, backup.create) should
// pass a context with a longer/no deadline.
const callTimeout = 30 * time.Minute

// EventHandler receives one event for a subscribed instance.
type EventHandler func(action string, payload json.RawMessage)

// subEvent is one queued event for a single subscriber. We use a per-handler
// buffered queue + drain goroutine so a slow handler (e.g. a stalled browser
// terminal WS) cannot block the WS reader and starve `Call` responses.
type subEvent struct {
	action  string
	payload json.RawMessage
}
type subscriber struct {
	uuid string
	user EventHandler
	ch   chan subEvent
	done chan struct{}
}
// Client is a Panel-side connection to one Daemon.
type Client struct {
	id      uint
	address string // host:port
	token   string
	// fingerprint is the SHA-256 colon-hex of the daemon's pinned cert.
	// Held atomically so a hot rotation (admin "re-fetch fingerprint")
	// updates without restarting the client. Empty value disables the
	// pin check (used by the add-daemon TOFU probe and only there).
	fingerprint atomic.Pointer[string]

	mu       sync.Mutex
	conn     *websocket.Conn
	pending  map[string]chan protocol.Message
	handlers map[string]map[uint64]*subscriber // uuid -> handlers
	nextH    uint64
	connected bool
	welcome   protocol.Welcome
	writeM    sync.Mutex

	stopOnce sync.Once
	stopCh   chan struct{}

	// OnConnect, if set, is invoked after every successful (re)handshake so
	// the panel can re-push state the daemon doesn't persist itself
	// (hibernation config, etc.). Called on a goroutine — safe to block.
	OnConnect func(c *Client)
	// OnDisconnect, if set, fires once per drop of an established
	// connection (paired with OnConnect). Used by the alerts layer to
	// debounce-then-notify on node offline. Like OnConnect, dispatched
	// on a fresh goroutine.
	OnDisconnect func(c *Client)
}

func New(id uint, address, token, fingerprint string) *Client {
	c := &Client{
		id:       id,
		address:  address,
		token:    token,
		pending:  map[string]chan protocol.Message{},
		handlers: map[string]map[uint64]*subscriber{},
		stopCh:   make(chan struct{}),
	}
	fp := fingerprint
	c.fingerprint.Store(&fp)
	return c
}

func (c *Client) ID() uint                  { return c.id }
func (c *Client) Address() string           { return c.address }
func (c *Client) Token() string             { return c.token }
func (c *Client) Fingerprint() string {
	if p := c.fingerprint.Load(); p != nil {
		return *p
	}
	return ""
}
// SetFingerprint hot-swaps the pinned fingerprint after an admin
// rotates the daemon's cert. Existing connection is left open (the
// admin already trusts the running session); the new value applies on
// next reconnect.
func (c *Client) SetFingerprint(fp string) {
	c.fingerprint.Store(&fp)
}
func (c *Client) Connected() bool           { c.mu.Lock(); defer c.mu.Unlock(); return c.connected }
func (c *Client) Welcome() protocol.Welcome { c.mu.Lock(); defer c.mu.Unlock(); return c.welcome }

// Run blocks until Stop is called, redialing on disconnect with backoff.
func (c *Client) Run() {
	backoff := time.Second
	for {
		select {
		case <-c.stopCh:
			return
		default:
		}
		err := c.connectAndServe()
		if err != nil {
			log.Printf("daemon[%d] %s: %v", c.id, c.address, err)
		}
		select {
		case <-c.stopCh:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Unlock()
	})
}

// buildTLSConfig returns a tls.Config that disables the standard CA
// chain check (the daemon's cert is self-signed) and replaces it with
// a SHA-256 fingerprint pin against the value the operator confirmed
// when adding the daemon. Empty fingerprint refuses the connection
// outright — TOFU should populate the field before the regular client
// ever runs.
func (c *Client) buildTLSConfig() (*tls.Config, error) {
	pin := c.Fingerprint()
	if pin == "" {
		return nil, errors.New("daemon cert fingerprint not pinned (run TOFU first)")
	}
	return &tls.Config{
		// Self-signed cert + we pin by fingerprint, so chain verification
		// would always fail; do our own check in VerifyPeerCertificate.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("peer presented no certificate")
			}
			got := tlscert.FingerprintFromDER(rawCerts[0])
			if !equalFingerprint(got, pin) {
				return fmt.Errorf("daemon cert fingerprint mismatch: got %s, want %s", got, pin)
			}
			return nil
		},
	}, nil
}

// HTTPClient returns an *http.Client wired with the same TLS pin used
// for the WebSocket. Used by file upload/download/backup proxies in
// the api package — they must enforce the same trust as the WS link.
func (c *Client) HTTPClient(timeout time.Duration) (*http.Client, error) {
	tlsCfg, err := c.buildTLSConfig()
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, nil
}

// equalFingerprint compares two SHA-256 colon-hex fingerprints case
// insensitively. Operators occasionally paste UPPER hex; both forms
// should match.
func equalFingerprint(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func (c *Client) connectAndServe() error {
	tlsCfg, err := c.buildTLSConfig()
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "wss", Host: c.address, Path: "/ws"}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: tlsCfg}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	if err := conn.WriteJSON(protocol.Hello{Token: c.token, Version: "26.1.0"}); err != nil {
		conn.Close()
		return err
	}
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return err
	}
	var welcomeMsg protocol.Message
	if err := json.Unmarshal(raw, &welcomeMsg); err != nil || welcomeMsg.Error != nil {
		conn.Close()
		if welcomeMsg.Error != nil {
			return errors.New(welcomeMsg.Error.Message)
		}
		return errors.New("bad welcome")
	}
	var welcome protocol.Welcome
	_ = json.Unmarshal(welcomeMsg.Payload, &welcome)

	c.mu.Lock()
	c.conn = conn
	c.welcome = welcome
	c.connected = true
	c.mu.Unlock()
	// Audit-2026-04-24-v3 M8: re-emit instanceSubscribe for every uuid
	// the client still has active handlers for. Without this, after
	// any WS drop the daemon side wipes its session map (every
	// `instanceSubscribe(true)` lives only on the active connection)
	// and live terminals / docker-pull progress / status streams go
	// silent until the user manually reloads. Retried 3 times with
	// half-second backoff per uuid, then logged and left for the
	// next reconnect cycle to pick up.
	go c.replaySubscriptions()
	if c.OnConnect != nil {
		go c.OnConnect(c)
	}
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.conn = nil
		// fail any pending calls
		for id, ch := range c.pending {
			ch <- protocol.Message{ID: id, Error: &protocol.Error{Code: "disconnected", Message: "daemon disconnected"}}
			delete(c.pending, id)
		}
		c.mu.Unlock()
		if c.OnDisconnect != nil {
			go c.OnDisconnect(c)
		}
	}()

	conn.SetReadDeadline(time.Time{})
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var msg protocol.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case protocol.TypeResponse:
			c.mu.Lock()
			if ch, ok := c.pending[msg.ID]; ok {
				ch <- msg
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()
		case protocol.TypeEvent:
			c.fanoutEvent(msg)
		}
	}
}

func (c *Client) fanoutEvent(msg protocol.Message) {
	// Events carry the subscribe-key as either `uuid` (instance events) or
	// `pullId` (docker.pullProgress / pullDone) — both serve the same
	// subscribe-fanout role on this side.
	var ev struct {
		UUID   string `json:"uuid"`
		PullID string `json:"pullId"`
	}
	_ = json.Unmarshal(msg.Payload, &ev)
	key := ev.UUID
	if key == "" {
		key = ev.PullID
	}
	c.mu.Lock()
	subs := append([]*subscriber(nil), collect(c.handlers[key])...)
	wild := append([]*subscriber(nil), collect(c.handlers[""])...) // wildcard subscribers
	c.mu.Unlock()
	deliver := func(s *subscriber) {
		// Non-blocking: a slow user handler can never stall the read loop.
		// Overflow drops the event with a log; for terminal output that's a
		// gap of bytes, which is strictly better than the whole panel
		// hanging on stalled browser WS writes.
		select {
		case s.ch <- subEvent{action: msg.Action, payload: msg.Payload}:
		default:
			log.Printf("daemon[%d] subscriber %q queue full, dropped %s", c.id, s.uuid, msg.Action)
		}
	}
	for _, s := range subs {
		deliver(s)
	}
	for _, s := range wild {
		deliver(s)
	}
}

func collect(m map[uint64]*subscriber) []*subscriber {
	out := make([]*subscriber, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	return out
}

// Call sends a request and awaits the response (or callTimeout).
func (c *Client) Call(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, errors.New("daemon not connected")
	}
	id := uuid.NewString()
	ch := make(chan protocol.Message, 1)
	c.pending[id] = ch
	conn := c.conn
	c.mu.Unlock()

	body, _ := json.Marshal(payload)
	req := protocol.Message{ID: id, Type: protocol.TypeRequest, Action: action, Payload: body}

	c.writeM.Lock()
	err := conn.WriteJSON(req)
	c.writeM.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	deadline := time.NewTimer(callTimeout)
	defer deadline.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-deadline.C:
		return nil, errors.New("daemon call timeout")
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Payload, nil
	}
}

// Subscribe registers a handler for events related to one instance uuid.
// Pass empty uuid to receive events for all instances.
func (c *Client) Subscribe(uuid string, h EventHandler) func() {
	s := &subscriber{
		uuid: uuid,
		user: h,
		ch:   make(chan subEvent, 256),
		done: make(chan struct{}),
	}
	go func() {
		for {
			select {
			case <-s.done:
				return
			case ev := <-s.ch:
				s.user(ev.action, ev.payload)
			}
		}
	}()

	c.mu.Lock()
	c.nextH++
	hid := c.nextH
	if c.handlers[uuid] == nil {
		c.handlers[uuid] = map[uint64]*subscriber{}
	}
	c.handlers[uuid][hid] = s
	first := len(c.handlers[uuid]) == 1
	c.mu.Unlock()

	if first {
		// ask the daemon to start streaming
		_, _ = c.Call(context.Background(), protocol.ActionInstanceSubscribe,
			protocol.InstanceSubscribeReq{UUID: uuid, Enabled: true})
	}
	return func() {
		c.mu.Lock()
		if hs, ok := c.handlers[uuid]; ok {
			delete(hs, hid)
			if len(hs) == 0 {
				delete(c.handlers, uuid)
			}
		}
		empty := c.handlers[uuid] == nil
		c.mu.Unlock()
		close(s.done)
		if empty {
			_, _ = c.Call(context.Background(), protocol.ActionInstanceSubscribe,
				protocol.InstanceSubscribeReq{UUID: uuid, Enabled: false})
		}
	}
}

// replaySubscriptions re-emits instanceSubscribe(true) for every uuid
// the client still holds active handlers for. Called from
// connectAndServe right after the WS handshake completes — before
// that point Call() would queue forever or fail. Three retries with
// 500 ms backoff per uuid; on giving up we log and leave it for the
// next reconnect cycle so we don't spin forever on a daemon that's
// in a bad state.
func (c *Client) replaySubscriptions() {
	c.mu.Lock()
	uuids := make([]string, 0, len(c.handlers))
	for uuid := range c.handlers {
		if uuid == "" {
			continue // wildcard — no subscribe needed
		}
		uuids = append(uuids, uuid)
	}
	c.mu.Unlock()
	for _, uuid := range uuids {
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := c.Call(ctx, protocol.ActionInstanceSubscribe,
				protocol.InstanceSubscribeReq{UUID: uuid, Enabled: true})
			cancel()
			if err == nil {
				lastErr = nil
				break
			}
			lastErr = err
			if attempt < 3 {
				time.Sleep(500 * time.Millisecond)
			}
		}
		if lastErr != nil {
			log.Printf("daemonclient: failed to replay subscription for uuid=%s after 3 attempts: %v (will retry on next reconnect)", uuid, lastErr)
		}
	}
}
