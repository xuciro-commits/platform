package host

import (
	"platformserver/platform"
	"platformserver/services/resources/authoring"

	"platformkernel/kernel"
)

func (t *Tenant) applicationAILocked(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, *kernel.Error) {
	var agents *platform.Caller
	if t.agents != nil {
		caller := t.automation(platform.AgentApp, false)
		agents = &caller
	}
	return build.ApplicationAI{Agents: agents, Evaluations: t.automation(build.ID, false), Model: func(kind, name string, options platform.ModelOptions) (string, string, bool) {
		return t.modelResolver().Describe(kind, name, options)
	}}.List(m, assets, visible)
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
