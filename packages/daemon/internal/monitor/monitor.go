package monitor

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

func Snapshot() protocol.MonitorSnapshot {
	s := protocol.MonitorSnapshot{Timestamp: time.Now().Unix()}

	if vs, err := mem.VirtualMemory(); err == nil {
		s.MemTotal = vs.Total
		s.MemUsed = vs.Used
		s.MemPercent = vs.UsedPercent
	}
	// short sample window so the call returns quickly
	if pcts, err := cpu.Percent(200*time.Millisecond, false); err == nil && len(pcts) > 0 {
		s.CPUPercent = pcts[0]
	}
	if du, err := disk.Usage(diskRoot()); err == nil {
		s.DiskTotal = du.Total
		s.DiskUsed = du.Used
		s.DiskPercent = du.UsedPercent
	}
	if up, err := host.Uptime(); err == nil {
		s.UptimeSec = up
	}
	return s
}