package host

import (
	"platformserver/platform"
	agentevaluation "platformserver/services/ai/evaluation"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) applicationAILocked(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, *platform.Error) {
	var agents *platform.Caller
	if t.agents != nil {
		caller := t.automation(platform.AgentApp, false)
		agents = &caller
	}
	return build.ApplicationAI{Agents: agents, FunctionEvaluations: agentevaluation.ApplicationReports(t.automation(build.ID, false)),
		Model: t.modelResolver().Describe}.List(m, assets, visible)
}

func (t *Tenant) applicationAIUsage(m platform.Member, application platform.AssetRef, items []platform.ApplicationAI) []platform.ApplicationAI {
	value, problem := t.Read(t.readContext(), m, "ai-usage")
	if problem != nil {
		return items
	}
	report, ok := value.(platform.AIUsageReport)
	if !ok {
		return items
	}
	return build.ApplicationAIUsage(report, application, items)
}
