// Package monitorhist polls each connected daemon every 5 seconds and keeps
// the most recent 12 minutes (144 samples) per daemon in memory. The Panel
// serves time-series via /api/daemons/:id/monitor/history.
package monitorhist

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/shared/protocol"
)

const (
	pollInterval = 5 * time.Second
	maxSamples   = 144
)

type ring struct {
	buf  []protocol.MonitorSnapshot
	head int
	size int
}

func (r *ring) push(s protocol.MonitorSnapshot) {
	if r.buf == nil {
		r.buf = make([]protocol.MonitorSnapshot, maxSamples)
	}
	r.buf[r.head] = s
	r.head = (r.head + 1) % maxSamples
	if r.size < maxSamples {
		r.size++
	}
}

func (r *ring) snapshot() []protocol.MonitorSnapshot {
	if r.size == 0 {
		return nil
	}
	out := make([]protocol.MonitorSnapshot, r.size)
	start := r.head - r.size
	if start < 0 {
		start += maxSamples
	}
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(start+i)%maxSamples]
	}
	return out
}

type Collector struct {
	reg *daemonclient.Registry

	mu   sync.RWMutex
	data map[uint]*ring
	stop chan struct{}
}

func New(reg *daemonclient.Registry) *Collector {
	return &Collector{reg: reg, data: map[uint]*ring{}, stop: make(chan struct{})}
}

func (c *Collector) Start() {
	go func() {
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
				c.tick()
			}
		}
	}()
}

func (c *Collector) Stop() { close(c.stop) }

func (c *Collector) tick() {
	c.reg.Each(func(cli *daemonclient.Client) {
		if !cli.Connected() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		raw, err := cli.Call(ctx, protocol.ActionMonitorSnap, struct{}{})
		if err != nil {
			return
		}
		var snap protocol.MonitorSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return
		}
		c.mu.Lock()
		r, ok := c.data[cli.ID()]
		if !ok {
			r = &ring{}
			c.data[cli.ID()] = r
		}
		r.push(snap)
		c.mu.Unlock()
	})
}

func (c *Collector) History(daemonID uint) []protocol.MonitorSnapshot {
	c.mu.RLock()
	r := c.data[daemonID]
	c.mu.RUnlock()
	if r == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return r.snapshot()
}
