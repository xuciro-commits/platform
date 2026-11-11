package kernel

import (
	"encoding/json"
	"maps"
	"platformserver/kernel/records"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type Decision struct {
	Logs              map[*platform.Ledger]*kernel.ChangeLog
	Receiving         map[*platform.Ledger]bool
	InputIssues       map[*kernel.Error][]platform.FieldIssue
	Failure           *kernel.Error
	BusinessRefusals  map[*kernel.Error]platform.BusinessRefusalFact
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
	PrivateApps       map[string]platform.App
	States            map[string]platform.AcceptedStateApp
	StateBases        map[string]json.RawMessage
	Connectors        *kernel.Connectors
	OperationAnswers  map[string]platform.OperationResult
	OperationCancels  map[string]bool
	ObservationInput  *platform.ObservationDecisionEvidence
}

func (d *Decision) HoldChanges(ledger *platform.Ledger) (func(), *kernel.Error) {
	if d.Receiving[ledger] {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The decision cannot re-enter its held ledger")
	}
	if d.Receiving == nil {
		d.Receiving = map[*platform.Ledger]bool{}
	}
	d.Receiving[ledger] = true
	return func() { delete(d.Receiving, ledger) }, nil
}

func (d *Decision) InputRefused(err *kernel.Error, issues []platform.FieldIssue) {
	if d.InputIssues == nil {
		d.InputIssues = map[*kernel.Error][]platform.FieldIssue{}
	}
	d.InputIssues[err] = issues
}

func (d *Decision) Probing() bool { return d.ProbeOnly }

func (d *Decision) FailDecision(err *kernel.Error) {
	if d.Failure == nil {
		d.Failure = err
	}
}

func (d *Decision) DraftChanges(l *platform.Ledger, fork func() *kernel.ChangeLog) *kernel.ChangeLog {
	if d.Logs[l] == nil {
		d.Logs[l] = fork()
	}
	return d.Logs[l]
}

func (d *Decision) BusinessRefused(answer *kernel.Error, fact platform.BusinessRefusalFact) {
	if answer == nil {
		return
	}
	app, _, _ := strings.Cut(fact.Action, ".")
	if err := acceptedformat.ValidateBusinessFact(app, fact); err != nil {
		d.Failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
		return
	}
	if d.BusinessRefusals == nil {
		d.BusinessRefusals = map[*kernel.Error]platform.BusinessRefusalFact{}
	}
	d.BusinessRefusals[answer] = acceptedformat.CloneBusinessFact(fact)
	d.InputRefused(answer, []platform.FieldIssue{{Code: "business_refusal", BusinessCode: fact.Code, Record: fact.Subject, Message: fact.Message, Translations: maps.Clone(fact.Translations), Path: []string{"$business"}}})
}

func Target(s *pb.Submission) string { return s.GetTarget().GetType() + "/" + s.GetTarget().GetId() }
