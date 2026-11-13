package agents

import (
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type evaluationApp struct {
	platform.App
	agent string
}

func (a evaluationApp) Fork(ledger *platform.Ledger) (platform.App, error) {
	fork, err := a.App.Fork(ledger)
	if err != nil {
		return nil, err
	}
	return evaluationApp{App: fork, agent: a.agent}, nil
}

func (a evaluationApp) Manifest() platform.Manifest {
	m := a.App.Manifest()
	m.Agents = slices.DeleteFunc(slices.Clone(m.Agents), func(d platform.Agent) bool { return d.Name != a.agent })
	return m
}

type CandidateEvaluation struct {
	Builder   string
	Candidate func(id string) (platform.ReleaseCandidate, error)
	Owner     func(app string) (platform.ReleaseExecutionOwner, bool)
}

func (c CandidateEvaluation) App(candidate, agent string) (platform.App, string, error) {
	owner, name, valid := strings.Cut(agent, ".")
	if !valid || owner != c.Builder {
		return nil, "", fmt.Errorf("candidate evaluations require a controlled builder agent")
	}
	saved, err := c.Candidate(candidate)
	if err != nil {
		return nil, "", err
	}
	ref := platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}
	i := slices.IndexFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return nil, "", fmt.Errorf("the saved candidate does not contain this agent")
	}
	compiler, ok := c.Owner(owner)
	if !ok {
		return nil, "", fmt.Errorf("the agent's original execution owner is unavailable")
	}
	bound, err := compiler.ForkReleaseExecution(saved.Assets)
	if err != nil {
		return nil, "", err
	}
	digest, err := platform.CanonicalDigest(saved.Assets[i])
	return evaluationApp{App: bound, agent: name}, digest, err
}

type ReleaseEvaluation struct {
	Tenant       string
	Candidate    CandidateEvaluation
	Existing     func(id string) (platform.Evaluation, bool)
	DefaultModel func() string
	Resolve      func(model string, options platform.ModelOptions) (string, *kernel.Error)
	Accept       func(*pb.Submission) *kernel.Error
}

func (r ReleaseEvaluation) model(candidate, name string) (string, error) {
	app, _, err := r.Candidate.App(candidate, name)
	if err != nil {
		return "", err
	}
	defs := app.Manifest().Agents
	if len(defs) != 1 {
		return "", fmt.Errorf("candidate agent is unavailable")
	}
	model := defs[0].Model
	if model == "" {
		model = r.DefaultModel()
	}
	resolved, problem := r.Resolve(model, defs[0].ModelOptions)
	if problem != nil {
		return "", fmt.Errorf("%s", problem.Message)
	}
	return resolved, nil
}

func (r ReleaseEvaluation) Start(m platform.Member, candidate, agent, key string) (string, error) {
	identity, err := platform.CanonicalDigest([]string{r.Tenant, m.ID, candidate, agent, key})
	if err != nil {
		return "", err
	}
	id := "eval-" + identity[:32]
	if prior, ok := r.Existing(id); ok {
		if prior.Candidate != candidate || prior.Agent != agent || prior.Created.By != m.ID {
			return "", fmt.Errorf("the original evaluation request differs")
		}
		return id, nil
	}
	model, err := r.model(candidate, agent)
	if err != nil {
		return "", err
	}
	if model == "" {
		return "", fmt.Errorf("set the agent model before evaluating a candidate")
	}
	sub := &pb.Submission{TenantId: r.Tenant, PrincipalId: m.ID, Authority: platform.AgentApp,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: platform.EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: platform.SchemaEvalStart, Version: 1}, Payload: platform.Raw(map[string]any{"agent": agent, "model": model, "suite": true, "candidate": candidate})}
	if refusal := r.Accept(sub); refusal != nil {
		return "", fmt.Errorf("agent evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

type StepPreparation struct {
	Quarantined func() bool
	Recording   func() bool
	Run         func(id string) (platform.AgentRunRecord, bool)
	Evaluation  func(id string) (platform.Evaluation, bool)
	Allows      func(platform.AgentRunRecord) bool
	Accept      func(platform.AgentResultStep, time.Time) error
	Failed      func(now time.Time, run, detail string)
}

func (s StepPreparation) Prepare(b platform.AgentResultStep, now time.Time) bool {
	if s.Quarantined() {
		return false
	}
	if s.Recording() {
		now = now.Truncate(time.Microsecond)
	}
	run, known := s.Run(b.Run)
	if b.Evaluation != nil {
		ev, waited := s.Evaluation(b.Evaluation.ID)
		if !waited || ev.State != "queued" || ev.Revision != b.Evaluation.Revision {
			return false
		}
		run.Agent = b.Evaluation.Agent
	} else if !known || run.State != "running" {
		return false
	}
	if b.CallID != "" && (len(run.Pending) == 0 || run.Pending[0].ID != b.CallID) {
		return false
	}
	if b.Stop == "" && run.OnBehalf != "" {
		if !s.Allows(run) {
			b.Stop = "the person no longer has access to this agent's app"
			b.Tool, b.Arguments, b.Observation, b.Calls, b.CallID = "", nil, nil, nil, ""
		}
	}
	err := s.Accept(b, now)
	if err != nil && b.Stop == "" && !s.Quarantined() {
		err = s.Accept(platform.AgentResultStep{Run: b.Run, Evaluation: b.Evaluation, Usage: b.Usage, Stop: "The agent step could not be accepted: " + err.Error()}, now)
	}
	if err != nil {
		s.Failed(now, b.Run, err.Error())
	}
	return err == nil && !s.Quarantined()
}

type EvaluationQueue struct {
	Stopped  func() bool
	Queued   func() ([]platform.Evaluation, *kernel.Error)
	Current  func(id string) (platform.Evaluation, bool)
	Evaluate func(ev platform.Evaluation, now time.Time, step func(*platform.Evaluation) bool) platform.Evaluation
	Apply    func(platform.AgentResultStep, time.Time) bool
}

func (q EvaluationQueue) Drain(now time.Time) {
	for {
		if q.Stopped() {
			return
		}
		evals, refusal := q.Queued()
		if refusal != nil || len(evals) == 0 {
			return
		}
		report := q.Evaluate(evals[0], now, func(ev *platform.Evaluation) bool {
			revision := ev.Revision
			q.Apply(platform.AgentResultStep{Run: ev.ID, Evaluation: ev}, now)
			current, known := q.Current(ev.ID)
			if !known || current.Revision != revision+1 {
				return false
			}
			*ev = current
			return true
		})
		if report.State != "done" {
			return
		}
		q.Apply(platform.AgentResultStep{Run: report.ID, Evaluation: &report}, now)
		current, known := q.Current(report.ID)
		if !known || current.State == "queued" {
			return
		}
	}
}
