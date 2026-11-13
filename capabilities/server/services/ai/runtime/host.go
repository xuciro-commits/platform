package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

const objectRows = 50

type ObjectQuery struct {
	Set        platform.ObjectSet
	Sort       []string
	Offset     int
	Limit      int
	Groups     []string
	Measures   []string
	Window     *records.AggregateWindowQuery
	Histogram  *records.HistogramQuery
	MaxRows    int
	Collection *platform.RecordSetRef
	Format     bool
}

type Objects struct {
	Load      func(ObjectQuery, time.Time) (any, string, *kernel.Error)
	Aggregate func(ObjectQuery, time.Time) (any, *kernel.Error)
}

func (o Objects) ObserveRaw(op string, raw json.RawMessage, now time.Time) (platform.AgentObservation, *kernel.Error) {
	var q ObjectQuery
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF || q.Offset < 0 || q.Limit < 0 || q.Set.Check() != nil {
		return platform.AgentObservation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object set request is not valid")
	}
	return o.Observe(op, q, now)
}

func (o Objects) Observe(op string, q ObjectQuery, now time.Time) (platform.AgentObservation, *kernel.Error) {
	invalid := func(message string) (platform.AgentObservation, *kernel.Error) {
		return platform.AgentObservation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
	}
	aggregated := len(q.Groups) > 0 || len(q.Measures) > 0 || q.Window != nil || q.Histogram != nil || q.MaxRows != 0 || q.Collection != nil
	switch op {
	case "load":
		if aggregated {
			return invalid("A load carries no aggregate options")
		}
		if q.Limit == 0 || q.Limit > objectRows {
			q.Limit = objectRows
		}
		page, object, err := o.Load(q, now)
		if err != nil {
			return platform.AgentObservation{}, err
		}
		return ObjectObservation(object, page), nil
	case "aggregate":
		if len(q.Sort) > 0 || q.Offset != 0 || q.Limit != 0 || q.Format || q.Window != nil || q.Histogram != nil || q.Collection != nil {
			return invalid("An aggregate carries groups and measures only")
		}
		result, err := o.Aggregate(q, now)
		if err != nil {
			return platform.AgentObservation{}, err
		}
		out := platform.AgentObservation{}
		out.Body, _ = json.Marshal(result)
		return out, nil
	default:
		return invalid("Unknown object set operation")
	}
}

type ReleaseRunner struct {
	Tenant       string
	Candidate    CandidateEvaluation
	Agent        bool
	Existing     func(string) (platform.Evaluation, bool)
	DefaultModel func() string
	Resolve      func(string, platform.ModelOptions) (string, *kernel.Error)
	Accept       func(*pb.Submission) *kernel.Error
}

func (r ReleaseRunner) Start(m platform.Member, candidate, agent, key string) (string, error) {
	if !r.Agent {
		return "", fmt.Errorf("the agent capability is unavailable")
	}
	return ReleaseEvaluation{Tenant: r.Tenant, Candidate: r.Candidate, Existing: r.Existing, DefaultModel: r.DefaultModel, Resolve: r.Resolve, Accept: r.Accept}.Start(m, candidate, agent, key)
}

func DecisionMembers(values []platform.AuditEntry) []string {
	var out []string
	for _, entry := range values {
		out = append(out, entry.Member)
	}
	return out
}

type Schedule struct {
	Agents      platform.AgentRuntime
	Stopped     func() bool
	Quarantined func() bool
	Recording   func() bool
	DueTurns    func(time.Time) []platform.AgentTurn
	Allowed     func(platform.AgentRunRecord) (bool, *kernel.Error)
	Running     func(string) bool
	Call        func(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	Meter       func(platform.Member, platform.AIUsage) *kernel.Error
	Step        func(platform.AgentResultStep, time.Time) bool
	Wake        func()
	Run         func(string) (platform.AgentRunRecord, bool)
	Evaluation  func(string) (platform.Evaluation, bool)
	Current     func(string) (platform.Evaluation, bool)
	Accept      func(platform.AgentResultStep, time.Time) error
	Failed      func(time.Time, string, string)
	Queued      func() ([]platform.Evaluation, *kernel.Error)
	Evaluator   platform.AgentEvaluator
}

func (s Schedule) Turns() Turns {
	return Turns{Agents: s.Agents, Stopped: s.Stopped, Quarantined: s.Quarantined, DueTurns: s.DueTurns, Allowed: s.Allowed,
		Running: s.Running, Call: s.Call, Meter: s.Meter, Step: s.Step, Wake: s.Wake}
}

func (s Schedule) Prepare(b platform.AgentResultStep, now time.Time) bool {
	return StepPreparation{Quarantined: s.Quarantined, Recording: s.Recording, Run: s.Run, Evaluation: s.Evaluation,
		Allows: func(run platform.AgentRunRecord) bool { return s.Agents.AllowsAgentRun(run) }, Accept: s.Accept, Failed: s.Failed}.Prepare(b, now)
}

func (s Schedule) Drain(now time.Time) {
	EvaluationQueue{Stopped: s.Stopped, Queued: s.Queued, Current: s.Current, Evaluate: func(ev platform.Evaluation, at time.Time, step func(*platform.Evaluation) bool) platform.Evaluation {
		return s.Evaluator.Evaluate(s.Agents.AgentEvaluationRuntime(), ev, at, step)
	}, Apply: s.Step}.Drain(now)
}
