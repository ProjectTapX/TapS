package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/daemonclient"
	"github.com/taps/panel/internal/model"
	"github.com/taps/shared/protocol"
)

type Scheduler struct {
	db   *gorm.DB
	reg  *daemonclient.Registry
	cron *cron.Cron

	mu      sync.Mutex
	entries map[uint]cron.EntryID // task id -> cron entry id
}

func New(db *gorm.DB, reg *daemonclient.Registry) *Scheduler {
	c := cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger)))
	return &Scheduler{db: db, reg: reg, cron: c, entries: map[uint]cron.EntryID{}}
}

// Start loads all enabled tasks from the database and begins firing.
func (s *Scheduler) Start() error {
	s.cron.Start()
	var tasks []model.Task
	if err := s.db.Find(&tasks).Error; err != nil {
		return err
	}
	for _, t := range tasks {
		if t.Enabled {
			s.add(t)
		}
	}
	return nil
}

func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// Upsert (re)installs the task in the cron. Removes any existing entry first.
func (s *Scheduler) Upsert(t model.Task) error {
	s.Remove(t.ID)
	if !t.Enabled {
		return nil
	}
	return s.add(t)
}

func (s *Scheduler) add(t model.Task) error {
	id, err := s.cron.AddFunc(normalizeCron(t.Cron), func() { s.fire(t.ID) })
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.entries[t.ID] = id
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) Remove(taskID uint) {
	s.mu.Lock()
	id, ok := s.entries[taskID]
	if ok {
		delete(s.entries, taskID)
	}
	s.mu.Unlock()
	if ok {
		s.cron.Remove(id)
	}
}

// fire reloads the task to pick up edits, then dispatches.
func (s *Scheduler) fire(taskID uint) {
	var t model.Task
	if err := s.db.First(&t, taskID).Error; err != nil {
		return
	}
	if !t.Enabled {
		return
	}
	cli, ok := s.reg.Get(t.DaemonID)
	if !ok || !cli.Connected() {
		log.Printf("scheduler: task %d daemon %d offline, skipping", t.ID, t.DaemonID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	switch t.Action {
	case model.TaskCommand:
		_, err = cli.Call(ctx, protocol.ActionInstanceInput,
			protocol.InstanceInputReq{UUID: t.UUID, Data: t.Data + "\n"})
	case model.TaskStart:
		_, err = cli.Call(ctx, protocol.ActionInstanceStart, protocol.InstanceTarget{UUID: t.UUID})
	case model.TaskStop:
		_, err = cli.Call(ctx, protocol.ActionInstanceStop, protocol.InstanceTarget{UUID: t.UUID})
	case model.TaskRestart:
		_, _ = cli.Call(ctx, protocol.ActionInstanceStop, protocol.InstanceTarget{UUID: t.UUID})
		time.Sleep(2 * time.Second)
		_, err = cli.Call(ctx, protocol.ActionInstanceStart, protocol.InstanceTarget{UUID: t.UUID})
	case model.TaskBackup:
		_, err = cli.Call(ctx, protocol.ActionBackupCreate,
			protocol.BackupCreateReq{UUID: t.UUID, Note: t.Data})
	default:
		log.Printf("scheduler: task %d unknown action %q", t.ID, t.Action)
		return
	}
	if err != nil {
		log.Printf("scheduler: task %d failed: %v", t.ID, err)
	}
	s.db.Model(&model.Task{}).Where("id = ?", t.ID).Update("last_run", time.Now())
}

// normalizeCron accepts both 5-field and 6-field cron expressions; we use a
// 6-field cron (with seconds), so we prepend "0" if the user gives 5 fields.
func normalizeCron(expr string) string {
	parts := 0
	for i, last := 0, byte(' '); i < len(expr); i++ {
		c := expr[i]
		if c == ' ' || c == '\t' {
			if last != ' ' && last != '\t' {
				parts++
			}
		}
		last = c
	}
	if expr != "" && expr[len(expr)-1] != ' ' {
		parts++
	}
	if parts == 5 {
		return "0 " + expr
	}
	return expr
}
