package build

import (
	"platformserver/platform"
	dataowner "platformserver/services/data"
	devopsmodel "platformserver/services/devops/descriptor"
)

func (b *Build) bindTestPlans() {
	env := testPlanEnvironment{
		App: ID, Type: TestPlanType, Builder: Builder,
		Caller:             func() platform.Caller { return b.host.Automation(platform.Caller{}, ID) },
		Authority:          func() platform.Authority { return b.ledger },
		SourceVersion:      func() string { return b.Manifest().Version },
		WithDependencies:   b.resolver.WithDependencies,
		CheckDependencies:  b.resolver.CheckAssetDependencies,
		EqualAsset:         b.data.EqualAsset,
		Decode:             dataowner.DecodeDescriptor,
		JointDeclarations:  func() map[platform.AssetRef]platform.ReleaseAsset { return b.jointDeclarations },
		FunctionDefinition: b.FunctionDefinition,
		Functions:          b.functions.Inventory,
		DraftID:            b.data.DraftID,
	}
	b.testPlans = b.testPlanFactory(env, b.evaluationFactory(env))
}

func (b *Build) bindSampleSets() {
	b.sampleSets = b.sampleSetFactory(devopsmodel.SampleSetEnvironment{
		App: ID, Type: SampleSetType, Builder: Builder, Version: ValueDefinitionVersion,
		Caller:            func() platform.Caller { return b.host.Automation(platform.Caller{}, ID) },
		WithDependencies:  b.resolver.WithDependencies,
		CheckDependencies: b.resolver.CheckAssetDependencies,
	})
}
