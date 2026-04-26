package instance

import (
	"sync"
)

type EventKind string

const (
	EventOutput          EventKind = "output"
	EventStatus          EventKind = "status"
	EventDockerPull      EventKind = "dockerPull"      // payload: string (one line)
	EventDockerPullDone  EventKind = "dockerPullDone"  // payload: string (error or "")
)

type Event struct {
	UUID string
	Kind EventKind
	Data any
}

type subKey struct {
	uuid string
	id   uint64
}

// outputHistoryLimit is the per-instance ring buffer cap for terminal
// scrollback. ~200 KB is enough for several screens of MC-style output
// without bloating daemon memory across many instances.
const outputHistoryLimit = 200 * 1024

// Bus is an in-process pub/sub for instance output and status events.
//
// It also retains the most recent EventOutput payloads per uuid so a newly
// connected terminal can replay history (panel asks via the
// instance.outputHistory action).
type Bus struct {
	mu      sync.RWMutex
	nextID  uint64
	subs    map[subKey]chan Event
	history map[string][]byte
}

func NewBus() *Bus { return &Bus{subs: map[subKey]chan Event{}, history: map[string][]byte{}} }

// Subscribe to events for one uuid (empty uuid subscribes to all). Returns chan and unsubscribe func.
func (b *Bus) Subscribe(uuid string, buf int) (<-chan Event, func()) {
	if buf <= 0 {
		buf = 64
	}
	ch := make(chan Event, buf)
	b.mu.Lock()
	b.nextID++
	k := subKey{uuid: uuid, id: b.nextID}
	b.subs[k] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if c, ok := b.subs[k]; ok {
			delete(b.subs, k)
			close(c)
		}
		b.mu.Unlock()
	}
}

func (b *Bus) Publish(uuid string, kind EventKind, data any) {
	if kind == EventOutput {
		if s, ok := data.(string); ok && s != "" {
			b.appendOutput(uuid, s)
		}
	}
	ev := Event{UUID: uuid, Kind: kind, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for k, ch := range b.subs {
		if k.uuid != "" && k.uuid != uuid {
			continue
		}
		select {
		case ch <- ev:
		default: // drop if subscriber is slow
		}
	}
}

// appendOutput grows the per-uuid ring; oldest bytes are trimmed once the
// buffer would exceed outputHistoryLimit.
func (b *Bus) appendOutput(uuid, s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur := b.history[uuid]
	cur = append(cur, s...)
	if len(cur) > outputHistoryLimit {
		cur = cur[len(cur)-outputHistoryLimit:]
	}
	b.history[uuid] = cur
}

// OutputHistory returns a copy of the retained scrollback for uuid.
func (b *Bus) OutputHistory(uuid string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return string(b.history[uuid])
}

// ClearHistory drops the buffer for uuid (called when an instance is deleted).
func (b *Bus) ClearHistory(uuid string) {
	b.mu.Lock()
	delete(b.history, uuid)
	b.mu.Unlock()
}

