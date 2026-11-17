package data

import (
	"platformserver/platform"
)

type IntegrationRuntime struct {
	App               platform.App
	Ledger            func() platform.Authority
	JointDeclarations func() map[platform.AssetRef]platform.ReleaseAsset
	Lookup            func(string) (platform.EntityInfo, bool)
	Record            func(string) (any, bool)
	FreezeProtocol    func(platform.ConfirmationParty) (*platform.ConfirmationProtocol, error)
	Holding           func(string, string) []string
}

func (o *Owner) AttachIntegrationRuntime(runtime IntegrationRuntime) {
	o.integration = runtime
}
