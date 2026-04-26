// Package uploadsession tracks in-flight chunked uploads on the
// daemon. The "init" handshake happens once per upload (the panel
// declares total bytes / chunks / target path), gets back a short-
// lived uploadId, and every subsequent chunk request must carry that
// id. We use this to:
//
//   1. Enforce a per-volume disk quota at init time (refuse uploads
//      that obviously won't fit) and again at each chunk (refuse
//      sessions that are inflating their declared total).
//   2. Pin chunks to one path — chunks for upload A can't be sneaked
//      into upload B's partial file by guessing the destination.
//   3. Garbage-collect partial files for sessions that never call
//      `final=true` within the TTL.
//
// State is purely in-memory; daemon restart drops in-flight sessions
// and the GC cleans the leftover .partial files on next sweep.
package uploadsession

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const (
	// DefaultTTL caps how long an init token stays valid. Long enough
	// for browser uploads of multi-GiB files over a slow connection,
	// short enough that an abandoned session reclaims disk soon.
	DefaultTTL = time.Hour
	// gcInterval is how often the housekeeping loop scans for stale
	// sessions. Cheap (single map walk).
	gcInterval = 5 * time.Minute
)

// Session is one declared upload. PathAbs is the daemon-resolved
// absolute target (post-fs.Resolve), so the chunk handler can compare
// against r.URL "path" without re-doing path traversal checks.
type Session struct {
	ID         string
	PathAbs    string
	TotalBytes int64
	TotalChunks int
	Filename   string

	mu       sync.Mutex
	received int64 // bytes appended to the .partial file so far
	chunks   int   // chunks accepted (just for sanity / diagnostics)
	finished bool
	created  time.Time
	expires  time.Time
}

func (s *Session) Received() int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.received }

// ErrUploadInProgress is returned by Init when the requested PathAbs
// already has a live (un-finalized, un-expired) session. The caller
// should surface this to the panel as a 409 with the daemon error code
// "daemon.upload_in_progress" so the operator (or the SPA) can wait
// for the in-flight upload to land instead of racing it.
var ErrUploadInProgress = errors.New("daemon.upload_in_progress")

// Manager owns all Sessions for the process lifetime.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// audit-2026-04-25 MED6: secondary index by PathAbs so Init can
	// reject a second concurrent upload to the same destination. Two
	// concurrent inits without this would each open .partial under
	// the same path, racing each other's writes and the final rename.
	byPath map[string]*Session
	stop   chan struct{}
	// onGC is invoked for each session GC'd because of TTL expiry —
	// the rpc layer wires it up to remove the .partial file from
	// disk so abandoned uploads don't pile up.
	onGC func(s *Session)
}

func New(onGC func(*Session)) *Manager {
	m := &Manager{
		sessions: map[string]*Session{},
		byPath:   map[string]*Session{},
		stop:     make(chan struct{}),
		onGC:     onGC,
	}
	go m.gcLoop()
	return m
}

// Stop halts the GC goroutine. Used by tests; production daemons
// just leak it because they exit anyway.
func (m *Manager) Stop() { close(m.stop) }

// Init creates a fresh session and returns its id.
func (m *Manager) Init(pathAbs, filename string, totalBytes int64, totalChunks int) (*Session, error) {
	if totalBytes <= 0 {
		return nil, errors.New("totalBytes must be positive")
	}
	if totalChunks <= 0 {
		return nil, errors.New("totalChunks must be positive")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s := &Session{
		ID:          id,
		PathAbs:     pathAbs,
		TotalBytes:  totalBytes,
		TotalChunks: totalChunks,
		Filename:    filename,
		created:     now,
		expires:     now.Add(DefaultTTL),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// audit-2026-04-25 MED6: refuse a second concurrent upload to the
	// same destination. Expired entries are reaped opportunistically
	// here so a previously-orphaned session that the GC tick hasn't
	// swept yet doesn't permanently lock the path.
	if existing, ok := m.byPath[pathAbs]; ok {
		if time.Now().After(existing.expires) {
			delete(m.sessions, existing.ID)
			delete(m.byPath, pathAbs)
		} else {
			return nil, ErrUploadInProgress
		}
	}
	m.sessions[id] = s
	m.byPath[pathAbs] = s
	return s, nil
}

// Get returns the session for id, or (nil, false) if it doesn't exist
// or has already expired.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(s.expires) {
		return nil, false
	}
	return s, true
}

// Accept records that one more chunk of `n` bytes has been written
// to the .partial. Refuses chunks that would overshoot TotalBytes —
// anti-inflation guard so a client can't promise 1 MiB at init and
// then upload 100 GiB worth of chunks.
func (m *Manager) Accept(s *Session, n int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return errors.New("session already finalized")
	}
	if s.received+n > s.TotalBytes {
		return errors.New("chunk overruns declared totalBytes")
	}
	s.received += n
	s.chunks++
	return nil
}

// Reset zeroes the per-session counters so the caller can start the
// upload over from chunk 0 (audit-2026-04-25-v2 MED16). The
// .partial file on disk is truncated by the caller (handleUpload's
// O_TRUNC branch when seq==0); this matches that side. We do NOT
// touch s.expires — a re-init shouldn't extend the TTL window
// beyond what Init originally granted.
func (m *Manager) Reset(s *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = 0
	s.chunks = 0
	s.finished = false
}

// Finalize marks the session as complete and removes it from the
// active set. Caller does the actual file rename.
func (m *Manager) Finalize(s *Session) {
	s.mu.Lock()
	s.finished = true
	s.mu.Unlock()
	m.mu.Lock()
	delete(m.sessions, s.ID)
	delete(m.byPath, s.PathAbs)
	m.mu.Unlock()
}

// Cancel drops the session without renaming the .partial. Used when
// a chunk fails so the client can re-init cleanly.
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	if s, ok := m.sessions[id]; ok {
		delete(m.byPath, s.PathAbs)
	}
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) gcLoop() {
	t := time.NewTicker(gcInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-t.C:
			var dead []*Session
			m.mu.Lock()
			for id, s := range m.sessions {
				if now.After(s.expires) {
					dead = append(dead, s)
					delete(m.sessions, id)
					delete(m.byPath, s.PathAbs)
				}
			}
			m.mu.Unlock()
			if m.onGC != nil {
				for _, s := range dead {
					m.onGC(s)
				}
			}
		}
	}
}

func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ul_" + hex.EncodeToString(buf), nil
}
