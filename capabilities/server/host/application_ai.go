package host

import (
	"platformserver/platform"
	agentevaluation "platformserver/services/ai/evaluation"
	applicationowner "platformserver/services/apps"
	functionauthoring "platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) applicationAILocked(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, *platform.Error) {
	var agentEvaluations func(string, string, string) ([]platform.ApplicationAIEvaluation, *platform.Error)
	if t.agents != nil {
		caller := t.automation(platform.AgentApp, false)
		agentEvaluations = agentevaluation.AgentApplicationReports(caller)
	}
	return applicationowner.ApplicationAI{
		AgentEvaluations: agentEvaluations, FunctionEvaluations: agentevaluation.ApplicationReports(t.automation(build.ID, false)),
		FunctionDefinition: functionauthoring.AssetDefinition, Model: t.modelResolver().Describe,
		Roles: []string{build.Builder, build.Publisher},
	}.List(m, assets, visible)
}

func (t *Tenant) applicationAIUsage(m platform.Member, application platform.AssetRef, items []platform.ApplicationAI) []platform.ApplicationAI {
	value, _ := t.Read(t.readContext(), m, "ai-usage")
	return applicationowner.ApplicationAIUsageOf(value, application, items)
}
