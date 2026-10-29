package platformserver

import automation "platformserver/engine/automation"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/journal"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/studio/build"
	"reflect"
	"slices"
	"strings"
	"time"
)

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a, ok := d.tenant.app(app).(platform.App)
	if !ok || c.Automation || c.Tenant != d.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if d.probing {
		return d.Decide(c, a, s, now)
	}
	return d.requestApproval(a, c.Member, s, now)
}

func (d *stagedDecision) requestApproval(a platform.App, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	// Permission/attribute/rule checks occur privately. Even a rule that uses
	// another app cannot leave anything in the actual request transaction.
	c := platform.NewCaller(d, m, a.Manifest().ID, false, false)
	if err := d.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, err := platform.Decide(probe, a, s, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner, ok := d.tenant.app(platform.WorkApp).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	return owner.RequestApproval(c, s, now)
}

func (d *stagedDecision) batchResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.checkFinalConstraints(); err != nil {
		return nil, err
	}
	if receipt == nil || len(d.events) == 0 || d.failure != nil {
		return nil, fmt.Errorf("batch needs accepted decisions and all written records")
	}
	if err := d.records.ValidateChangedLinkConstraints(); err != nil {
		return nil, err
	}
	result := acceptedformat.Batch{Version: 2, Kind: "record-batch", Tenant: d.tenant.ID,
		App: app, At: at.UTC(), Sequences: maps.Clone(d.allocated), SequenceBases: maps.Clone(d.sequenceBases),
		Notices: d.savedNotices(), Intents: slices.Clone(d.intents), Observations: slices.Clone(d.observations),
		Deliveries: slices.Clone(d.deliveries)}
	var err error
	result.Authorizations, err = d.savedAuthorizations()
	if err != nil {
		return nil, err
	}
	result.Control, err = d.savedControl()
	if err != nil {
		return nil, err
	}
	if result.Control != nil {
		result.Version = 3
	}
	result.Operations, err = d.savedOperationCancellations()
	if err != nil {
		return nil, err
	}
	result.States, err = d.savedStates()
	if err != nil {
		return nil, err
	}
	result.Receipt, err = protojson.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if !proto.Equal(request, receipt.GetSubmission()) {
		result.Submission, err = protojson.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	result.SchemaVersion = d.schemaVersion
	result.RequestHash, err = acceptedformat.SubmissionHash(request, result.SchemaVersion)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, event := range d.events {
		owner, ok := d.tenant.app(event.App).(platform.App)
		if !ok || d.logs[owner.AcceptedLedger()] == nil {
			return nil, fmt.Errorf("batch decision has no staged ledger for %s", event.App)
		}
		logged := false
		for _, record := range d.logs[owner.AcceptedLedger()].Records(d.tenant.ID) {
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
			return nil, err
		}
		plan, refusal := d.routingTenant().planAcceptedEvent(event)
		if refusal != nil {
			return nil, refusal
		}
		plan.Observed = d.observed
		plan.Hops = d.hops
		if plan.Hops > acceptedformat.MaxHops {
			plan.Stopped, plan.Subscribers = plan.Subscribers, nil
		}
		result.Decisions = append(result.Decisions, acceptedformat.Decision{Receipt: raw, Event: plan, Definition: d.definitions[event.App+"/"+event.Record.GetChangeId()],
			Publication: d.publications[event.App+"/"+event.Record.GetChangeId()]})
		if publication := result.Decisions[len(result.Decisions)-1].Publication; publication != nil {
			copied := *publication
			if et := d.records.Type(event.Record.GetSubmission().GetTarget().GetType()); et != nil {
				copied.Format = et.Info().RecordFormat
			}
			result.Decisions[len(result.Decisions)-1].Publication = &copied
		}
		for _, ref := range event.Changed {
			if !d.records.Written(ref) {
				return nil, fmt.Errorf("batch event names an unwritten record")
			}
			seen[ref] = true
		}
	}
	if len(seen) != d.records.WriteCount() {
		result.DirectRows, err = d.savedCallbackRows(seen)
		if err != nil {
			return nil, err
		}
		if len(seen)+len(result.DirectRows) != d.records.WriteCount() {
			return nil, fmt.Errorf("batch event omits a written record")
		}
		result.Version = 4
	}
	if len(d.iterations) > 0 {
		result.Iterations = slices.Clone(d.iterations)
		result.Version = 5
	}
	if len(d.businessFacts) > 0 {
		result.Business = slices.Clone(d.businessFacts)
		result.Version = 7
	}
	result.Version = 8
	if len(result.Iterations) > 0 {
		result.Version = 9
	}
	if d.observationInput != nil && d.observationInput.Intervals != nil {
		result.Version = 10
	}
	result.Unique = d.savedUniqueChanges()
	d.records.Lock()
	defer d.records.Unlock()
	d.records.Parent().Lock()
	defer d.records.Parent().Unlock()
	for _, ref := range slices.Sorted(d.records.Writes()) {
		if !seen[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.Type(typ)
		if !ok || et == nil || et.Rows().Get(id) == nil {
			return nil, fmt.Errorf("batch includes an undeclared record")
		}
		var prior *records.Row
		if old := d.records.Parent().Type(typ); old != nil {
			prior = old.Rows().Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.Rows().Get(id), prior)
		if err != nil {
			return nil, err
		}
		row := acceptedformat.BatchRow{Image: image, App: et.Info().App, Authorities: slices.Clone(d.writers[ref]), HistoryBase: image.HistoryRef.Base}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, err
		}
		result.Rows = append(result.Rows, row)
	}
	result.Digest, err = acceptedformat.DigestBatch(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := acceptedformat.DecodeBatch(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Validate every row, counter and ledger on private forks before promotion.
// A late invalid child cannot advance an earlier authoritative ledger.
type preparedAcceptedBatch struct {
	result  acceptedformat.Batch
	records *records.Store
	ledgers map[string]*platform.Ledger
	changes map[string]*pb.ChangeRecord
	key     string
	raw     []byte
}

func (t *Tenant) applyAcceptedBatch(l *platform.Ledger, raw []byte) (bool, error) {
	prepared, err := t.prepareAcceptedBatch(l, raw)
	if err != nil || prepared == nil {
		return false, err
	}
	return t.promoteAcceptedBatch(prepared)
}

func (t *Tenant) prepareAcceptedBatch(l *platform.Ledger, raw []byte) (*preparedAcceptedBatch, error) {
	result, receipt, err := acceptedformat.DecodeBatch(raw)
	if err != nil {
		return nil, err
	}
	owner, ok := t.app(result.App).(platform.App)
	if result.Tenant != t.ID || !ok || owner.AcceptedLedger() != l {
		return nil, fmt.Errorf("record batch belongs to another tenant or ledger")
	}
	sub, _ := acceptedformat.BatchSubmission(result, receipt)
	key := result.App + "/" + sub.GetIdempotencyKey()
	if saved := t.committed.Answer(key); len(saved) > 0 {
		prior, answer, err := acceptedformat.DecodeBatch(saved)
		if err != nil || prior.RequestHash != result.RequestHash || !proto.Equal(answer, receipt) {
			return nil, fmt.Errorf("record batch key belongs to another answer")
		}
		return nil, nil
	}
	answerOwner, ok := t.app(receipt.GetSubmission().GetAuthority()).(platform.App)
	if !ok {
		return nil, fmt.Errorf("record batch answer has no authority")
	}
	if prior := answerOwner.AcceptedLedger().AcceptedFor(t.ID, receipt.GetSubmission().GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior, receipt) {
			return nil, fmt.Errorf("record batch key belongs to another result")
		}
		return nil, nil // later commits may already have changed rows/counters
	}
	ledgers := map[string]*platform.Ledger{}
	changes := map[string]*pb.ChangeRecord{}
	for _, decision := range result.Decisions {
		ra, ok := t.app(decision.Event.App).(platform.App)
		if !ok {
			return nil, fmt.Errorf("record batch needs missing authority %s", decision.Event.App)
		}
		ledgers[decision.Event.App] = ra.AcceptedLedger()
		for _, subscriber := range decision.Event.Subscribers {
			if t.app(subscriber) == nil {
				return nil, fmt.Errorf("record batch needs missing subscriber %s", subscriber)
			}
		}
		r := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(decision.Receipt, r); err != nil {
			return nil, err
		}
		if _, refused := t.committed.Refusal(decision.Event.App + "/" + r.GetSubmission().GetIdempotencyKey()); refused {
			return nil, fmt.Errorf("record batch reused a refused child key")
		}
		changes[decision.Event.App+"/"+r.GetChangeId()] = r
	}
	draft := t.records.ForkRecords()
	if err := t.validateAcceptedControl(result.Control); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedNotices(result.Notices); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedIntents(result.Intents); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedObservations(result.Observations); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedStates(result.States); err != nil {
		return nil, err
	}
	if err := t.validateOperationCancellations(result.Operations); err != nil {
		return nil, err
	}
	if err := t.validateControlDeliveries(result.Control, result.Deliveries); err != nil {
		return nil, err
	}
	for i, decision := range result.Decisions {
		if decision.Publication == nil {
			continue
		}
		publisher, ok := t.app(decision.Event.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), decision.Publication.Schema) ||
			decision.Publication.Schema != decision.Event.Names[0] {
			return nil, fmt.Errorf("record batch has an invalid publication")
		}
		r := changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if r == nil {
			return nil, fmt.Errorf("publication has no receipt")
		}
		et := draft.Type(r.GetSubmission().GetTarget().GetType())
		if et == nil {
			return nil, fmt.Errorf("publication record type is unavailable")
		}
		decoded, err := records.DecodeFormattedImage(et.Info(), decision.Publication.Image, nil, draft.Current().Release(), decision.Publication.Format)
		if err != nil {
			return nil, err
		}
		copy := *decision.Publication
		copy.Image = decoded
		copy.Format = et.Info().RecordFormat
		result.Decisions[i].Publication = &copy
		if err := publisher.ValidateAcceptedPublication(copy.Schema, copy.Image); err != nil {
			return nil, err
		}
	}
	if err := t.prepareCallbackRows(draft, result.DirectRows, result.At); err != nil {
		return nil, err
	}
	if err := t.applyBatchRows(draft, result, ledgers, changes); err != nil {
		return nil, err
	}
	for _, decision := range result.Decisions {
		if p := decision.Publication; p != nil {
			if err := t.installPublicationLinkConstraint(draft, p.Schema, p.Image); err != nil {
				return nil, err
			}
		}
	}
	if err := draft.ValidateChangedLinkConstraints(); err != nil {
		return nil, err
	}
	if result.Version >= 8 {
		if err := validateUniqueReplacements(draft, result.Unique); err != nil {
			return nil, err
		}
	}
	if err := t.sequences.Check(result.SequenceBases, result.Sequences); err != nil {
		return nil, err
	}
	checks := map[string]platform.ChangeLog{}
	for app, ledger := range ledgers {
		checks[app] = ledger.ForkAcceptedChanges()
	}
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if changed, err := checks[decision.Event.App].ApplyAccepted(r); err != nil || !changed {
			return nil, fmt.Errorf("record batch ledger validation: %v (new=%t)", err, changed)
		}
	}
	return &preparedAcceptedBatch{result, draft, ledgers, changes, key, raw}, nil
}

func (t *Tenant) promoteAcceptedBatch(prepared *preparedAcceptedBatch) (bool, error) {
	result, draft := prepared.result, prepared.records
	ledgers, changes, key, raw := prepared.ledgers, prepared.changes, prepared.key, prepared.raw
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if changed, err := ledgers[decision.Event.App].ApplyAcceptedChange(r); err != nil || !changed {
			return false, fmt.Errorf("record batch ledger application: %v (new=%t)", err, changed)
		}
	}
	if err := t.records.PromoteRecords(draft); err != nil {
		return false, err
	}
	// Application entry availability is owned by its current Build record.
	// The cached discovery must move with that accepted record as well as with
	// installed definition bytes, including withdrawal and restoration.
	if slices.ContainsFunc(result.Rows, func(row acceptedformat.BatchRow) bool { return row.Type == build.AppType }) {
		t.defGen.Add(1)
	}
	if err := t.applyAcceptedControl(result.Control); err != nil {
		return false, err
	}
	if err := t.applyAcceptedStates(result.States); err != nil {
		return false, err
	}
	if err := t.applyOperationCancellations(result.Operations); err != nil {
		return false, err
	}
	if err := t.applyAcceptedDeliveries(result.Deliveries); err != nil {
		return false, err
	}
	for _, decision := range result.Decisions {
		if p := decision.Publication; p != nil {
			if err := t.app(decision.Event.App).(platform.AcceptedPublisher).ApplyAcceptedPublication(p.Schema, p.Image); err != nil {
				return false, err
			}
		}
	}
	t.applyAcceptedNotices(result.Notices)
	t.installAcceptedIntents(result.Intents)
	if err := t.applyAcceptedObservations(result.Observations); err != nil {
		return false, err
	}
	t.sequences.Set(result.Sequences)
	for _, child := range result.Decisions {
		t.committed.SaveDecisionDefinition(child.Event.App, acceptedformat.ReceiptID(child.Receipt), child.Definition)
	}
	if result.SchemaVersion != nil {
		_, request, _ := strings.Cut(key, "/")
		t.committed.SaveSchemaVersion(result.App, request, result.SchemaVersion)
	}
	answer := new(pb.ChangeRecord)
	_ = protojson.Unmarshal(result.Receipt, answer)
	input, _ := acceptedformat.BatchSubmission(result, answer)
	if result.HasSavedAnswer(input) {
		t.committed.SaveAnswer(key, raw)
	}
	return true, nil
}

