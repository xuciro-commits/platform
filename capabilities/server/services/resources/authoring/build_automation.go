package build

import (
	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
)

func (b *Build) CheckProcess(c platform.Caller, process automationauthoring.Process) *platform.Error {
	return b.automation.Check(c, process)
}

func (b *Build) ProcessFromAsset(asset platform.ReleaseAsset) (automationauthoring.Process, error) {
	return b.automation.ProcessFromAsset(asset)
}

func (b *Build) FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error) {
	return b.automation.FlowReleaseAsset(name, version)
}
