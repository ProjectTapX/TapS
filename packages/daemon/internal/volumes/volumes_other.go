//go:build !linux

package volumes

import (
	"errors"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// Stubs for non-linux builds (Windows / macOS dev). Managed volumes only
// work on Linux because they rely on losetup + mount.

type Manager struct{ root string }

func New(d string) *Manager        { return &Manager{root: d + "/volumes"} }
func (m *Manager) Root() string    { return m.root }
func (m *Manager) Available() bool { return false }
func (m *Manager) MountAll()         {}
func (m *Manager) UnmountAll() error { return nil }
func (m *Manager) List() (protocol.VolumeListResp, error) {
	return protocol.VolumeListResp{Available: false, Error: "managed volumes only work on Linux"}, nil
}
func (m *Manager) Create(_ protocol.VolumeCreateReq) (protocol.Volume, error) {
	return protocol.Volume{}, errors.New("managed volumes only work on Linux")
}
func (m *Manager) Remove(_ string) error {
	return errors.New("managed volumes only work on Linux")
}
func (m *Manager) Resize(_ string, _ int64) error {
	return errors.New("managed volumes only work on Linux")
}
