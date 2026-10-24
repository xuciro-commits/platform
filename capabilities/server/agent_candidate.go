package platformserver

import (
	"fmt"
	"platformserver/internal/acceptance"
	"slices"
	"strings"

	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

type agentEvaluationApp struct {
	platform.App
	agent string
}

func (a agentEvaluationApp) Manifest() platform.Manifest {
	m := a.App.Manifest()
	m.Agents = slices.DeleteFunc(slices.Clone(m.Agents), func(d platform.Agent) bool { return d.Name != a.agent })
	return m
}

func (h hostView) AgentEvaluationApp(candidate, agent string) (platform.App, string, error) {
	owner, name, valid := strings.Cut(agent, ".")
	if !valid || owner != construction.ID {
		return nil, "", fmt.Errorf("candidate evaluations require a controlled builder agent")
	}
	saved, err := h.t.releases.candidate(candidate)
	if err != nil {
		return nil, "", err
	}
	ref := platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}
	i := slices.IndexFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return nil, "", fmt.Errorf("the saved candidate does not contain this agent")
	}
	compiler, ok := h.t.app(owner).(host.ReleaseExecutionOwner)
	if !ok {
		return nil, "", fmt.Errorf("the agent's original execution owner is unavailable")
	}
	bound, err := compiler.ForkReleaseExecution(saved.Assets)
	if err != nil {
		return nil, "", err
	}
	digest, err := acceptance.Digest(saved.Assets[i])
	return agentEvaluationApp{App: bound, agent: name}, digest, err
}

func (a *Agents) evaluationDefinition(candidate, agent string) (*agentDef, string, error) {
	owner, ok := a.host.(host.AgentEvaluationDefinitions)
	if !ok {
		return nil, "", fmt.Errorf("candidate agent declarations are unavailable")
	}
	app, digest, err := owner.AgentEvaluationApp(candidate, agent)
	if err != nil {
		return nil, "", err
	}
	// Reuse the original tool compiler and dry evaluator; no installed
	// declaration, business record or live run is changed by this selection.
	compiler := NewAgents(a.tenant)
	compiler.AttachAgent(a.host)
	if err := compiler.DeclareAgentApp(app); err != nil {
		return nil, "", err
	}
	d := compiler.defs[agent]
	if d == nil || len(d.Cases) == 0 {
		return nil, "", fmt.Errorf("the candidate agent needs declared evaluation cases")
	}
	return d, digest, nil
}


func (t *Tenant) agentEvaluationsReadyLocked(candidate platform.ReleaseCandidate) error {
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetAgent || asset.Ref.App != construction.ID {
			continue
		}
		if t.agents == nil || t.ai == nil {
			return fmt.Errorf("agent %s requires the agent and model capabilities", asset.Ref.Name)
		}
		name := asset.Ref.App + "." + asset.Ref.Name
		digest, err := acceptance.Digest(asset)
		if err != nil {
			return err
		}
		modelName := t.setting(t.automation(AgentApp, false), SettingAgentModel)
		model, provider, refusal := t.ai.Model(modelName)
		if refusal != nil {
			return fmt.Errorf("agent %s requires an enabled model", name)
		}
		config, err := acceptance.Digest([]any{model, provider})
		if err != nil {
			return err
		}
		reports, _, refusal := platform.Find[Evaluation](t.automation(AgentApp, false), platform.Query{Domain: platform.Raw([]any{[]any{"candidate", "=", candidate.ID}, []any{"agent", "=", name}, []any{"state", "=", "done"}})})
		if refusal != nil {
			return fmt.Errorf("agent evaluation reports are unavailable")
		}
		if !slices.ContainsFunc(reports, func(e Evaluation) bool {
			return !e.Archived && e.Suite && e.DefinitionDigest == digest && e.Model == modelName && e.ModelConfig == config && e.Score == 1 && e.Fails == 0 && e.Differs == 0 && len(e.Cases) > 0 && !slices.ContainsFunc(e.Cases, func(c EvalCase) bool { return c.Runs != suiteRuns || c.Passes != suiteRuns })
		}) {
			return fmt.Errorf("agent %s needs a passing declared-case evaluation of this candidate and current model configuration", name)
		}
	}
	return nil
}
