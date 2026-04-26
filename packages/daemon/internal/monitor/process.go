package monitor

import (
	"github.com/shirou/gopsutil/v3/process"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// ProcessSnapshot samples CPU/memory for the given PID. Returns Running=false
// when the process is gone or PID is zero.
func ProcessSnapshot(uuid string, pid int) protocol.ProcessSnapshot {
	out := protocol.ProcessSnapshot{UUID: uuid, PID: pid}
	if pid == 0 {
		return out
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return out
	}
	if exists, _ := process.PidExists(int32(pid)); !exists {
		return out
	}
	out.Running = true
	if cpu, err := p.CPUPercent(); err == nil {
		out.CPUPercent = cpu
	}
	if mi, err := p.MemoryInfo(); err == nil && mi != nil {
		out.MemBytes = mi.RSS
	}
	if n, err := p.NumThreads(); err == nil {
		out.NumThreads = n
	}
	return out
}
