package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/kernel/protocol"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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

func RunValue(value any) (protocol.AgentRunRecord, bool) {
	return records.As[protocol.AgentRunRecord](value)
}

type Objects struct {
	Load      func(ObjectQuery, time.Time) (any, string, *platform.Error)
	Aggregate func(ObjectQuery, time.Time) (any, *platform.Error)
}

func (o Objects) ObserveRaw(op string, raw json.RawMessage, now time.Time) (protocol.AgentObservation, *platform.Error) {
	var q ObjectQuery
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF || q.Offset < 0 || q.Limit < 0 || q.Set.Check() != nil {
		return protocol.AgentObservation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object set request is not valid")
	}
	return o.Observe(op, q, now)
}

func (o Objects) Observe(op string, q ObjectQuery, now time.Time) (protocol.AgentObservation, *platform.Error) {
	invalid := func(message string) (protocol.AgentObservation, *platform.Error) {
		return protocol.AgentObservation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
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
			return protocol.AgentObservation{}, err
		}
		return ObjectObservation(object, page), nil
	case "aggregate":
		if len(q.Sort) > 0 || q.Offset != 0 || q.Limit != 0 || q.Format || q.Window != nil || q.Histogram != nil || q.Collection != nil {
			return invalid("An aggregate carries groups and measures only")
		}
		result, err := o.Aggregate(q, now)
		if err != nil {
			return protocol.AgentObservation{}, err
		}
		out := protocol.AgentObservation{}
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
	Existing     func(string) (protocol.Evaluation, bool)
	DefaultModel func() string
	Resolve      func(string, platform.ModelOptions) (string, *platform.Error)
	Accept       func(*pb.Submission) *platform.Error
}

func (r ReleaseRunner) Start(m platform.Member, candidate, agent, key string) (string, error) {
	if !r.Agent {
		return "", fmt.Errorf("the agent capability is unavailable")
	}
	return ReleaseEvaluation{Tenant: r.Tenant, Candidate: r.Candidate, Existing: r.Existing, DefaultModel: r.DefaultModel, Resolve: r.Resolve, Accept: r.Accept}.Start(m, candidate, agent, key)
}

func DecisionMembers(values []protocol.AuditEntry) []string {
	var out []string
	for _, entry := range values {
		out = append(out, entry.Member)
	}
	return out
}

type ScheduleView struct {
	Agents     protocol.AgentRuntime
	Run        func(string) (protocol.AgentRunRecord, bool)
	Evaluation func(string) (protocol.Evaluation, bool)
	Queued     func() ([]protocol.Evaluation, *platform.Error)
}

type Schedule struct {
	Agents      protocol.AgentRuntime
	Stopped     func() bool
	Quarantined func() bool
	Recording   func() bool
	Read        func(func(ScheduleView) *platform.Error) *platform.Error
	Call        func(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	Meter       func(platform.Member, platform.AIUsage) *platform.Error
	Step        func(protocol.AgentResultStep, time.Time) bool
	Wake        func()
	Run         func(string) (protocol.AgentRunRecord, bool)
	Evaluation  func(string) (protocol.Evaluation, bool)
	Accept      func(protocol.AgentResultStep, time.Time) error
	Failed      func(time.Time, string, string)
	Evaluator   protocol.AgentEvaluator
}

func (s Schedule) Turns() Turns {
	return Turns{Agents: s.Agents, Stopped: s.Stopped, Quarantined: s.Quarantined, Call: s.Call, Meter: s.Meter, Step: s.Step, Wake: s.Wake,
		DueTurns: func(now time.Time) []protocol.AgentTurn {
			var turns []protocol.AgentTurn
			s.Read(func(v ScheduleView) *platform.Error { turns = v.Agents.DueAgentTurns(now); return nil })
			return turns
		},
		Allowed: func(run protocol.AgentRunRecord) (allowed bool, refusal *platform.Error) {
			refusal = s.Read(func(v ScheduleView) *platform.Error { allowed = v.Agents.AllowsAgentRun(run); return nil })
			return allowed, refusal
		},
		Running: func(id string) (running bool) {
			refusal := s.Read(func(v ScheduleView) *platform.Error {
				run, known := v.Run(id)
				running = known && run.State == "running"
				return nil
			})
			return running && refusal == nil
		}}
}

func (s Schedule) Prepare(b protocol.AgentResultStep, now time.Time) bool {
	return StepPreparation{Quarantined: s.Quarantined, Recording: s.Recording, Run: s.Run, Evaluation: s.Evaluation,
		Allows: func(run protocol.AgentRunRecord) bool { return s.Agents.AllowsAgentRun(run) }, Accept: s.Accept, Failed: s.Failed}.Prepare(b, now)
}

func (s Schedule) Drain(now time.Time) {
	EvaluationQueue{Stopped: s.Stopped,
		Queued: func() (evals []protocol.Evaluation, refusal *platform.Error) {
			refusal = s.Read(func(v ScheduleView) (inner *platform.Error) { evals, inner = v.Queued(); return })
			return evals, refusal
		},
		Current: func(id string) (current protocol.Evaluation, known bool) {
			refusal := s.Read(func(v ScheduleView) *platform.Error { current, known = v.Evaluation(id); return nil })
			return current, known && refusal == nil
		},
		Evaluate: func(ev protocol.Evaluation, at time.Time, step func(*protocol.Evaluation) bool) protocol.Evaluation {
			return s.Evaluator.Evaluate(s.Agents.AgentEvaluationRuntime(), ev, at, step)
		}, Apply: s.Step}.Drain(now)
}
