package agents

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (a *Agents) evaluationDefinition(candidate, agent string) (*platform.CompiledAgent, string, error) {
	owner, ok := a.host.(platform.AgentEvaluationDefinitions)
	if !ok {
		return nil, "", fmt.Errorf("candidate agent declarations are unavailable")
	}
	app, digest, err := owner.AgentEvaluationApp(candidate, agent)
	if err != nil {
		return nil, "", err
	}
	compiler := New(a.tenant)
	compiler.host = a.host
	if err := compiler.DeclareAgentApp(app); err != nil {
		return nil, "", err
	}
	d := compiler.defs[agent]
	if d == nil || len(d.Cases) == 0 {
		return nil, "", fmt.Errorf("the candidate agent needs declared evaluation cases")
	}
	return d, digest, nil
}

const (
	evaluationRuns = 20
)

const (
	memoryDays   = 90
	proposalDays = 14
	memoriesUsed = 8
)

func (a *Agents) evaluationVersion(d *platform.CompiledAgent) string {
	cases := make([][]string, len(d.Cases))
	for i, c := range d.Cases {
		cases[i] = []string{c.Name, c.Goal, c.Ref}
	}
	digest, _ := platform.CanonicalDigest([]any{a.definitionVersion(d), cases})
	return digest
}

func (a *Agents) startEvaluation(c platform.Caller, id string, p struct {
	Agent, Model, Candidate string
	Suite                   bool
}) (func(*pb.ChangeRecord), *kernel.Error) {
	if _, known := platform.Get[platform.Evaluation](c, id); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d := a.def(p.Agent)
	digest, config := "", ""
	if p.Candidate != "" {
		if !p.Suite || !c.Holds("build", "builder") {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A builder and declared cases are required for candidate agent evaluation")
		}
		var err error
		d, digest, err = a.evaluationDefinition(p.Candidate, p.Agent)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		model, provider, refusal := a.host.AgentModel(p.Model)
		if refusal != nil {
			return nil, refusal
		}
		binding, _, refusal := a.host.ResolveAgentModel(p.Model, d.ModelOptions, platform.AIStageDesign)
		if refusal != nil {
			return nil, refusal
		}
		config, err = a.host.AgentModelConfigurationDigest(model, provider, &binding)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
	}
	if d == nil || p.Model == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if p.Suite && len(d.Cases) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The agent {agent} declares no cases", p.Agent)
	}
	binding, _, refusal := a.host.ResolveAgentModel(p.Model, d.ModelOptions, platform.AIStageDesign)
	if refusal != nil {
		return nil, refusal
	}
	if config == "" {
		model, pv, _ := a.host.AgentModel(p.Model)
		config, _ = a.host.AgentModelConfigurationDigest(model, pv, &binding)
	}
	ev := platform.Evaluation{Binding: &binding, Record: platform.Record{ID: id}, Agent: p.Agent, Model: p.Model, State: "queued", Cases: []platform.EvalCase{}, Suite: p.Suite, Candidate: p.Candidate, DefinitionDigest: digest, ModelConfig: config}
	return func(r *pb.ChangeRecord) { a.host.Automation(c, platform.AgentApp).Put(r, ev) }, nil
}

func (a *Agents) AgentEvaluationRuntime() platform.AgentEvaluationRuntime {
	return platform.AgentEvaluationRuntime{Tenant: a.tenant, Host: a.host, Definition: a.def, Compile: a.evaluationDefinition, Version: a.evaluationVersion, Prompt: a.prompt, Principal: a.AgentPrincipal, Reader: a.reader, Automation: a.automation, View: a.run, Use: a.use, ObserveKnowledge: a.ObserveAgentKnowledge, Compiled: a.Compiled}
}

func reason(err *kernel.Error) string {
	if err.Message == "" {
		return err.Error()
	}
	return err.Error() + ": " + err.Message
}
