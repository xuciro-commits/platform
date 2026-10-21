package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// Simulation is what a submission would change if it were taken now, decided
// in a private staged decision that is then discarded: no
// journal entry, no record, notice, effect or delivery leaves it.
type Simulation struct {
	Issues          []platform.FieldIssue      `json:"issues,omitempty"`
	RefusalCode     string                     `json:"refusalCode,omitempty"`
	Unsupported     bool                       `json:"unsupported,omitempty"`
	TraceTruncated  bool                       `json:"traceTruncated,omitempty"`
	Input           *SimulationStep            `json:"input,omitempty"`
	Trace           []platform.DecisionTrace   `json:"trace,omitempty"`
	Read            json.RawMessage            `json:"read,omitempty"`
	RolledBack      bool                       `json:"rolledBack,omitempty"`
	Accepted        bool                       `json:"accepted"`
	Refusal         string                     `json:"refusal,omitempty"`
	Changes         []SimulatedChange          `json:"changes"`
	Matched         *bool                      `json:"matched,omitempty"`
	Flows           []SimulatedFlow            `json:"flows,omitempty"`
	Tasks           []platform.WorkTask        `json:"tasks,omitempty"`
	Functions       []construction.FunctionRun `json:"functions,omitempty"`
	ComputeMatched  *bool                      `json:"computeMatched,omitempty"`
	FunctionMatched *bool                      `json:"functionMatched,omitempty"`
}

// SimulatedFlow exposes native progress/trace, never its raw source Data.
type SimulatedFlow struct {
	ID           string                     `json:"id"`
	Flow         string                     `json:"flow"`
	Version      int                        `json:"version"`
	Dependencies string                     `json:"dependencies,omitempty"`
	Release      string                     `json:"release,omitempty"`
	State        string                     `json:"state"`
	Tokens       []platform.FlowToken       `json:"tokens"`
	Trace        []platform.FlowTraceLine   `json:"trace"`
	Outputs      map[string]json.RawMessage `json:"outputs,omitempty"`
}

// SimulatedChange is one record the decision would write, as it would read.
type SimulatedChange struct {
	Before json.RawMessage `json:"before,omitempty"`
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Record json.RawMessage `json:"record"`
}

// Simulate lets a member try their own action, or a builder try another member's action,
// against the tenant's current records, without any of it being kept. Only
// actions that take the accepted-result path can be staged; others are refused
// rather than run for real.
func (t *Tenant) Simulate(member platform.Member, as string, s *pb.Submission, now time.Time) (Simulation, *kernel.Error) {
	if err := t.admits(member); err != nil {
		return Simulation{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	member = t.authorizationMember(member)
	if !member.Active() {
		return Simulation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if s == nil || s.GetSchema() == nil || s.GetTarget() == nil {
		return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "a submission with a schema and a target is required")
	}
	if t.quarantined() {
		return Simulation{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	m := member
	if as != "" && as != member.ID {
		caller := platform.NewCaller(runtime{t}, member, construction.ID, false, false)
		if refusal := authorizationRefusal(t.authorizeNative(caller, "POST /v1/simulate", "action", []string{construction.Builder}, true)); refusal != nil {
			return Simulation{}, refusal
		}
		other, ok := t.member(as)
		if !ok {
			return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", as)
		}
		m = other
	}
	a := t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return Simulation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "{action} cannot be tried", s.GetSchema().GetName())
	}
	s = proto.Clone(s).(*pb.Submission)
	s.TenantId, s.PrincipalId, s.Authority = t.ID, m.ID, a.Manifest().ID
	s = t.currentSubmissionSchema(a, s)
	if err := t.delegatedBound(m, s); err != nil {
		return Simulation{}, err
	}
	if s.GetIdempotencyKey() == "" {
		s.IdempotencyKey = "simulate"
	}
	draft := t.newStagedDecision()
	draft.simulation = true
	_, refusal := decideAccepted(a, draft, m, s, now)
	out := Simulation{Changes: []SimulatedChange{}}
	if refusal != nil {
		out.Refusal = explained(refusal, a, s.GetSchema().GetName(), target(s)).Message
		out.RefusalCode = refusal.Code.String()
		out.Issues = t.readableFieldIssues(member, draft.inputIssues[refusal], now)
		out.Unsupported = refusal.Code == pb.ErrorCode_ERROR_CODE_CONFLICT && refusal.Message == "This action uses an effect outside the accepted-result boundary"
		return out, nil
	}
	out.Accepted = true
	// Use the member-facing read contract on the private records, so scope,
	// field masks and source-derived restrictions all apply to the requester.
	// Privacy read auditing is private to this view and is discarded too.
	view := &Tenant{ID: t.ID, identity: t.identity, apps: t.apps, owner: t.owner, records: draft.records, directory: t.directory}
	for _, ref := range slices.Sorted(func(yield func(string) bool) {
		for k := range draft.records.writes {
			if !yield(k) {
				return
			}
		}
	}) {
		typ, id, _ := strings.Cut(ref, "/")
		domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
		page, err := view.recordsRead(member, typ, platform.Query{Domain: domain, Limit: 1, Archived: true}, now)
		if err != nil || len(page.Records) == 0 {
			continue
		}
		raw, _ := json.Marshal(page.Records[0])
		out.Changes = append(out.Changes, SimulatedChange{Type: typ, ID: id, Record: raw})
	}
	return out, nil
}
