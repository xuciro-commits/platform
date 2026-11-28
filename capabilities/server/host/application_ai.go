package host

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	agentevaluation "platformserver/services/ai/evaluation"
	applicationowner "platformserver/services/apps"
	functionauthoring "platformserver/services/functions/authoring"
	build "platformserver/services/resources/authoring"
)

func (t *Tenant) applicationAILocked(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]protocol.ApplicationAI, *platform.Error) {
	return applicationowner.ApplicationAI{
		FunctionEvaluations: agentevaluation.ApplicationReports(t.automation(build.ID, false)),
		FunctionDefinition:  functionauthoring.AssetDefinition, Model: t.modelResolver().Describe,
		Roles: []string{build.Builder, build.Publisher},
	}.List(m, assets, visible)
}

func (t *Tenant) applicationAIUsage(m platform.Member, application platform.AssetRef, items []protocol.ApplicationAI) []protocol.ApplicationAI {
	value, _ := t.Read(t.readContext(), m, "ai-usage")
	return applicationowner.ApplicationAIUsageOf(value, application, items)
}
