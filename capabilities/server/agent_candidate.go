package platformserver

import (
	"fmt"
	"platformserver/internal/acceptance"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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

func (t *Tenant) candidateAgentModel(candidate, name string) (string, platform.ModelOptions, error) {
	app, _, err := (hostView{t: t}).AgentEvaluationApp(candidate, name)
	if err != nil {
		return "", platform.ModelOptions{}, err
	}
	defs := app.Manifest().Agents
	if len(defs) != 1 {
		return "", platform.ModelOptions{}, fmt.Errorf("candidate agent is unavailable")
	}
	model := defs[0].Model
	if model == "" {
		model = t.setting(t.automation(AgentApp, false), SettingAgentModel)
	}
	binding, _, problem := t.resolveModel("agent", model, defs[0].ModelOptions)
	if problem != nil {
		return "", platform.ModelOptions{}, fmt.Errorf("%s", problem.Message)
	}
	return binding.Model, defs[0].ModelOptions, nil
}

func (t *Tenant) evaluateReleaseAgentLocked(m platform.Member, request ReleaseEvaluationRequest, now time.Time) (string, error) {
	if t.agents == nil {
		return "", fmt.Errorf("the agent capability is unavailable")
	}
	identity, err := acceptance.Digest([]string{t.ID, m.ID, request.CandidateID, request.Agent, request.Key})
	if err != nil {
		return "", err
	}
	id := "eval-" + identity[:32]
	if prior, ok := platform.Get[Evaluation](t.automation(AgentApp, false), id); ok {
		if prior.Candidate != request.CandidateID || prior.Agent != request.Agent || prior.Created.By != m.ID {
			return "", fmt.Errorf("the original evaluation request differs")
		}
		return id, nil
	}
	model, _, err := t.candidateAgentModel(request.CandidateID, request.Agent)
	if err != nil {
		return "", err
	}
	if model == "" {
		return "", fmt.Errorf("set the agent model before evaluating a candidate")
	}
	sub := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: AgentApp,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: SchemaEvalStart, Version: 1}, Payload: platform.Raw(map[string]any{"agent": request.Agent, "model": model, "suite": true, "candidate": request.CandidateID})}
	// This builder-only endpoint queues the original evaluator as the actual
	// builder, so its authorized reads/probes retain that person's identity.
	if _, refusal := t.submitAccepted(t.agents, m, sub, now, true); refusal != nil {
		return "", fmt.Errorf("agent evaluation was refused: %s", refusal.Message)
	}
	return id, nil
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
		modelName, options, err := t.candidateAgentModel(candidate.ID, name)
		if err != nil {
			return err
		}
		model, provider, refusal := t.ai.Model(modelName)
		if refusal != nil {
			return fmt.Errorf("agent %s requires an enabled model", name)
		}
		binding, _, refusal := t.resolveModel("agent", modelName, options)
		if refusal != nil {
			return fmt.Errorf("agent %s has invalid model options: %s", name, refusal.Message)
		}
		config, err := modelConfigurationDigest(model, provider, &binding)
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