// applyBatchRows writes the batch's row images into the forked records, each
// under a ledger that declares its type, keeping the history it extends.
func (t *Tenant) applyBatchRows(draft *records.Store, result acceptedformat.Batch, ledgers map[string]*platform.Ledger, changes map[string]*pb.ChangeRecord) error {
	draft.Lock()
	defer draft.Unlock()
	for _, image := range result.Rows {
		ledger := ledgers[image.App]
		if ledger == nil {
			if owner, ok := t.app(image.App).(platform.App); ok {
				ledger = owner.AcceptedLedger()
			}
		}
		if ledger == nil || !slices.ContainsFunc(ledger.Declarations(), func(d *pb.AuthorityDeclaration) bool {
			return d.GetTenantId() == t.ID && d.GetAuthorityId() == image.App && d.GetDataClass() == image.Type
		}) {
			return fmt.Errorf("record batch has no authority for %s", image.Type)
		}
		et := draft.Type(image.Type)
		if et == nil || et.Info().App != image.App {
			return fmt.Errorf("record batch needs installed entity %s", image.Type)
		}
		prior := et.Rows().Get(image.ID)
		if prior != nil && !reflect.DeepEqual(prior.Extraction, image.Extraction) {
			return fmt.Errorf("record extraction origin changed")
		}
		before := ""
		baseHistory, revision := 0, uint32(0)
		if result.Version >= 8 {
			if err := records.CheckHistoryPredecessor(image.Image, prior); err != nil {
				return err
			}
			var err error
			before, err = records.Predecessor(image.Type, image.ID, prior)
			if err != nil {
				return err
			}
			if before != image.Before {
				return fmt.Errorf("record batch image predecessor differs")
			}
			if prior != nil {
				revision = records.Header(prior.Value).Revision
			}
		} else {
			if prior != nil {
				row, err := records.FullImage(image.Type, image.ID, prior)
				if err != nil {
					return err
				}
				before, err = platform.CanonicalDigest(row)
				if err != nil {
					return err
				}
				baseHistory, revision = len(prior.RecordHistory()), records.Header(prior.Value).Revision
			}
			if result.Version >= 2 {
				if image.HistoryBase != baseHistory {
					return fmt.Errorf("record batch history predecessor differs for %s/%s", image.Type, image.ID)
				}
				if prior != nil {
					image.History = append(records.CopyHistory(prior.RecordHistory()), image.History...)
				}
			}
			prefixEqual := true
			if result.Version == 1 && prior != nil && baseHistory > 0 && len(image.History) >= baseHistory {
				oldDigest, oldErr := platform.CanonicalDigest(prior.RecordHistory())
				savedDigest, savedErr := platform.CanonicalDigest(image.History[:baseHistory])
				prefixEqual = oldErr == nil && savedErr == nil && oldDigest == savedDigest
			}
			// PostgreSQL JSONB normalizes JSON field-change bytes (object key
			// order/whitespace). The canonical predecessor hash is authoritative;
			// bytewise DeepEqual would quarantine a sound recovered result.
			if before != image.Before || len(image.History) <= baseHistory || !prefixEqual {
				return fmt.Errorf("record batch predecessor differs for %s/%s (value=%t history=%d/%d prefix=%t)",
					image.Type, image.ID, before == image.Before, baseHistory, len(image.History), prefixEqual)
			}
		}
		value := reflect.New(et.Info().Go).Elem()
		decode, err := records.DecodeFormattedImage(et.Info(), image.Value, image.Projection, draft.Current().Release(), image.Format)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(decode))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(value.Addr().Interface()); err != nil {
			return fmt.Errorf("record batch image: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("record batch image has trailing data")
		}
		if err := checkRunApplicationBindings(value.Interface()); err != nil {
			return err
		}
		rec := records.Header(value)
		var last *pb.ChangeRecord
		if len(image.Authorities) > 0 && len(image.Authorities) != len(image.History)-baseHistory {
			return fmt.Errorf("record batch history authorities differ")
		}
		for index, change := range image.History[baseHistory:] {
			app := image.App
			if len(image.Authorities) > 0 {
				app = image.Authorities[index]
			}
			r := changes[app+"/"+change.Change]
			if r == nil || change.Schema != r.GetSubmission().GetSchema().GetName() ||
				change.By != r.GetSubmission().GetPrincipalId() || !change.At.Equal(r.GetRecordedTime().AsTime()) {
				return fmt.Errorf("record batch has a foreign history suffix")
			}
			if r.GetSubmission().GetTarget().GetType() == image.Type &&
				r.GetSubmission().GetTarget().GetId() == image.ID {
				revision = r.GetRevision()
			}
			last = r
		}
		if rec.ID != image.ID || rec.Revision != revision || last == nil ||
			rec.Changed.Change != last.GetChangeId() || rec.Changed.By != last.GetSubmission().GetPrincipalId() ||
			!rec.Changed.At.Equal(last.GetRecordedTime().AsTime()) ||
			prior != nil && !reflect.DeepEqual(rec.Created, records.Header(prior.Value).Created) ||
			prior == nil && (rec.Created.Change != image.History[0].Change ||
				rec.Created.By != image.History[0].By || !rec.Created.At.Equal(image.History[0].At)) {
			return fmt.Errorf("record batch image differs from its decisions")
		}
		recovered := &records.Row{Value: value, Format: image.Format, History: records.CopyHistory(image.History), Projection: records.CloneProjection(image.Projection), Extraction: image.Extraction}
		if result.Version >= 8 {
			if err := records.AppendRowHistory(recovered, prior, image.History); err != nil {
				return err
			}
		}
		recovered.RetainOriginal(image.Value)
		et.SetRow(rec.ID, recovered)
		draft.TrackWrite(image.Type + "/" + rec.ID)
		for _, change := range image.History[baseHistory:] {
			draft.RememberChange(image.App+"/"+change.Change, image.Type+"/"+rec.ID)
		}
		if et.Knowledge() {
			draft.MarkDirty(image.Type + "/" + rec.ID)
		}
	}
	return nil
}

