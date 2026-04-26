// Package api: small per-connection token-bucket rate limiter used by
// the terminal WS to cap inbound input frames (audit-2026-04-24-v3
// M5). Independent from the IP-keyed `internal/auth/ratelimit` because
// (a) terminal needs sub-second granularity (per-frame, not per-min)
// and (b) it's per-WS-connection state, not per-IP.
package api

import (
	"sync"
	"time"
)

// tokenBucket is a classic Token Bucket sized for sub-second pacing.
// tokens accumulate at `rate` per second up to `burst`; each Take()
// consumes one or returns false when none remain.
//
// Zero or negative rate disables the limiter (Take always returns
// true) so the operator can effectively turn it off via settings.
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64 // cap
	tokens float64
	last   time.Time
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	return &tokenBucket{
		rate:   rate,
		burst:  burst,
		tokens: burst, // start full so immediate flurry of activity passes
		last:   time.Now(),
	}
}

// Take consumes one token if available. Returns false (over rate)
// when the bucket is empty; the caller decides whether to drop the
// frame, sleep, or close the connection.
func (b *tokenBucket) Take() bool {
	if b.rate <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
