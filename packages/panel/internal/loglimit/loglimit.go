// Package loglimit caps the audit_logs and login_logs tables so the
// panel DB doesn't grow forever. A background goroutine trims the
// oldest excess rows every minute; admins can also set the per-table
// row caps via the settings page.
package loglimit

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
)

// Default per-table caps if the admin hasn't saved a setting yet.
const (
	DefaultAuditMaxRows = 1_000_000
	DefaultLoginMaxRows = 1_000_000
)

const (
	settingKeyAuditMax = "log.auditMaxRows"
	settingKeyLoginMax = "log.loginMaxRows"
)

// Limits holds the currently-active caps. Loaded from settings table
// at startup and on every save.
type Limits struct {
	AuditMaxRows int64
	LoginMaxRows int64
}

// Manager runs the periodic trim loop and exposes the current limits
// to the rest of the panel.
type Manager struct {
	db     *gorm.DB
	limits atomic.Value // Limits
	stop   chan struct{}
}

func New(db *gorm.DB) *Manager {
	m := &Manager{db: db, stop: make(chan struct{})}
	m.Reload()
	return m
}

// Reload re-reads the limits from the settings table — call after a
// SetCaps endpoint write so the next trim cycle uses the new values.
func (m *Manager) Reload() {
	get := func(key string, def int64) int64 {
		var s model.Setting
		if err := m.db.First(&s, "key = ?", key).Error; err != nil {
			return def
		}
		// Cheap atoi without the strconv import.
		var v int64
		for i := 0; i < len(s.Value); i++ {
			c := s.Value[i]
			if c < '0' || c > '9' {
				return def
			}
			v = v*10 + int64(c-'0')
		}
		if v <= 0 {
			return def
		}
		return v
	}
	m.limits.Store(Limits{
		AuditMaxRows: get(settingKeyAuditMax, DefaultAuditMaxRows),
		LoginMaxRows: get(settingKeyLoginMax, DefaultLoginMaxRows),
	})
}

// Get returns the current limits snapshot.
func (m *Manager) Get() Limits {
	if v, ok := m.limits.Load().(Limits); ok {
		return v
	}
	return Limits{AuditMaxRows: DefaultAuditMaxRows, LoginMaxRows: DefaultLoginMaxRows}
}

// Start spawns the trim goroutine. Cancel via Stop.
func (m *Manager) Start() {
	go m.loop()
}

func (m *Manager) Stop() {
	close(m.stop)
}

func (m *Manager) loop() {
	// Trim immediately at boot in case the panel restarted with way-
	// too-many rows already accumulated.
	m.trimOnce()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.trimOnce()
		}
	}
}

func (m *Manager) trimOnce() {
	lim := m.Get()
	m.trimTable("audit_logs", lim.AuditMaxRows)
	m.trimTable("login_logs", lim.LoginMaxRows)
}

// trimTable deletes the oldest rows in `table` until at most `maxRows`
// remain. Uses the primary key (auto-increment id) as the ordering so
// we don't depend on the time column being indexed.
func (m *Manager) trimTable(table string, maxRows int64) {
	if maxRows <= 0 {
		return
	}
	var count int64
	if err := m.db.Table(table).Count(&count).Error; err != nil {
		log.Printf("[loglimit] count %s: %v", table, err)
		return
	}
	excess := count - maxRows
	if excess <= 0 {
		return
	}
	// Find the id of the (maxRows-th newest) row; everything older
	// goes. SQLite handles this in one indexed pass.
	var cutoff int64
	row := m.db.Table(table).Select("id").Order("id DESC").Offset(int(maxRows) - 1).Limit(1).Row()
	if err := row.Scan(&cutoff); err != nil {
		log.Printf("[loglimit] cutoff %s: %v", table, err)
		return
	}
	res := m.db.Table(table).Where("id < ?", cutoff).Delete(nil)
	if res.Error != nil {
		log.Printf("[loglimit] delete %s: %v", table, res.Error)
		return
	}
	if res.RowsAffected > 0 {
		log.Printf("[loglimit] trimmed %s: removed %d rows (was %d, cap %d)",
			table, res.RowsAffected, count, maxRows)
	}
}

// Keys exposes the settings table keys so the SettingsHandler can
// read/write without dup-defining strings.
func Keys() (auditKey, loginKey string) {
	return settingKeyAuditMax, settingKeyLoginMax
}

// EnsureContext is a thin wrapper so callers don't need to import
// context for now (reserved for future when trim takes ctx).
var _ = context.Background