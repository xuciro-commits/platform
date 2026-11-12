package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type ResultSources struct {
	Tenant     string
	Controlled bool
	Pending    bool
	Observers  bool
	Pure       func(platform.Manifest, string) bool
	App        func(string) platform.App
	Control    func() (*acceptedformat.Control, error)
	Operations func() ([]acceptedformat.OperationCancellation, error)
	Plan       func(platform.Event) (platform.AcceptedEventPlan, *kernel.Error)
}

func (d *Decision) BatchResult(src ResultSources, app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.CheckFinalConstraints(); err != nil {
		return nil, fmt.Errorf("batch constraints: %w", err)
	}
	if receipt == nil || len(d.Events) == 0 || d.Failure != nil {
		return nil, fmt.Errorf("batch needs accepted decisions and all written records")
	}
	if err := d.Records.ValidateChangedLinkConstraints(); err != nil {
		return nil, fmt.Errorf("batch links: %w", err)
	}
	result := acceptedformat.Batch{Version: 8, Kind: "record-batch", Tenant: src.Tenant,
		Definitions: slices.Clone(d.Installations),
		App:         app, At: at.UTC(), Sequences: maps.Clone(d.Allocated), SequenceBases: maps.Clone(d.SequenceBases),
		Notices: d.Notices.Saved(), Intents: slices.Clone(d.Intents), Observations: slices.Clone(d.Observations)}
	var err error
	result.Authorizations, err = d.SavedAuthorizations()
	if err != nil {
		return nil, fmt.Errorf("batch authorizations: %w", err)
	}
	result.Control, err = src.Control()
	if err != nil {
		return nil, fmt.Errorf("batch control: %w", err)
	}
	result.Operations, err = src.Operations()
	if err != nil {
		return nil, fmt.Errorf("batch operations: %w", err)
	}
	result.States, err = SavedStates(d.States, d.StateBases)
	if err != nil {
		return nil, fmt.Errorf("batch states: %w", err)
	}
	result.Receipt, err = protojson.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("batch receipt: %w", err)
	}
	if !proto.Equal(request, receipt.GetSubmission()) {
		result.Submission, err = protojson.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("batch submission: %w", err)
		}
	}
	result.SchemaVersion = d.SchemaVersion
	result.RequestHash, err = acceptedformat.SubmissionHash(request, result.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("batch request hash: %w", err)
	}
	seen := map[string]bool{}
	for _, event := range d.Events {
		owner := src.App(event.App)
		if owner == nil || d.Logs[owner.AcceptedLedger()] == nil {
			return nil, fmt.Errorf("batch decision has no staged ledger for %s", event.App)
		}
		logged := false
		for _, record := range d.Logs[owner.AcceptedLedger()].Records(src.Tenant) {
			if proto.Equal(record, event.Record) {
				logged = true
				break
			}
		}
		if !logged {
			return nil, fmt.Errorf("batch decision is not in the staged ledger")
		}
		raw, err := protojson.Marshal(event.Record)
		if err != nil {
			return nil, fmt.Errorf("batch event %s: %w", event.App, err)
		}
		plan, refusal := src.Plan(event)
		if refusal != nil {
			return nil, fmt.Errorf("batch event plan %s: %w", event.App, refusal)
		}
		plan.Observed = d.Observed
		plan.Hops = d.Hops
		if plan.Hops > acceptedformat.MaxHops {
			plan.Stopped, plan.Subscribers = plan.Subscribers, nil
		}
		result.Decisions = append(result.Decisions, acceptedformat.Decision{Receipt: raw, Event: plan, Definition: d.Definitions[event.App+"/"+event.Record.GetChangeId()],
			Publication: d.Publications[event.App+"/"+event.Record.GetChangeId()]})
		if publication := result.Decisions[len(result.Decisions)-1].Publication; publication != nil {
			copied := *publication
			if et := d.Records.Type(event.Record.GetSubmission().GetTarget().GetType()); et != nil {
				copied.Format = et.Info().RecordFormat
			}
			result.Decisions[len(result.Decisions)-1].Publication = &copied
		}
		for _, ref := range event.Changed {
			if !d.Records.Written(ref) {
				return nil, fmt.Errorf("batch event names an unwritten record")
			}
			seen[ref] = true
		}
	}
	if len(seen) != d.Records.WriteCount() {
		result.DirectRows, err = d.SavedCallbackRows(seen)
		if err != nil {
			return nil, fmt.Errorf("batch direct rows: %w", err)
		}
		if len(seen)+len(result.DirectRows) != d.Records.WriteCount() {
			return nil, fmt.Errorf("batch event omits a written record")
		}
	}
	if len(d.Iterations) > 0 {
		result.Iterations = slices.Clone(d.Iterations)
	}
	if len(d.BusinessFacts) > 0 {
		result.Business = slices.Clone(d.BusinessFacts)
	}
	if len(result.Iterations) > 0 {
		result.Version = 9
	}
	if d.ObservationInput != nil && d.ObservationInput.Intervals != nil {
		result.Version = 10
	}
	result.Unique = d.SavedUniqueChanges()
	return SealBatch(d.Records, result, seen, d.Writers)
}

func (d *Decision) ChangePlan(control func() (*acceptedformat.Control, error)) (acceptedformat.ChangePlan, error) {
	plan := acceptedformat.ChangePlan{Notices: d.Notices.Saved(), Intents: slices.Clone(d.Intents),
		Sequences: maps.Clone(d.Allocated), SequenceBases: maps.Clone(d.SequenceBases), Unique: d.SavedUniqueChanges()}
	var err error
	plan.States, err = SavedStates(d.States, d.StateBases)
	if err == nil {
		plan.Control, err = control()
	}
	return plan, err
}