func (t *Tenant) publishAcceptedBatch(result acceptedformat.Batch) {
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		_ = protojson.Unmarshal(decision.Receipt, r) // already checked
		t.publishAccepted(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed},
			decision.Event, 2)
	}
}

// Callback observations have no new kernel change receipt. Their authority is
// the host-delivered, already accepted effect and the exact owner callback,
// with the same direct-row preimage/history checks as connector/effect results.
type callbackWrite struct {
	Source  platform.Effect
	History int
}

func (d *stagedDecision) savedCallbackRows(published map[string]bool) ([]acceptedformat.CallbackRow, error) {
	var rows []acceptedformat.CallbackRow
	for _, ref := range slices.Sorted(d.records.Writes()) {
		if published[ref] {
			continue
		}
		callback, ok := d.callbackWrites[ref]
		if !ok {
			return nil, fmt.Errorf("batch event omits a written record outside an effect callback")
		}
		direct, err := captureDirectRows(d, callback.Source.App, []string{ref}, nil, "effect answer")
		if err != nil || len(direct) != 1 {
			return nil, fmt.Errorf("callback row cannot be captured: %v", err)
		}
		historyCount := len(direct[0].History)
		if direct[0].HistoryRef != nil {
			historyCount += direct[0].HistoryRef.Base
		}
		if historyCount != callback.History {
			return nil, fmt.Errorf("callback row has a write outside its source callback")
		}
		if _, err := d.tenant.prepareAcceptedDirectRows(direct, "app:"+callback.Source.App, d.at); err != nil {
			return nil, err
		}
		rows = append(rows, acceptedformat.CallbackRow{Row: direct[0], Source: callback.Source})
	}
	return rows, nil
}

// Validate sources and direct rows in private views before any ledger or
// business/control state in the containing result is promoted.
func (t *Tenant) prepareCallbackRows(target *records.Store, rows []acceptedformat.CallbackRow, at time.Time) error {
	seen := map[string]bool{}
	for _, callback := range rows {
		row, source := callback.Row, callback.Source
		ref := row.Type + "/" + row.ID
		if seen[ref] || row.App == "" || source.ID == "" || source.App != row.App {
			return fmt.Errorf("callback row has an invalid source or owner")
		}
		seen[ref] = true
		if _, ok := t.app(row.App).(platform.Answerer); !ok {
			return fmt.Errorf("callback row has no effect answer owner")
		}
		t.opsMu.Lock()
		var current platform.Effect
		for _, effect := range t.outbound {
			if effect.ID == source.ID {
				current = effect.Effect
				break
			}
		}
		t.opsMu.Unlock()
		actual, err := platform.CanonicalDigest(current)
		expected, expectedErr := platform.CanonicalDigest(source)
		if current.ID == "" || err != nil || expectedErr != nil || actual != expected {
			return fmt.Errorf("callback row source effect predecessor differs")
		}
		private, err := t.prepareAcceptedDirectRows([]acceptedformat.InputRow{row}, "app:"+row.App, at)
		if err != nil {
			return err
		}
		typ, id, _ := strings.Cut(ref, "/")
		private.Lock()
		image := private.Type(typ).Rows().Get(id)
		private.Unlock()
		target.Lock()
		target.Type(typ).SetRow(id, image)
		target.TrackWrite(ref)
		if target.Type(typ).Knowledge() {
			target.MarkDirty(ref)
		}
		target.Unlock()
	}
	return nil
}

// A connector's cursor and last-seen mark belong to the same durable result
// as the decisions caused by its delivery. Before guards both fields, including
// push deliveries (which have no cursor transition).

func markFor(marks []kernel.ConnectorMark, tenant, connector string) kernel.ConnectorMark {
	for _, mark := range marks {
		if mark.Tenant == tenant && mark.Connector == connector {
			return mark
		}
	}
	return kernel.ConnectorMark{}
}

func (t *Tenant) validateAcceptedDeliveries(deliveries []acceptedformat.ConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	descriptors, marks := t.connectors.kernel.State()
	private := t.services.Kernel.Connectors()
	private.Restore(descriptors, marks)
	for _, delivery := range deliveries {
		if delivery.Connector == "" || delivery.DataClass == "" || delivery.At.IsZero() || delivery.Before == "" {
			return fmt.Errorf("accepted connector delivery is incomplete")
		}
		_, current := private.State()
		before, err := platform.CanonicalDigest(markFor(current, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor differs", delivery.Connector)
		}
		if err := private.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s delivery: %w", delivery.Connector, err)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedDeliveries(deliveries []acceptedformat.ConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, delivery := range deliveries {
		_, marks := t.connectors.kernel.State()
		before, err := platform.CanonicalDigest(markFor(marks, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor changed during application", delivery.Connector)
		}
		if err := t.connectors.kernel.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s changed during application: %w", delivery.Connector, err)
		}
	}
	return nil
}

// Control changes participate in the business result. Each owned component has
// a typed immutable image and its own predecessor, rather than restoring an
// entire tenant over records or concurrent outside-lane bookkeeping.
type controlWork struct {
	Queues map[string][]string           `json:"queues"`
	Failed []string                      `json:"failed"`
	Tasks  []platform.ScheduledTaskState `json:"tasks"`
	Jobs   []automation.Task             `json:"jobs"`
}
type controlDecision struct {
	host *Tenant
	base map[string]json.RawMessage
}

func (t *Tenant) captureControl() (map[string]json.RawMessage, error) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := map[string]json.RawMessage{}
	save := func(kind string, v any) error { raw, err := json.Marshal(v); out[kind] = raw; return err }
	descriptors, _ := t.connectors.kernel.State()
	raw, err := platform.Protos(descriptors)
	if err != nil {
		return nil, err
	}
	out["connectors"] = raw
	if err := save("settings", t.settings.Snapshot()); err != nil {
		return nil, err
	}
	bindings := map[string]string{}
	for protocol, b := range t.bindings {
		bindings[protocol] = b.provider.Manifest().ID
	}
	if err := save("bindings", bindings); err != nil {
		return nil, err
	}
	state := tenantState{Queues: map[string][]string{}}
	work, workErr := t.work.Snapshot()
	state.Queues = work.Queues
	state.Failed = append(state.Failed, work.Failed...)
	state.Tasks = append(state.Tasks, work.Tasks...)
	state.Jobs = append(state.Jobs, work.Jobs...)
	if err := workErr; err != nil {
		return nil, err
	}
	if err := save("work", controlWork{state.Queues, state.Failed, state.Tasks, state.Jobs}); err != nil {
		return nil, err
	}
	if err := save("endpoints", t.endpoints); err != nil {
		return nil, err
	}
	effects := make([]acceptedformat.EffectState, 0, len(t.outbound))
	for _, e := range t.outbound {
		effects = append(effects, acceptedformat.EffectState{Effect: e.Effect, Since: e.since})
	}
	if err := save("effects", effects); err != nil {
		return nil, err
	}
	if err := save("words", t.words.Clone()); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeControl(raw []byte, v any) error {
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return fmt.Errorf("control image is empty or too large")
	}
	if err := platform.DecodeJSON(raw, v, "control image"); err != nil {
		return err
	}
	return nil
}

