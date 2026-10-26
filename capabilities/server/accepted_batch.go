package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/construction"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/platform"
)

func (d *stagedDecision) batchResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.checkFinalConstraints(); err != nil {
		return nil, err
	}
	if receipt == nil || len(d.events) == 0 || d.failure != nil {
		return nil, fmt.Errorf("batch needs accepted decisions and all written records")
	}
	if err := d.records.validateChangedLinkConstraints(); err != nil {
		return nil, err
	}
	result := resultformat.Batch{Version: 2, Kind: "record-batch", Tenant: d.tenant.ID,
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
	result.RequestHash, err = resultformat.SubmissionHash(request, result.SchemaVersion)
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
		plan := d.routingTenant().planAcceptedEvent(event)
		plan.Observed = d.observed
		plan.Hops = d.hops
		if plan.Hops > resultformat.MaxHops {
			plan.Stopped, plan.Subscribers = plan.Subscribers, nil
		}
		result.Decisions = append(result.Decisions, resultformat.Decision{Receipt: raw, Event: plan, Definition: d.definitions[event.App+"/"+event.Record.GetChangeId()],
			Publication: d.publications[event.App+"/"+event.Record.GetChangeId()]})
		if publication := result.Decisions[len(result.Decisions)-1].Publication; publication != nil {
			copied := *publication
			if et := d.records.types[event.Record.GetSubmission().GetTarget().GetType()]; et != nil {
				copied.Format = et.info.RecordFormat
			}
			result.Decisions[len(result.Decisions)-1].Publication = &copied
		}
		for _, ref := range event.Changed {
			if !d.records.writes[ref] {
				return nil, fmt.Errorf("batch event names an unwritten record")
			}
			seen[ref] = true
		}
	}
	if len(seen) != len(d.records.writes) {
		result.DirectRows, err = d.savedCallbackRows(seen)
		if err != nil {
			return nil, err
		}
		if len(seen)+len(result.DirectRows) != len(d.records.writes) {
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
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	for _, ref := range slices.Sorted(maps.Keys(d.records.writes)) {
		if !seen[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.rows.Get(id) == nil {
			return nil, fmt.Errorf("batch includes an undeclared record")
		}
		var prior *records.Row
		if old := d.records.parent.types[typ]; old != nil {
			prior = old.rows.Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.rows.Get(id), prior)
		if err != nil {
			return nil, err
		}
		row := resultformat.BatchRow{Image: image, App: et.info.App, Authorities: slices.Clone(d.writers[ref]), HistoryBase: image.HistoryRef.Base}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, err
		}
		result.Rows = append(result.Rows, row)
	}
	result.Digest, err = resultformat.DigestBatch(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := resultformat.DecodeBatch(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Validate every row, counter and ledger on private forks before promotion.
// A late invalid child cannot advance an earlier authoritative ledger.
type preparedAcceptedBatch struct {
	result  resultformat.Batch
	records *recordStore
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
	result, receipt, err := resultformat.DecodeBatch(raw)
	if err != nil {
		return nil, err
	}
	owner, ok := t.app(result.App).(platform.App)
	if result.Tenant != t.ID || !ok || owner.AcceptedLedger() != l {
		return nil, fmt.Errorf("record batch belongs to another tenant or ledger")
	}
	sub, _ := resultformat.BatchSubmission(result, receipt)
	key := result.App + "/" + sub.GetIdempotencyKey()
	if saved := t.committed.answer(key); len(saved) > 0 {
		prior, answer, err := resultformat.DecodeBatch(saved)
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
		if _, refused := t.committed.refusal(decision.Event.App + "/" + r.GetSubmission().GetIdempotencyKey()); refused {
			return nil, fmt.Errorf("record batch reused a refused child key")
		}
		changes[decision.Event.App+"/"+r.GetChangeId()] = r
	}
	draft := t.records.forkRecords()
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
		r := changes[decision.Event.App+"/"+resultformat.ReceiptID(decision.Receipt)]
		if r == nil {
			return nil, fmt.Errorf("publication has no receipt")
		}
		et := draft.types[r.GetSubmission().GetTarget().GetType()]
		if et == nil {
			return nil, fmt.Errorf("publication record type is unavailable")
		}
		decoded, err := records.DecodeFormattedImage(et.info, decision.Publication.Image, nil, draft.readRelease, decision.Publication.Format)
		if err != nil {
			return nil, err
		}
		copy := *decision.Publication
		copy.Image = decoded
		copy.Format = et.info.RecordFormat
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
	if err := draft.validateChangedLinkConstraints(); err != nil {
		return nil, err
	}
	if result.Version >= 8 {
		if err := validateUniqueReplacements(draft, result.Unique); err != nil {
			return nil, err
		}
	}
	if err := t.sequences.check(result.SequenceBases, result.Sequences); err != nil {
		return nil, err
	}
	checks := map[string]platform.ChangeLog{}
	for app, ledger := range ledgers {
		checks[app] = ledger.ForkAcceptedChanges()
	}
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+resultformat.ReceiptID(decision.Receipt)]
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
		r := changes[decision.Event.App+"/"+resultformat.ReceiptID(decision.Receipt)]
		if changed, err := ledgers[decision.Event.App].ApplyAcceptedChange(r); err != nil || !changed {
			return false, fmt.Errorf("record batch ledger application: %v (new=%t)", err, changed)
		}
	}
	if err := t.records.promoteRecords(draft); err != nil {
		return false, err
	}
	// Application entry availability is owned by its current Build record.
	// The cached discovery must move with that accepted record as well as with
	// installed definition bytes, including withdrawal and restoration.
	if slices.ContainsFunc(result.Rows, func(row resultformat.BatchRow) bool { return row.Type == construction.AppType }) {
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
	t.sequences.set(result.Sequences)
	for _, child := range result.Decisions {
		t.committed.saveDecisionDefinition(child.Event.App, resultformat.ReceiptID(child.Receipt), child.Definition)
	}
	if result.SchemaVersion != nil {
		_, request, _ := strings.Cut(key, "/")
		t.committed.saveSchemaVersion(result.App, request, result.SchemaVersion)
	}
	answer := new(pb.ChangeRecord)
	_ = protojson.Unmarshal(result.Receipt, answer)
	input, _ := resultformat.BatchSubmission(result, answer)
	if result.HasSavedAnswer(input) {
		t.committed.saveAnswer(key, raw)
	}
	return true, nil
}

// applyBatchRows writes the batch's row images into the forked records, each
// under a ledger that declares its type, keeping the history it extends.
func (t *Tenant) applyBatchRows(draft *recordStore, result resultformat.Batch, ledgers map[string]*platform.Ledger, changes map[string]*pb.ChangeRecord) error {
	draft.mu.Lock()
	defer draft.mu.Unlock()
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
		et := draft.types[image.Type]
		if et == nil || et.info.App != image.App {
			return fmt.Errorf("record batch needs installed entity %s", image.Type)
		}
		prior := et.rows.Get(image.ID)
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
				before, err = acceptance.Digest(row)
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
				oldDigest, oldErr := acceptance.Digest(prior.RecordHistory())
				savedDigest, savedErr := acceptance.Digest(image.History[:baseHistory])
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
		value := reflect.New(et.info.Go).Elem()
		decode, err := records.DecodeFormattedImage(et.info, image.Value, image.Projection, draft.readRelease, image.Format)
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
		et.setRow(rec.ID, recovered)
		draft.writes[image.Type+"/"+rec.ID] = true
		for _, change := range image.History[baseHistory:] {
			draft.recentChanges.Remember(image.App+"/"+change.Change, image.Type+"/"+rec.ID)
		}
		if et.knowledge {
			draft.dirty.Set(image.Type+"/"+rec.ID, true)
		}
	}
	return nil
}

func (t *Tenant) publishAcceptedBatch(result resultformat.Batch) {
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		_ = protojson.Unmarshal(decision.Receipt, r) // already checked
		t.publishAccepted(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed},
			decision.Event, 2)
	}
}
