package authoring

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func (b *Build) CheckProcess(c platform.Caller, process protocol.Process) *platform.Error {
	return b.automation.Check(c, process)
}

func (b *Build) ProcessFromAsset(asset platform.ReleaseAsset) (protocol.Process, error) {
	return b.automation.ProcessFromAsset(asset)
}

func (b *Build) FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error) {
	return b.automation.FlowReleaseAsset(name, version)
}