func (t *Tenant) forkControl(images map[string]json.RawMessage) (*Tenant, error) {
	view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, Store: t.store(), identity: t.identity, apps: t.apps, appIndex: t.appIndex, owner: t.owner, definitions: t.definitions, records: t.records, procs: t.procs, tasks: t.tasks, linker: t.linker, directory: t.directory, agents: t.agents, ai: t.ai,
		bindings: maps.Clone(t.bindings), connectors: newConnectorRoster(t.services.Kernel), work: automation.NewWorkBoard(), works: t.forkWorkState()}
	for _, j := range t.work.Jobs {
		copy := *j
		view.work.AddJob(&copy)
	}
	t.opsMu.Lock()
	_, marks := t.connectors.kernel.State()
	t.opsMu.Unlock()
	view.connectors.kernel.Restore(nil, marks)
	view.i18n = transport.NewTranslator(func() []platform.App { return view.apps }, nil, view.words.Clone, PlatformLanguages)
	view.i18n.InstallApplications(view.definitions)
	for _, kind := range []string{"connectors", "settings", "bindings", "work", "endpoints", "effects", "words"} {
		if err := view.installControl(kind, images[kind]); err != nil {
			return nil, err
		}
	}
	return view, nil
}

func (t *Tenant) installControl(kind string, raw json.RawMessage) error {
	switch kind {
	case "connectors":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		descriptors, err := platform.Unprotos[*pb.ConnectorDescriptor](raw)
		if err != nil {
			return err
		}
		_, marks := t.connectors.kernel.State()
		t.connectors.kernel = t.services.Kernel.Connectors()
		t.connectors.kernel.Restore(descriptors, marks)
		t.connectors.byID = map[string]*pb.ConnectorDescriptor{}
		for _, desc := range descriptors {
			if desc.GetTenantId() != t.ID || t.connectors.byID[desc.GetConnectorId()] != nil {
				return fmt.Errorf("control connector identity differs")
			}
			t.connectors.byID[desc.GetConnectorId()] = desc
		}
	case "settings":
		var values map[string]string
		if err := decodeControl(raw, &values); err != nil {
			return err
		}
		t.settings.Restore(values)
	case "bindings":
		var bindings map[string]string
		if err := decodeControl(raw, &bindings); err != nil {
			return err
		}
		t.bindings = map[string]binding{}
		for protocol, provider := range bindings {
			if !t.rebind(protocol, provider) {
				return fmt.Errorf("control protocol provider is missing")
			}
		}
	case "work":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var w controlWork
		if err := decodeControl(raw, &w); err != nil {
			return err
		}
		state := tenantState{Queues: w.Queues, Failed: w.Failed, Tasks: w.Tasks, Jobs: w.Jobs}
		known := map[string]bool{}
		for _, task := range w.Tasks {
			if task.ID == "" || known[task.ID] || t.app(task.App) == nil {
				return fmt.Errorf("control work identity differs")
			}
			known[task.ID] = true
		}
		indexed := map[string]bool{}
		for app, queue := range w.Queues {
			if t.app(app) == nil {
				return fmt.Errorf("control queue owner missing")
			}
			for _, id := range queue {
				if !known[id] || indexed[id] {
					return fmt.Errorf("control queue references missing or duplicate work")
				}
				indexed[id] = true
			}
		}
		for _, id := range w.Failed {
			if !known[id] || indexed[id] {
				return fmt.Errorf("control failed work identity differs")
			}
			indexed[id] = true
		}
		if len(indexed) != len(known) {
			return fmt.Errorf("control work is unassigned")
		}
		old := map[string]*automation.Task{}
		for _, queue := range t.work.Queues {
			for _, task := range queue {
				old[task.ID] = task
			}
		}
		for _, task := range t.work.Failed {
			old[task.ID] = task
		}
		if err := t.work.Restore(t.ID, automation.WorkState{Queues: state.Queues, Failed: state.Failed, Tasks: state.Tasks, Jobs: state.Jobs}); err != nil {
			return err
		}
		reuse := func(task *automation.Task) *automation.Task {
			if prior := old[task.ID]; prior != nil {
				prior.RestoreOutcome(*task)
				prior.Since = task.Since
				return prior
			}
			return task
		}
		for app, queue := range t.work.Queues {
			for i, task := range queue {
				t.work.Queues[app][i] = reuse(task)
			}
		}
		for i, task := range t.work.Failed {
			t.work.Failed[i] = reuse(task)
		}
	case "endpoints":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var endpoints []*Endpoint
		if err := decodeControl(raw, &endpoints); err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, endpoint := range endpoints {
			if endpoint == nil || endpoint.ID == "" || ids[endpoint.ID] {
				return fmt.Errorf("control endpoint identity differs")
			}
			ids[endpoint.ID] = true
		}
		t.endpoints = endpoints
	case "effects":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var states []acceptedformat.EffectState
		if err := decodeControl(raw, &states); err != nil {
			return err
		}
		existing := map[string]*effect{}
		for _, e := range t.outbound {
			existing[e.ID] = e
		}
		ids := map[string]bool{}
		effects := make([]*effect, 0, len(states))
		for _, state := range states {
			if state.ID == "" || ids[state.ID] || !slices.Contains([]string{"held", "pending", "retrying", "unknown", "compensated", "delivered", "failed", "rejected", "discarded"}, state.State) {
				return fmt.Errorf("control effect identity or state differs")
			}
			ids[state.ID] = true
			e := existing[state.ID]
			if e == nil {
				e = &effect{}
			}
			e.Effect, e.since = state.Effect, state.Since
			effects = append(effects, e)
		}
		t.outbound = effects
	case "words":
		var words map[string]map[string]string
		if err := decodeControl(raw, &words); err != nil {
			return err
		}
		t.words.Restore(words)
		t.i18n.Reset()
	default:
		return fmt.Errorf("unknown control owner %s", kind)
	}
	return nil
}

func (d *stagedDecision) controlHost() *Tenant {
	if d.control != nil {
		return d.control.host
	}
	base, err := d.tenant.captureControl()
	if err != nil {
		unsupportedStagedEffect()
	}
	d.control = &controlDecision{d.newControlView(base), base}
	return d.control.host
}

// Both a first control change and a savepoint use the same private owners.
// Their predecessor images remain selected by the calling operation.
func (d *stagedDecision) newControlView(images map[string]json.RawMessage) *Tenant {
	view, err := d.tenant.forkControl(images)
	if err != nil {
		unsupportedStagedEffect()
	}
	view.records = d.records
	view.connectors.kernel = d.connectors
	view.connectors.byID = map[string]*pb.ConnectorDescriptor{}
	connectors, _ := d.connectors.State()
	for _, descriptor := range connectors {
		view.connectors.byID[descriptor.GetConnectorId()] = descriptor
	}
	view.controlAnswer = func(x platform.Effect, out platform.Outcome, at time.Time) *kernel.Error {
		if err := d.tenant.stageEffectAnswer(d, x, out, nil, at); err != nil {
			if refusal, ok := err.(*kernel.Error); ok {
				d.failure = refusal
			} else {
				unsupportedStagedEffect()
			}
		}
		return d.failure
	}
	if d.tenant.agents != nil {
		view.agents = d.stateApp(d.tenant.agents).(platform.AgentRuntime)
	}
	return view
}

func (d *stagedDecision) savedControl() (*acceptedformat.Control, error) {
	if d.control == nil {
		return nil, nil
	}
	current, err := d.control.host.captureControl()
	if err != nil {
		return nil, err
	}
	result := &acceptedformat.Control{}
	for _, kind := range slices.Sorted(maps.Keys(current)) {
		base := d.control.base[kind]
		image := current[kind]
		if kind == "effects" {
			var prior, after []acceptedformat.EffectState
			if err := decodeControl(base, &prior); err != nil {
				return nil, err
			}
			if err := decodeControl(image, &after); err != nil {
				return nil, err
			}
			lookup := map[string]acceptedformat.EffectState{}
			for _, x := range prior {
				lookup[x.ID] = x
			}
			var beforeChanges, afterChanges []acceptedformat.EffectState
			for _, x := range after {
				old, known := lookup[x.ID]
				if !known {
					return nil, fmt.Errorf("control may not create effect intents")
				}
				oldHash, _ := platform.CanonicalDigest(old)
				newHash, _ := platform.CanonicalDigest(x)
				if oldHash != newHash && !d.operationCancels[x.ID] {
					beforeChanges = append(beforeChanges, old)
					afterChanges = append(afterChanges, x)
				}
			}
			if len(afterChanges) == 0 {
				continue
			}
			base, _ = json.Marshal(beforeChanges)
			image, _ = json.Marshal(afterChanges)
		}
		before, err := platform.CanonicalDigest(base)
		if err != nil {
			return nil, err
		}
		after, err := platform.CanonicalDigest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			result.Changes = append(result.Changes, acceptedformat.ControlChange{Kind: kind, Before: before, Image: image})
		}
	}
	if len(result.Changes) == 0 {
		return nil, nil
	}
	return result, nil
}

