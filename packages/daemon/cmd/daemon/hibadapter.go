package main

import (
	"github.com/ProjectTapX/TapS/packages/daemon/internal/hibernation"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/instance"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// hibProvider adapts *instance.Manager to the hibernation.InstanceProvider
// interface. Lives in main so both packages stay independent of each
// other (the alternative would be a circular import).
type hibProvider struct{ mgr *instance.Manager }

func (p *hibProvider) List() []protocol.InstanceInfo { return p.mgr.List() }
func (p *hibProvider) Get(uuid string) (hibernation.Instance, bool) {
	it, ok := p.mgr.Get(uuid)
	if !ok {
		return nil, false
	}
	return hibInstance{it}, true
}
func (p *hibProvider) Start(uuid string) error { return p.mgr.StartByUUID(uuid) }
func (p *hibProvider) Stop(uuid string) error  { return p.mgr.StopByUUID(uuid) }
func (p *hibProvider) SetStatus(uuid string, s protocol.InstanceStatus) {
	p.mgr.SetExternalStatus(uuid, s)
}
func (p *hibProvider) PersistField(uuid string, mutate func(*protocol.InstanceConfig)) {
	p.mgr.MutateConfig(uuid, mutate)
}
func (p *hibProvider) BaseDir() string { return p.mgr.BaseDir() }

type hibInstance struct{ it *instance.Instance }

func (h hibInstance) Config() protocol.InstanceConfig { return h.it.Config() }
func (h hibInstance) Status() protocol.InstanceStatus { return h.it.Status() }
