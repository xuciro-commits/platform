package kernel

import (
	"encoding/json"
	"maps"
	"platformserver/kernel/records"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type Decision struct {
	Logs              map[platform.Authority]platform.ChangeLog
	Receiving         map[platform.Authority]bool
	InputIssues       map[*platform.Error][]platform.FieldIssue
	Failure           *platform.Error
	BusinessRefusals  map[*platform.Error]platform.BusinessRefusalFact
	ProbeOnly         bool
	Installations     []platform.ReleaseInstallation
	CurrentSubmission *pb.Submission
	BusinessFacts     []platform.BusinessRefusalFact
	Simulation        bool
	Definitions       map[string]*platform.DecisionDefinition
	SchemaVersion     *platform.SchemaVersion
	Authorizations    []acceptedformat.AuthorizationEvidence
	Iterations        []platform.TxnIteration
	ConstraintWrites  map[string]ConstraintWrite
	CallbackOrigin    *platform.Effect
	Records           *records.Store
	Events            []platform.Event
	Sequences         map[string]int
	Allocated         map[string]int
	SequenceBases     map[string]int
	Active            map[string]bool
	At                time.Time
	Notices           *NoticeDraft
	Writers           map[string][]string
	Changed           map[string][]string
	Publications      map[string]*acceptedformat.Publication
	Intents           []platform.Effect
	TransactionSteps  int
	Observations      []acceptedformat.Observation
	Observed          bool
	Hops              int
	RequestCounts     map[string]int
	PrivateApps       map[string]platform.Service
	States            map[string]platform.AcceptedStateApp
	StateBases        map[string]json.RawMessage
	Connectors        *protocol.Connectors
	OperationAnswers  map[string]platform.OperationResult
	OperationCancels  map[string]bool
	ObservationInput  *platform.ObservationDecisionEvidence
	CallbackWrites    map[string]CallbackWrite
	AIWrites          map[string]AIWrite
	Rules             ConstraintRules
	DirectRows        func([]acceptedformat.InputRow, string, time.Time) error
	BusinessQuery     func(platform.RecordTypeInfo, platform.Query, platform.Member, time.Time) platform.Query
	DirectoryOf       func() platform.Directory
}

func (d *Decision) roles() OptionalRoles {
	if d.DirectoryOf == nil {
		return OptionalRoles{}
	}
	return OptionalRoles{Directory: d.DirectoryOf()}
}

func (d *Decision) Units(c platform.Caller, structure string, now time.Time) []string {
	return d.roles().MemberUnits(c, structure, now)
}

func (d *Decision) Calendar(c platform.Caller, now time.Time) platform.Calendar {
	return d.roles().MemberCalendar(c, now)
}

func (d *Decision) Element(_ platform.Caller, id string, now time.Time) (platform.ElementInfo, bool) {
	return d.roles().Element(id, now)
}

func (d *Decision) Related(_ platform.Caller, element, stereotype string, outgoing bool, now time.Time) []string {
	return d.roles().Related(element, stereotype, outgoing, now)
}

func NewDecision(base *records.Store, s *Sequence, simulation bool) Decision {
	return Decision{Logs: map[platform.Authority]platform.ChangeLog{}, Simulation: simulation, Records: base,
		Sequences: s.Sequences.Clone(), Allocated: map[string]int{}, SequenceBases: map[string]int{}, Active: map[string]bool{},
		Notices: s.Notices.Draft(), Writers: map[string][]string{}, Changed: map[string][]string{}, Connectors: s.ConnectorRoster.Clone()}
}

func (d *Decision) ReadableAt(check func(*records.Store, time.Time) bool) bool {
	d.Records.Lock()
	defer d.Records.Unlock()
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return check(d.Records, at)
}

func (d *Decision) HoldChanges(ledger platform.Authority) (func(), *platform.Error) {
	if d.Receiving[ledger] {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The decision cannot re-enter its held ledger")
	}
	if d.Receiving == nil {
		d.Receiving = map[platform.Authority]bool{}
	}
	d.Receiving[ledger] = true
	return func() { delete(d.Receiving, ledger) }, nil
}

func (d *Decision) InputRefused(err *platform.Error, issues []platform.FieldIssue) {
	if d.InputIssues == nil {
		d.InputIssues = map[*platform.Error][]platform.FieldIssue{}
	}
	d.InputIssues[err] = issues
}

func (d *Decision) Probing() bool { return d.ProbeOnly }

func (d *Decision) Seen(c platform.Caller, keys ...string) { d.Notices.Seen(c.App, keys) }

func (d *Decision) FailDecision(err *platform.Error) {
	if d.Failure == nil {
		d.Failure = err
	}
}

func (d *Decision) DraftChanges(l platform.Authority, fork func() platform.ChangeLog) platform.ChangeLog {
	if d.Logs[l] == nil {
		d.Logs[l] = fork()
	}
	return d.Logs[l]
}

func (d *Decision) BusinessRefused(answer *platform.Error, fact platform.BusinessRefusalFact) {
	if answer == nil {
		return
	}
	app := platform.OwnerOfType(fact.Action)
	if err := acceptedformat.ValidateBusinessFact(app, fact); err != nil {
		d.Failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
		return
	}
	if d.BusinessRefusals == nil {
		d.BusinessRefusals = map[*platform.Error]platform.BusinessRefusalFact{}
	}
	d.BusinessRefusals[answer] = acceptedformat.CloneBusinessFact(fact)
	d.InputRefused(answer, []platform.FieldIssue{{Code: "business_refusal", BusinessCode: fact.Code, Record: fact.Subject, Message: fact.Message, Translations: maps.Clone(fact.Translations), Path: []string{"$business"}}})
}

func Target(s *pb.Submission) string { return s.GetTarget().GetType() + "/" + s.GetTarget().GetId() }

type CallbackWrite struct {
	Source  platform.Effect
	History int
}

type ConstraintRules struct {
	Definitions func() []platform.Definition
	RuleQuery   func(app, name string, version int) (platform.NamedQuery, int, bool)
	Binding     func(platform.AssetBinding) (*platform.Definition, bool)
	Trace       func() func(platform.DecisionTrace)
}

type AIWrite struct {
	Target string
	Change string
	Caller protocol.AICaller
}

type AIBatchClaim struct {
	Target string            `json:"target"`
	Change string            `json:"change"`
	Caller protocol.AICaller `json:"caller"`
}