func (t *Tenant) validateAcceptedControl(saved *acceptedformat.Control) error {
	if saved == nil {
		return nil
	}
	current, err := t.captureControl()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, change := range saved.Changes {
		prior := current[change.Kind]
		if change.Kind == "effects" {
			var after, all []acceptedformat.EffectState
			if err := decodeControl(change.Image, &after); err != nil {
				return err
			}
			if err := decodeControl(prior, &all); err != nil {
				return err
			}
			selected := []acceptedformat.EffectState{}
			seenIDs := map[string]bool{}
			for _, next := range after {
				i := slices.IndexFunc(all, func(x acceptedformat.EffectState) bool { return x.ID == next.ID })
				if i < 0 || seenIDs[next.ID] {
					return fmt.Errorf("control effect is missing or duplicated")
				}
				seenIDs[next.ID] = true
				old := all[i]
				fixed := next
				fixed.State, fixed.Due, fixed.Since = old.State, old.Due, old.Since
				fixed.Last, fixed.Error, fixed.CallbackError = old.Last, old.Error, old.CallbackError
				oldHash, _ := platform.CanonicalDigest(old)
				fixedHash, _ := platform.CanonicalDigest(fixed)
				if oldHash != fixedHash {
					return fmt.Errorf("control effect rewrites immutable intent")
				}
				selected = append(selected, old)
				all[i] = next
			}
			prior, _ = json.Marshal(selected)
			current[change.Kind], _ = json.Marshal(all)
		}
		before, err := platform.CanonicalDigest(prior)
		if err != nil || seen[change.Kind] || current[change.Kind] == nil || change.Before != before {
			return fmt.Errorf("control %s predecessor differs", change.Kind)
		}
		seen[change.Kind] = true
		if change.Kind != "effects" {
			current[change.Kind] = change.Image
		}
	}
	_, err = t.forkControl(current)
	return err
}

func (t *Tenant) applyAcceptedControl(saved *acceptedformat.Control) error {
	if saved == nil {
		return nil
	}
	for _, change := range saved.Changes {
		// Tenant.mu serializes the authoritative promotion. Only this component's
		// owned bytes change; effects preserve outside-lane sending flags by ID.
		image := change.Image
		if change.Kind == "effects" {
			var patches []acceptedformat.EffectState
			if err := decodeControl(image, &patches); err != nil {
				return err
			}
			t.opsMu.Lock()
			for _, patch := range patches {
				i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == patch.ID })
				if i < 0 {
					t.opsMu.Unlock()
					return fmt.Errorf("control effect disappeared")
				}
				t.outbound[i].Effect, t.outbound[i].since = patch.Effect, patch.Since
			}
			t.opsMu.Unlock()
			continue
		}
		if err := t.installControl(change.Kind, image); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) controlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	switch s.GetTarget().GetType() {
	case controlowner.ConnectorType:
		return t.decideConnector(c, s, at)
	case controlowner.SettingType:
		return t.decideSetting(c, s, at)
	case controlowner.ProtocolType:
		return t.decideBinding(c, s, at)
	case controlowner.WorkType:
		return t.decideWork(c, s, at)
	case controlowner.NotificationType:
		return t.decideNotification(c, s, at)
	case controlowner.EndpointType:
		return t.decideEndpoint(c, s, at)
	case controlowner.EffectType:
		return t.decideEffect(c, s, at)
	case controlowner.TranslationType:
		return t.decideTranslation(c, s, at)
	default:
		return nil, unknown()
	}
}

func (r runtime) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Control actions require a private decision")
	}
	return r.t.controlDecision(c, s, at)
}
func (d *stagedDecision) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.App != controlowner.PlatformApp || s.GetAuthority() != controlowner.PlatformApp || c.ID != s.GetPrincipalId() || c.Tenant != d.tenant.ID || s.GetTenantId() != d.tenant.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the console may decide tenant controls")
	}
	if s.GetTarget().GetType() == controlowner.NotificationType {
		id := s.GetTarget().GetId()
		i := slices.IndexFunc(d.notices, func(n platform.Notification) bool { return n.ID == id })
		if i < 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "")
		}
		if d.notices[i].Member != c.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "")
		}
		return func(*pb.ChangeRecord) { d.notices[i].Read = true; d.noticeChanged = true }, nil
	}
	if s.GetTarget().GetType() == controlowner.EffectType && s.GetSchema().GetName() == controlowner.SchemaEffectDiscard {
		d.tenant.opsMu.Lock()
		var current platform.Effect
		for _, effect := range d.tenant.outbound {
			if effect.ID == s.GetTarget().GetId() {
				current = effect.Effect
				break
			}
		}
		d.tenant.opsMu.Unlock()
		if current.Endpoint == acceptedformat.OperationEndpoint {
			if platform.EffectSettled(current.State) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "")
			}
			return func(*pb.ChangeRecord) {
				if d.operationCancels == nil {
					d.operationCancels = map[string]bool{}
				}
				d.operationCancels[current.ID] = true
				after := current
				after.State, after.Error, after.Output = "discarded", "Cancelled", nil
				if err := d.tenant.stageEffectAnswer(d, after, platform.Outcome{Effect: current.ID, Result: "discarded", Detail: "Cancelled"}, nil, at); err != nil {
					if refusal, ok := err.(*kernel.Error); ok {
						d.failure = refusal
					} else {
						unsupportedStagedEffect()
					}
				}
			}, nil
		}
	}
	view := d.controlHost()
	// Effect callbacks stay on the original staged Caller; only their control
	// metadata is owned by this private host view.
	return view.controlDecision(c, s, at)
}

func (d *stagedDecision) forkControlDecision(original *controlDecision) {
	if original == nil {
		return
	}
	images, err := original.host.captureControl()
	if err != nil {
		unsupportedStagedEffect()
	}
	d.control = &controlDecision{d.newControlView(images), maps.Clone(original.base)}
}

func (d *stagedDecision) routingTenant() *Tenant {
	if d.control != nil {
		return d.control.host
	}
	return d.tenant
}

func (t *Tenant) validateControlDeliveries(saved *acceptedformat.Control, deliveries []acceptedformat.ConnectorDelivery) error {
	if saved == nil || len(deliveries) == 0 {
		return t.validateAcceptedDeliveries(deliveries)
	}
	images, err := t.captureControl()
	if err != nil {
		return err
	}
	for _, change := range saved.Changes {
		if change.Kind == "connectors" {
			images[change.Kind] = change.Image
		}
	}
	view, err := t.forkControl(images)
	if err != nil {
		return err
	}
	return view.validateAcceptedDeliveries(deliveries)
}

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	ids := map[string]bool{}
	for _, x := range intents {
		if err := platform.CheckApplicationBindings(x.Applications); err != nil {
			return err
		}
		kind, ok := strings.CutPrefix(x.Event, x.App+"/")
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, x.App, kind, x.Key, x.Endpoint)
		if kind == "model" && x.Endpoint == acceptedformat.ModelEndpoint {
			id = fmt.Sprintf("%s:%s:model:%s", t.ID, x.App, x.Key)
		}
		if !ok || kind == "" || x.App == "" || t.app(x.App) == nil || x.Key == "" || x.Endpoint == "" ||
			x.ID != id ||
			ids[x.ID] || x.State != "pending" && x.State != "held" || x.At.IsZero() || !x.Due.Equal(x.At) ||
			x.Attempts != 0 || !x.Last.IsZero() || x.Error != "" || x.Digest != "" ||
			x.State == "held" && x.Agent == "" || slices.ContainsFunc(t.outbound, func(e *effect) bool { return e.ID == x.ID }) {
			return fmt.Errorf("accepted result has an invalid or duplicate outbound intent")
		}
		ids[x.ID] = true
	}
	return nil
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, planned := range intents {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}

// Notification state and already-rendered mail intents belong to the decision,
// not to enqueue. Replaying a result does not resolve recipients or translate it.

// Reuse the tenant recipient resolver over private directory owners. This
// read view has no runtime lane, storage hooks or mutable host components.
func (d *stagedDecision) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	view := &Tenant{apps: slices.Clone(d.tenant.apps), appIndex: d.tenant.appIndex, directory: d.directoryView()}
	for i, app := range view.apps {
		if app.Manifest().ID == controlowner.PlatformApp {
			view.apps[i] = d.stateApp(app)
			break
		}
	}
	return view.recipients(c, now, to)
}

func (d *stagedDecision) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	var out []string
	for _, member := range d.recipients(c, now, to) {
		if n.Key != "" && slices.ContainsFunc(d.notices, func(x platform.Notification) bool {
			return x.Member == member && x.App == c.App && x.Key == n.Key
		}) {
			continue
		}
		d.noticeSeq++
		x := n
		x.ID, x.Member, x.App, x.At, x.Read = fmt.Sprintf("n-%d", d.noticeSeq), member, c.App, now, false
		d.notices = append(d.notices, x)
		d.noticeChanged = true
		out = append(out, member)
		console, _ := d.tenant.app(controlowner.PlatformApp).(platform.IdentityProfile)
		if private, ok := d.states[controlowner.PlatformApp].(platform.IdentityProfile); ok {
			console = private // invitation recipients exist in the accepted directory fork
		}
		if console != nil {
			to := console.Reach(member)
			if !to.InApp {
				d.notices[len(d.notices)-1].Read = true
			}
			router := d.routingTenant()
			router.opsMu.Lock()
			d.noticeEffects = append(d.noticeEffects, router.planMailNotice(x, to)...)
			router.opsMu.Unlock()
		}
	}
	if len(d.notices) > runtimekernel.NoticesKept {
		d.notices = d.notices[len(d.notices)-runtimekernel.NoticesKept:]
	}
	return out
}

