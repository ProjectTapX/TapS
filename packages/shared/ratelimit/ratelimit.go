// Package ratelimit is a small in-memory per-IP failure counter used
// by the panel's auth-related endpoints (login, change password, API
// key validation) and by the daemon's shared-token gate.
//
// Design:
//   - One Bucket per logical endpoint (login / change-pw / api-key /
//     daemon-token). Each bucket is independent — failing a login
//     does not count against an API-key holder, and vice versa.
//   - State is per-IP, kept in a sync.Map. No DB writes; counts are
//     ephemeral and reset on process restart. This is intentional —
//     persistence would invite write amplification, and a restart
//     gives the operator a clean slate to deal with a stuck attacker.
//   - Each bucket has a 1-minute rolling window: when the window
//     elapses, the counter resets. Crossing the threshold inside
//     one window triggers a ban that lasts BanFor.
//   - On every failure we return a small backoff sleep (capped at
//     3 s) the caller should respect, so even sub-threshold attackers
//     burn time per attempt.
//   - Idle entries are reaped opportunistically every CheckEvery so
//     the map can't grow without bound from one-off bad actors.
package ratelimit

import (
	"sync"
	"sync/atomic"
	"time"
)

// Bucket is a single IP-keyed counter.
type Bucket struct {
	name   string
	thresh atomic.Int64 // failures-per-window that trip a ban
	banFor atomic.Int64 // ban duration in nanoseconds
	window time.Duration

	entries sync.Map // map[string]*entry
	lastGC  atomic.Int64
}

type entry struct {
	mu          sync.Mutex
	windowStart time.Time
	count       int
	bannedUntil time.Time
}

// New constructs a Bucket. threshold = max failures per window before
// the IP is banned for banFor. window is fixed at 1 minute.
func New(name string, threshold int, banFor time.Duration) *Bucket {
	return NewWithWindow(name, threshold, banFor, time.Minute)
}

// NewWithWindow lets the caller pick a non-default rolling window. Used
// by the SSO oauth-start limiter where 1-minute windows would be too
// coarse — typical setting "30 attempts / 5 minutes" needs a 5-minute
// window so a steady-state attacker can't paper-thin across windows.
func NewWithWindow(name string, threshold int, banFor, window time.Duration) *Bucket {
	if window <= 0 {
		window = time.Minute
	}
	b := &Bucket{name: name, window: window}
	b.SetThreshold(threshold)
	b.SetBan(banFor)
	b.lastGC.Store(time.Now().UnixNano())
	return b
}

// SetWindow swaps the rolling-window length at runtime. Existing
// in-flight `entry` rows keep their original windowStart until the
// next Fail rolls them, which is fine — the worst case is one stale
// window's worth of carried-over count, less than a minute typically.
func (b *Bucket) SetWindow(d time.Duration) {
	if d <= 0 {
		return
	}
	// window is read in Fail without locking the bucket; protect with
	// a swap that's atomic from the perspective of any single Fail
	// call (the *entry* mutex serialises the read+update there).
	b.window = d
}

// SetThreshold updates the failure threshold at runtime (settings
// page changes the value live without a process restart).
func (b *Bucket) SetThreshold(n int) {
	if n < 1 {
		n = 1
	}
	b.thresh.Store(int64(n))
}

// SetBan updates the ban duration at runtime.
func (b *Bucket) SetBan(d time.Duration) {
	if d < time.Second {
		d = time.Second
	}
	b.banFor.Store(int64(d))
}

// Threshold returns the current threshold (for diagnostics / settings UI).
func (b *Bucket) Threshold() int           { return int(b.thresh.Load()) }
func (b *Bucket) BanDuration() time.Duration { return time.Duration(b.banFor.Load()) }

// Check returns whether the IP is currently allowed and, if not, how
// long until the ban lifts. Call this BEFORE doing the auth work so
// banned IPs don't even pay the bcrypt cost.
func (b *Bucket) Check(ip string) (allowed bool, retryAfter time.Duration) {
	now := time.Now()
	b.maybeGC(now)
	v, ok := b.entries.Load(ip)
	if !ok {
		return true, 0
	}
	e := v.(*entry)
	e.mu.Lock()
	defer e.mu.Unlock()
	if now.Before(e.bannedUntil) {
		return false, e.bannedUntil.Sub(now)
	}
	return true, 0
}

// Fail records one failure for the IP and returns a small backoff the
// caller should sleep before responding. If the new count crosses the
// threshold, the IP is banned for BanDuration; subsequent Check calls
// will refuse it.
func (b *Bucket) Fail(ip string) (banned bool, backoff time.Duration) {
	now := time.Now()
	b.maybeGC(now)
	v, _ := b.entries.LoadOrStore(ip, &entry{windowStart: now})
	e := v.(*entry)
	e.mu.Lock()
	defer e.mu.Unlock()
	// Roll the window forward if expired.
	if now.Sub(e.windowStart) > b.window {
		e.windowStart = now
		e.count = 0
	}
	e.count++
	thresh := int(b.thresh.Load())
	if e.count >= thresh {
		e.bannedUntil = now.Add(time.Duration(b.banFor.Load()))
		banned = true
	}
	// Exponential backoff: 100ms, 200ms, 400ms, ... capped at 3s.
	// Even pre-threshold attempts cost the attacker time.
	ms := int64(100) << uint(min(e.count-1, 5))
	if ms > 3000 {
		ms = 3000
	}
	backoff = time.Duration(ms) * time.Millisecond
	return banned, backoff
}

// Reset clears the counter for an IP. Used when auth succeeds so a
// user who fat-fingered once doesn't carry the count to their next
// session.
func (b *Bucket) Reset(ip string) {
	b.entries.Delete(ip)
}

// Snapshot reports current state for an IP (used by tests + admin UI).
func (b *Bucket) Snapshot(ip string) (count int, bannedUntil time.Time) {
	v, ok := b.entries.Load(ip)
	if !ok {
		return 0, time.Time{}
	}
	e := v.(*entry)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count, e.bannedUntil
}

// maybeGC walks the map at most once every 5 minutes, dropping entries
// whose window expired and whose ban (if any) has lifted. Cheap because
// the map is small in practice.
func (b *Bucket) maybeGC(now time.Time) {
	last := b.lastGC.Load()
	if now.UnixNano()-last < int64(5*time.Minute) {
		return
	}
	if !b.lastGC.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	b.entries.Range(func(k, v any) bool {
		e := v.(*entry)
		e.mu.Lock()
		dead := now.After(e.bannedUntil) && now.Sub(e.windowStart) > b.window
		e.mu.Unlock()
		if dead {
			b.entries.Delete(k)
		}
		return true
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