func CheckObservations(app func(string) platform.App, saved []acceptedformat.Observation) error {
	for _, item := range saved {
		owner, ok := app(item.App).(platform.AcceptedObserver)
		if !ok || len(item.Body) == 0 {
			return fmt.Errorf("accepted observation needs missing projection %s", item.App)
		}
		if err := owner.ValidateObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func ApplyObservations(app func(string) platform.App, saved []acceptedformat.Observation) error {
	for _, item := range saved {
		if err := app(item.App).(platform.AcceptedObserver).ApplyObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func (d *Decision) SealResult(src ResultSources, app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.CheckFinalConstraints(); err != nil {
		return nil, err
	}
	owner := src.App(app)
	if owner == nil || request.GetAuthority() != app {
		return nil, fmt.Errorf("decision has no matching submission owner")
	}
	action, declared := owner.Manifest().Actions.Action(request.GetSchema().GetName())
	if !declared || action.Target != request.GetTarget().GetType() {
		return nil, fmt.Errorf("submission target differs from its declared action")
	}
	if len(d.BusinessFacts) == 0 && !strings.HasPrefix(request.GetCorrelationId(), "correction:") && proto.Equal(request, receipt.GetSubmission()) {
		if raw, err := d.recordResult(src, app, receipt, at); err == nil {
			return raw, nil
		}
	}
	return d.BatchResult(src, app, request, receipt, at)
}

func (d *Decision) recordResult(src ResultSources, app string, receipt *pb.ChangeRecord, submittedAt time.Time) ([]byte, error) {
	if src.Controlled || d.Failure != nil || len(d.Allocated) > 0 || d.Notices.Changed() ||
		len(d.Intents) > 0 || len(d.Observations) > 0 || len(d.States) > 0 ||
		len(d.OperationCancels) > 0 ||
		d.Observed && src.Observers {
		return nil, fmt.Errorf("decision needs a result carrying all state and intents")
	}
	if receipt == nil || len(d.Events) != 1 || d.Records.WriteCount() != 1 || len(d.Logs) != 1 {
		return nil, fmt.Errorf("one accepted record and event are required")
	}
	accepted := false
	for _, log := range d.Logs {
		accepted = proto.Equal(log.Last(receipt.GetSubmission().GetTenantId()), receipt)
	}
	if !accepted {
		return nil, fmt.Errorf("decision has no staged kernel receipt")
	}
	typ, id := receipt.GetSubmission().GetTarget().GetType(), receipt.GetSubmission().GetTarget().GetId()
	ref := typ + "/" + id
	if !d.Records.Written(ref) || d.Events[0].App != app || !proto.Equal(d.Events[0].Record, receipt) ||
		len(d.Events[0].Changed) != 1 || d.Events[0].Changed[0] != ref {
		return nil, fmt.Errorf("decision has other record or event effects")
	}
	kind := "record"
	if publisher, ok := src.App(app).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "publication"
	} else if src.Pure(src.App(app).Manifest(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "pure-transition"
	}
	plan, refusal := src.Plan(d.Events[0])
	if refusal != nil {
		return nil, refusal
	}
	return EncodeAcceptedRecord(RecordResult{Store: d.Records, App: app, Kind: kind, Receipt: receipt, At: submittedAt,
		SchemaVersion: d.SchemaVersion, Definition: d.Definitions[app+"/"+receipt.GetChangeId()], Event: plan,
		Authorizations: d.Authorizations, Unique: d.SavedUniqueChanges()})
}

func (d *Decision) InputDecision(src ResultSources, app, name string, m platform.Member, body []byte, answer any, at time.Time) ([]byte, error) {
	if d.Failure != nil || src.Pending {
		return nil, fmt.Errorf("connector input contains effects not represented by its result")
	}
	result, err := InputResult(src.Tenant, app, name, m.ID, body, at)
	if err != nil {
		return nil, err
	}
	result.Notices, result.Authorizations = d.Notices.Saved(), slices.Clone(d.Authorizations)
	if message, ok := answer.(proto.Message); ok {
		result.Answer, err = protojson.Marshal(message)
	} else {
		result.Answer, err = json.Marshal(answer)
	}
	if err != nil {
		return nil, err
	}
	if len(d.Events) > 0 {
		receipt, ok := answer.(*pb.ChangeRecord)
		if !ok || receipt.GetSubmission().GetAuthority() != app {
			return nil, fmt.Errorf("connector input has decisions without a matching answer")
		}
		result.Changes, err = d.BatchResult(src, app, receipt.GetSubmission(), receipt, at)
		if err != nil {
			return nil, err
		}
		result.Notices = nil
		return SealInput(result)
	}
	if src.Controlled || len(d.Intents) > 0 || len(d.Observations) > 0 || len(d.Allocated) > 0 || len(d.OperationCancels) > 0 {
		return nil, fmt.Errorf("connector input contains effects without a decision")
	}
	for ledger, staged := range d.Logs {
		if !proto.Equal(ledger.LastAccepted(src.Tenant), staged.Last(src.Tenant)) {
			return nil, fmt.Errorf("connector input contains an unpublished decision")
		}
	}
	plan, err := d.ChangePlan(src.Control)
	if err != nil {
		return nil, err
	}
	result.States, result.Notices, result.Unique = plan.States, plan.Notices, plan.Unique
	result.Rows, err = d.CaptureDirectRows(app, nil, nil, "connector input")
	if err != nil {
		return nil, err
	}
	return SealInput(result)
}