func (d *stagedDecision) Seen(c platform.Caller, keys ...string) {
	for i, n := range d.notices {
		if n.App == c.App && slices.Contains(keys, n.Key) && !n.Read {
			d.notices[i].Read = true
			d.noticeChanged = true
		}
	}
}

func (d *stagedDecision) savedNotices() *acceptedformat.Notices {
	if !d.noticeChanged {
		return nil
	}
	return &acceptedformat.Notices{Base: d.noticeBase, Seq: d.noticeSeq, Before: d.noticeBefore,
		Notices: slices.Clone(d.notices), Effects: slices.Clone(d.noticeEffects)}
}

func (t *Tenant) validateAcceptedNotices(n *acceptedformat.Notices) error {
	if n == nil {
		return nil
	}
	current, seq := t.notices.State()
	before, err := platform.CanonicalDigest(current)
	if err != nil || n.Base != seq || n.Before != before || n.Seq < n.Base || len(n.Notices) > runtimekernel.NoticesKept {
		return fmt.Errorf("notification result predecessor differs")
	}
	ids := map[string]bool{}
	prior := map[string]platform.Notification{}
	for _, x := range current {
		prior[x.ID] = x
	}
	for _, x := range n.Notices {
		if x.ID == "" || x.App == "" || x.Member == "" || x.At.IsZero() || ids[x.ID] {
			return fmt.Errorf("notification result has an invalid notification")
		}
		ids[x.ID] = true
		if p, ok := prior[x.ID]; ok {
			p.Read = x.Read
			if p != x || prior[x.ID].Read && !x.Read {
				return fmt.Errorf("notification result rewrites an existing notice")
			}
		}
	}
	// The only operations are appending stable numbered notices and marking
	// existing ones read. Retention may drop a prefix, never an arbitrary row.
	expected := current
	for i, x := range expected {
		if found := slices.IndexFunc(n.Notices, func(n platform.Notification) bool { return n.ID == x.ID }); found >= 0 {
			expected[i].Read = n.Notices[found].Read
		}
	}
	added := 0
	for _, x := range n.Notices {
		if _, known := prior[x.ID]; !known {
			added++
			want := n.Base + added
			if n.Seq-n.Base > runtimekernel.NoticesKept {
				want = n.Seq - runtimekernel.NoticesKept + added
			}
			if x.ID != fmt.Sprintf("n-%d", want) {
				return fmt.Errorf("notification result numbering differs")
			}
			expected = append(expected, x)
		}
	}
	if added != min(n.Seq-n.Base, runtimekernel.NoticesKept) {
		return fmt.Errorf("notification result omits an allocated notice")
	}
	if len(expected) > runtimekernel.NoticesKept {
		expected = expected[len(expected)-runtimekernel.NoticesKept:]
	}
	if !reflect.DeepEqual(expected, n.Notices) {
		return fmt.Errorf("notification result omits or reorders existing notices")
	}
	for _, effect := range n.Effects {
		if effect.ID == "" || effect.Endpoint == "" || effect.State != "pending" || effect.At.IsZero() {
			return fmt.Errorf("notification result has an invalid mail intent")
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedNotices(n *acceptedformat.Notices) {
	if n == nil {
		return
	}
	t.notices.Restore(slices.Clone(n.Notices), n.Seq)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.acted += n.Seq - n.Base
	for _, planned := range n.Effects {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}

// A caught child refusal is a savepoint, not a partial accepted transaction.
// This copies only private record/ledger/intent owners, never the live tenant.
func (d *stagedDecision) Attempt(decide func() (*pb.ChangeRecord, *kernel.Error)) (r *pb.ChangeRecord, err *kernel.Error) {
	saved := *d
	*d = *d.privateFork()
	d.attachPrivateHosts()
	defer func() {
		if p := recover(); p != nil {
			*d = saved
			d.attachPrivateHosts()
			// An unsupported effect is not a business refusal from the child.
			// It invalidates the entire top-level attempt; inventing a refused
			// provider answer would turn a supported business operation into a
			// durable, false rejection.
			panic(p)
		}
	}()
	r, err = decide()
	if err == nil && r != nil && len(d.events) > len(saved.events) && slices.ContainsFunc(d.events[len(saved.events):], func(event platform.Event) bool {
		return event.App == r.GetSubmission().GetAuthority() && event.Record.GetChangeId() == r.GetChangeId()
	}) {
		// A completed child decision must report its deferred business refusal
		// before its caller chooses a success/error path. An in-progress parent
		// transaction has no published receipt yet and keeps its final boundary.
		err = d.checkFinalConstraints()
	}
	if err != nil {
		*d = saved
		d.attachPrivateHosts()
	}
	return r, err
}

func (d *stagedDecision) privateFork() *stagedDecision {
	fork := *d
	fork.definitionPlans = nil
	fork.businessFacts = slices.Clone(d.businessFacts)
	for i := range fork.businessFacts {
		fork.businessFacts[i] = acceptedformat.CloneBusinessFact(fork.businessFacts[i])
	}
	fork.businessRefusals = maps.Clone(d.businessRefusals)
	for key, fact := range fork.businessRefusals {
		fork.businessRefusals[key] = acceptedformat.CloneBusinessFact(fact)
	}
	fork.callbackWrites = maps.Clone(d.callbackWrites)
	if d.callbackOrigin != nil {
		origin := *d.callbackOrigin
		origin.Output = slices.Clone(origin.Output)
		fork.callbackOrigin = &origin
	}
	fork.operationCancels = maps.Clone(d.operationCancels)
	fork.operationAnswers = maps.Clone(d.operationAnswers)
	for id, answer := range fork.operationAnswers {
		answer.Output = slices.Clone(answer.Output)
		if answer.Ref != nil {
			ref := *answer.Ref
			answer.Ref = &ref
		}
		fork.operationAnswers[id] = answer
	}
	// Prepared artifacts are immutable, but consuming a queue belongs to this
	// attempt. A refused child or probe must leave its parent's queue intact.
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
	}
	fork.inputIssues = maps.Clone(d.inputIssues)
	fork.constraintWrites = maps.Clone(d.constraintWrites)
	for refusal, issues := range fork.inputIssues {
		issues = slices.Clone(issues)
		for i := range issues {
			issues[i].Translations = maps.Clone(issues[i].Translations)
			issues[i].Values = maps.Clone(issues[i].Values)
			for name, raw := range issues[i].Values {
				issues[i].Values[name] = slices.Clone(raw)
			}
			issues[i].Path = slices.Clone(issues[i].Path)
			issues[i].RelatedPaths = slices.Clone(issues[i].RelatedPaths)
			for j := range issues[i].RelatedPaths {
				issues[i].RelatedPaths[j] = slices.Clone(issues[i].RelatedPaths[j])
			}
		}
		fork.inputIssues[refusal] = issues
	}
	fork.records = d.records.ForkRecords()
	fork.records.InheritDecision(d.records)
	fork.logs = map[*platform.Ledger]platform.ChangeLog{}
	for ledger, log := range d.logs {
		suspended := d.receiving[ledger]
		for id := range d.active {
			owner := d.tenant.app(id)
			if owner != nil && owner.AcceptedLedger() == ledger {
				suspended = true
				break
			}
		}
		if suspended {
			// Receive appends its parent's receipt after deciding the nested
			// child. The parent's live stack already holds this private log.
			fork.logs[ledger] = log
		} else {
			fork.logs[ledger] = log.ForkLog()
		}
	}
	fork.events, fork.notices = slices.Clone(d.events), slices.Clone(d.notices)
	fork.definitions = maps.Clone(d.definitions)
	fork.sequences, fork.allocated, fork.sequenceBases = maps.Clone(d.sequences), maps.Clone(d.allocated), maps.Clone(d.sequenceBases)
	fork.writers, fork.changed = cloneLists(d.writers), cloneLists(d.changed)
	fork.noticeEffects, fork.publications = slices.Clone(d.noticeEffects), maps.Clone(d.publications)
	fork.intents = slices.Clone(d.intents)
	fork.iterations = slices.Clone(d.iterations)
	fork.authorizations = slices.Clone(d.authorizations)
	fork.observations = slices.Clone(d.observations)
	fork.active = maps.Clone(d.active)
	fork.receiving = maps.Clone(d.receiving)
	fork.requests, fork.requestCounts = slices.Clone(d.requests), maps.Clone(d.requestCounts)
	descriptors, marks := d.connectors.State()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	fork.connectors = d.tenant.services.Kernel.Connectors()
	fork.connectors.Restore(descriptors, marks)
	fork.deliveries = slices.Clone(d.deliveries)
	fork.privateApps = map[string]platform.App{}
	fork.states, fork.stateBases = map[string]platform.AcceptedStateApp{}, map[string]json.RawMessage{}
	for id, base := range d.stateBases {
		fork.stateBases[id] = slices.Clone(base)
	}
	for id, owner := range d.states {
		// A suspended parent is still executing this receiver. Children cannot
		// re-enter its owner (Decide guards active IDs), so keep that receiver
		// instead of replacing it or taking its already-held decision lock.
		if d.active[id] || d.receiving[owner.AcceptedLedger()] {
			fork.states[id], fork.privateApps[id] = owner, owner
			continue
		}
		app, err := owner.ForkAcceptedState()
		copy, ok := app.(platform.AcceptedStateApp)
		if err != nil || !ok {
			unsupportedStagedEffect()
		}
		fork.states[id], fork.privateApps[id] = copy, app
	}
	for id, app := range d.privateApps {
		if d.active[id] || d.receiving[app.AcceptedLedger()] {
			fork.privateApps[id] = app
			continue
		}
		if fork.privateApps[id] != nil {
			continue
		}
		owner, ok := app.(platform.DecisionForkApp)
		if !ok {
			unsupportedStagedEffect()
		}
		copy, err := owner.ForkDecision()
		if err != nil || copy == nil || copy.Manifest().ID != id || copy.AcceptedLedger() != app.AcceptedLedger() {
			unsupportedStagedEffect()
		}
		fork.privateApps[id] = copy
	}
	for id, app := range fork.privateApps {
		if d.active[id] || d.receiving[app.AcceptedLedger()] {
			continue
		}
		(hostView{t: d.tenant, app: app, decision: &fork}).bindOwner(app)
	}
	fork.forkControlDecision(d.control)
	for ledger := range fork.logs {
		fork.bindPrivateFacts(ledger)
	}
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := d.privateFork()
	err := probe.runProbe(c, decide)
	if err != nil {
		// Carry only the refused answer's diagnostics across a read-only probe.
		// No private rows, intents, numbers or successful branch facts escape.
		if fact, ok := probe.businessRefusals[err]; ok {
			d.BusinessRefused(err, fact)
		} else if issues := probe.inputIssues[err]; len(issues) > 0 {
			d.InputRefused(err, issues)
		}
	}
	return err
}

// runProbe owns its mode and all host-managed writes. No live tenant or
// parent decision is switched into a different mode while rules run.
func (d *stagedDecision) runProbe(c platform.Caller, decide func(platform.Caller) *kernel.Error) (err *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(stagedEffectPanic); !ok {
				panic(failure)
			}
			err = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")
		}
	}()
	d.probing = true
	err = decide(platform.NewCaller(d, c.Member, c.App, c.Replaying, c.Automation))
	if err == nil {
		err = d.failure
	}
	if err == nil {
		err = d.checkFinalConstraints()
	}
	return err
}

func cloneLists(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, list := range src {
		dst[key] = slices.Clone(list)
	}
	return dst
}

func (d *stagedDecision) stageObservers() *kernel.Error {
	if d.observed {
		return d.failure
	}
	count := len(d.events)
	for _, e := range slices.Clone(d.events) {
		for _, app := range d.tenant.liveApps() {
			_, observer := app.(platform.Observer)
			if !observer {
				continue
			}
			owner, ok := d.stateApp(app).(platform.AcceptedObserver)
			if !ok {
				unsupportedStagedEffect()
			}
			id := app.Manifest().ID
			c := platform.NewCaller(d, platform.Member{ID: "app:" + id, Tenant: d.tenant.ID}, id, false, true)
			raw, err := owner.PlanObserved(c, e, d.tenant.protocolEvents(e))
			if err != nil || owner.ValidateObserved(raw) != nil {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			d.observations = append(d.observations, acceptedformat.Observation{App: id, Body: raw})
		}
	}
	if len(d.events) != count {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d.observed = true
	return d.failure
}

func (t *Tenant) validateAcceptedObservations(saved []acceptedformat.Observation) error {
	for _, item := range saved {
		owner, ok := t.app(item.App).(platform.AcceptedObserver)
		if !ok || len(item.Body) == 0 {
			return fmt.Errorf("accepted observation needs missing projection %s", item.App)
		}
		if err := owner.ValidateObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedObservations(saved []acceptedformat.Observation) error {
	for _, item := range saved {
		if err := t.app(item.App).(platform.AcceptedObserver).ApplyObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func (d *stagedDecision) savedChangePlan() (acceptedformat.ChangePlan, error) {
	plan := acceptedformat.ChangePlan{Notices: d.savedNotices(), Intents: slices.Clone(d.intents),
		Deliveries: slices.Clone(d.deliveries), Sequences: maps.Clone(d.allocated),
		SequenceBases: maps.Clone(d.sequenceBases), Unique: d.savedUniqueChanges()}
	var err error
	plan.States, err = d.savedStates()
	if err == nil {
		plan.Control, err = d.savedControl()
	}
	return plan, err
}

// This preparation changes no live owner. In particular a valid child cannot
// advance its ledger before the outer result's remaining owners are checked.
type preparedAcceptedChanges struct {
	plan    acceptedformat.ChangePlan
	records *records.Store
	child   *preparedAcceptedBatch
}

func (t *Tenant) prepareAcceptedChanges(plan acceptedformat.ChangePlan) (*preparedAcceptedChanges, error) {
	if err := t.validateAcceptedControl(plan.Control); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedNotices(plan.Notices); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedIntents(plan.Intents); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedStates(plan.States); err != nil {
		return nil, err
	}
	if err := t.validateControlDeliveries(plan.Control, plan.Deliveries); err != nil {
		return nil, err
	}
	if err := t.sequences.Check(plan.SequenceBases, plan.Sequences); err != nil {
		return nil, err
	}
	prepared := &preparedAcceptedChanges{plan: plan}
	var err error
	if len(plan.Changes) > 0 {
		batch, _, err := acceptedformat.DecodeBatch(plan.Changes)
		if err != nil || batch.Tenant != t.ID || !batch.At.Equal(plan.At) {
			return nil, fmt.Errorf("accepted result has an invalid child decision: %v", err)
		}
		owner := t.app(batch.App)
		if owner == nil {
			return nil, fmt.Errorf("accepted result needs missing child authority")
		}
		direct := make(map[string]bool, len(plan.Rows))
		for _, row := range plan.Rows {
			direct[row.Type+"/"+row.ID] = true
		}
		for _, row := range batch.Rows {
			if direct[row.Type+"/"+row.ID] {
				return nil, fmt.Errorf("accepted result writes the same direct and child record")
			}
		}
		for _, row := range batch.DirectRows {
			if direct[row.Row.Type+"/"+row.Row.ID] {
				return nil, fmt.Errorf("accepted result writes the same direct and child record")
			}
		}
		prepared.child, err = t.prepareAcceptedBatch(owner.AcceptedLedger(), plan.Changes)
		if err != nil || prepared.child == nil {
			return nil, fmt.Errorf("accepted child result: %v (already applied=%t)", err, prepared.child == nil)
		}
	}
	if len(plan.Rows) > 0 {
		base := t.records
		if prepared.child != nil {
			base = prepared.child.records
		}
		prepared.records, err = t.prepareAcceptedDirectRowsOn(base, plan.Rows, plan.Member, plan.At)
		if err != nil {
			return nil, err
		}
		if prepared.child != nil {
			// Flatten this private fork onto the child's original live parent.
			// Keep its write set as well as the outer direct observations, so
			// promotion applies both together and invalidates every changed row.
			prepared.records.ExtendDecision(base)
			prepared.child.records = prepared.records
			prepared.records = nil
		}
	}
	if plan.CheckUnique {
		records := prepared.records
		if prepared.child != nil {
			records = prepared.child.records
		}
		if records == nil {
			records = t.records.ForkRecords()
		}
		if err := validateUniqueReplacements(records, plan.Unique); err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

func (p *preparedAcceptedChanges) apply(t *Tenant) error {
	if p.child != nil {
		if applied, err := t.promoteAcceptedBatch(p.child); err != nil || !applied {
			return fmt.Errorf("accepted child application: %v (applied=%t)", err, applied)
		}

	}
	if p.records != nil {
		if err := t.records.PromoteRecords(p.records); err != nil {
			return err
		}
	}
	if err := t.applyAcceptedControl(p.plan.Control); err != nil {
		return err
	}
	if err := t.applyAcceptedStates(p.plan.States); err != nil {
		return err
	}
	if err := t.applyAcceptedDeliveries(p.plan.Deliveries); err != nil {
		return err
	}
	t.applyAcceptedNotices(p.plan.Notices)
	t.installAcceptedIntents(p.plan.Intents)
	t.sequences.Set(p.plan.Sequences)
	return nil
}

func (p *preparedAcceptedChanges) publish(t *Tenant) {
	if p.child != nil {
		t.publishAcceptedBatch(p.child.result)
	}
}

// Capture the same full record image and predecessor for connector inputs,
// effect answers and callback observations, in the supplied stable order.
func captureDirectRows(d *stagedDecision, owner string, refs []string, exclude map[string]bool, source string) ([]acceptedformat.InputRow, error) {
	d.records.Lock()
	defer d.records.Unlock()
	d.records.Parent().Lock()
	defer d.records.Parent().Unlock()
	if refs == nil {
		refs = slices.Sorted(d.records.Writes())
	}
	var rows []acceptedformat.InputRow
	for _, ref := range refs {
		if exclude[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.Type(typ)
		if !ok || et == nil || et.Info().App != owner || et.Rows().Get(id) == nil {
			return nil, fmt.Errorf("%s writes a foreign or undeclared record", source)
		}
		var prior *records.Row
		if old := d.records.Parent().Type(typ); old != nil {
			prior = old.Rows().Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.Rows().Get(id), prior)
		if err != nil {
			return nil, err
		}
		row := acceptedformat.InputRow{Image: image, App: owner}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// prepareAcceptedDirectRows verifies and forks record changes caused by a
// top-level input without a child ledger decision (connector facts or an
// external effect's answer). The caller promotes only after all other owners
// of the same result have passed their predecessor checks.
func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	return t.prepareAcceptedDirectRowsOn(t.records, rows, member, at)
}

func (t *Tenant) prepareAcceptedDirectRowsOn(base *records.Store, rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	draft := base.ForkRecords()
	draft.Lock()
	defer draft.Unlock()
	for _, image := range rows {
		et := draft.Type(image.Type)
		if et == nil || et.Info().App != image.App {
			return nil, fmt.Errorf("direct result needs missing record type %s", image.Type)
		}
		prior := et.Rows().Get(image.ID)
		before, previousHistory, revision := "", 0, uint32(0)
		if image.HistoryRef != nil {
			if err := records.CheckHistoryPredecessor(image.Image, prior); err != nil {
				return nil, err
			}
			var err error
			before, err = records.Predecessor(image.Type, image.ID, prior)
			if err != nil {
				return nil, err
			}
			if before != image.Before {
				return nil, fmt.Errorf("direct record image predecessor differs")
			}
			if prior != nil {
				revision = records.Header(prior.Value).Revision
			}
		} else {
			if prior != nil {
				row, err := records.FullImage(image.Type, image.ID, prior)
				if err != nil {
					return nil, err
				}
				before, err = platform.CanonicalDigest(row)
				if err != nil {
					return nil, err
				}
				previousHistory, revision = len(prior.RecordHistory()), records.Header(prior.Value).Revision
			}
			if image.Before != before || len(image.History) <= previousHistory {
				return nil, fmt.Errorf("direct record predecessor differs")
			}
			if prior != nil {
				old, err := platform.CanonicalDigest(image.History[:previousHistory])
				actual, actualErr := platform.CanonicalDigest(prior.RecordHistory())
				if err != nil || actualErr != nil || old != actual {
					return nil, fmt.Errorf("direct record history predecessor differs")
				}
			}
		}
		for _, change := range image.History[previousHistory:] {
			if change.Change != "" || change.Schema == "" || change.By != member || !change.At.Equal(at) {
				return nil, fmt.Errorf("direct record has a foreign change")
			}
		}
		value := reflect.New(et.Info().Go).Elem()
		decode, err := records.DecodeFormattedImage(et.Info(), image.Value, image.Projection, draft.Current().Release(), image.Format)
		if err != nil {
			return nil, err
		}
		if err := platform.DecodeJSON(decode, value.Addr().Interface(), "direct record"); err != nil {
			return nil, err
		}
		if err := checkRunApplicationBindings(value.Interface()); err != nil {
			return nil, err
		}
		rec := records.Header(value)
		last := image.History[len(image.History)-1]
		if rec.ID != image.ID || rec.Revision != revision || rec.Changed != (platform.Stamp{
			By: member, At: at, Change: ""}) ||
			prior != nil && rec.Created != records.Header(prior.Value).Created ||
			prior == nil && rec.Created != (platform.Stamp{By: member,
				At: image.History[0].At, Change: ""}) ||
			last.Schema == "" {
			return nil, fmt.Errorf("direct record image differs from history")
		}
		recovered := &records.Row{Value: value, Format: image.Format, History: records.CopyHistory(image.History), Projection: records.CloneProjection(image.Projection), Extraction: image.Extraction}
		if image.HistoryRef != nil {
			if err := records.AppendRowHistory(recovered, prior, image.History); err != nil {
				return nil, err
			}
		}
		recovered.RetainOriginal(image.Value)
		et.SetRow(image.ID, recovered)
		draft.TrackWrite(image.Type + "/" + image.ID)
		if et.Knowledge() {
			draft.MarkDirty(image.Type + "/" + image.ID)
		}
	}
	return draft, nil
}

func (d *stagedDecision) stateApp(app platform.App) platform.App {
	id := app.Manifest().ID
	if existing := d.privateApps[id]; existing != nil {
		return existing
	}
	var fork platform.App
	var before json.RawMessage
	var err error
	owner, ownsState := app.(platform.AcceptedStateApp)
	if ownsState {
		before, err = owner.AcceptedState()
		if err == nil {
			fork, err = owner.ForkAcceptedState()
		}
	} else if execution, ok := app.(platform.DecisionForkApp); ok {
		fork, err = execution.ForkDecision()
	} else {
		return app
	}
	if err != nil || fork == nil || fork.Manifest().ID != id || fork.AcceptedLedger() != app.AcceptedLedger() {
		unsupportedStagedEffect()
	}
	if d.privateApps == nil {
		d.privateApps = map[string]platform.App{}
	}
	d.privateApps[id] = fork
	(hostView{t: d.tenant, app: fork, decision: d}).bindOwner(fork)
	if ownsState {
		copy, ok := fork.(platform.AcceptedStateApp)
		if !ok {
			unsupportedStagedEffect()
		}
		if d.states == nil {
			d.states = map[string]platform.AcceptedStateApp{}
			d.stateBases = map[string]json.RawMessage{}
		}
		d.states[id], d.stateBases[id] = copy, slices.Clone(before)
	}
	return fork
}

func (d *stagedDecision) attachPrivateHosts() {
	for _, app := range d.privateApps {
		(hostView{t: d.tenant, app: app, decision: d}).bindOwner(app)
	}
}

// Version choices belong to the private Flow instance that actually decided.
// They are saved with the result before the live registry is changed.
func (d *stagedDecision) flowVersions() map[string]int {
	if private, ok := d.privateApps[platform.FlowApp].(platform.Processes); ok {
		return private.Versions()
	}
	return nil
}

func (d *stagedDecision) savedStates() ([]acceptedformat.State, error) {
	var out []acceptedformat.State
	for _, id := range slices.Sorted(maps.Keys(d.states)) {
		owner := d.states[id]
		image, err := owner.AcceptedState()
		if err != nil {
			return nil, err
		}
		if err := owner.ValidateAcceptedState(image); err != nil {
			return nil, err
		}
		before, err := platform.CanonicalDigest(d.stateBases[id])
		if err != nil {
			return nil, err
		}
		after, err := platform.CanonicalDigest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			out = append(out, acceptedformat.State{App: id, Before: before, Image: image})
		}
	}
	return out, nil
}

func (t *Tenant) validateAcceptedStates(states []acceptedformat.State) error {
	seen := map[string]bool{}
	for _, state := range states {
		owner, ok := t.app(state.App).(platform.AcceptedStateApp)
		if !ok || seen[state.App] || state.Before == "" || len(state.Image) == 0 {
			return fmt.Errorf("accepted result has a duplicate or unknown state owner %s", state.App)
		}
		seen[state.App] = true
		if err := owner.ValidateAcceptedState(state.Image); err != nil {
			return err
		}
		prior, err := owner.AcceptedState()
		if err != nil {
			return err
		}
		hash, err := platform.CanonicalDigest(prior)
		if err != nil || hash != state.Before {
			return fmt.Errorf("accepted state predecessor differs for %s", state.App)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedStates(states []acceptedformat.State) error {
	for _, state := range states {
		if err := t.app(state.App).(platform.AcceptedStateApp).ApplyAcceptedState(state.Image); err != nil {
			return err
		}
	}
	return nil
}

func uniqueReplacements(changes map[string]map[string]string) []journal.StateUnique {
	out := make([]journal.StateUnique, 0, len(changes))
	for _, ref := range slices.Sorted(maps.Keys(changes)) {
		typ, id, _ := strings.Cut(ref, "/")
		out = append(out, journal.StateUnique{Type: typ, ID: id, Keys: changes[ref]})
	}
	return out
}

func (d *stagedDecision) savedUniqueChanges() []journal.StateUnique {
	d.records.Lock()
	defer d.records.Unlock()
	return uniqueReplacements(d.records.UniqueChanges())
}

func validateUniqueReplacements(store *records.Store, saved []journal.StateUnique) error {
	store.Lock()
	defer store.Unlock()
	expected := store.UniqueChanges()
	if len(saved) != len(expected) {
		return fmt.Errorf("record unique-key replacements differ")
	}
	seen := map[string]bool{}
	for _, row := range saved {
		ref := row.Type + "/" + row.ID
		keys, ok := expected[ref]
		if !ok || seen[ref] || !maps.Equal(keys, row.Keys) {
			return fmt.Errorf("record unique-key replacement differs for %s", ref)
		}
		seen[ref] = true
	}
	return nil
}
