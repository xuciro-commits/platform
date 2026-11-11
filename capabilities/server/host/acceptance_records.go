package host

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
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
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
		return nil, fmt.Errorf("batch constraints: %w", err)
	}
	if receipt == nil || len(d.events) == 0 || d.failure != nil {
		return nil, fmt.Errorf("batch needs accepted decisions and all written records")
	}
	if err := d.records.ValidateChangedLinkConstraints(); err != nil {
		return nil, fmt.Errorf("batch links: %w", err)
	}
	result := acceptedformat.Batch{Version: 2, Kind: "record-batch", Tenant: d.tenant.ID,
		Definitions: slices.Clone(d.installations),
		App:         app, At: at.UTC(), Sequences: maps.Clone(d.allocated), SequenceBases: maps.Clone(d.sequenceBases),
		Notices: d.notices.Saved(), Intents: slices.Clone(d.intents), Observations: slices.Clone(d.observations)}
	var err error
	result.Authorizations, err = d.savedAuthorizations()
	if err != nil {
		return nil, fmt.Errorf("batch authorizations: %w", err)
	}
	result.Control, err = d.savedControl()
	if err != nil {
		return nil, fmt.Errorf("batch control: %w", err)
	}
	if result.Control != nil {
		result.Version = 3
	}
	result.Operations, err = d.savedOperationCancellations()
	if err != nil {
		return nil, fmt.Errorf("batch operations: %w", err)
	}
	result.States, err = runtimekernel.SavedStates(d.states, d.stateBases)
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
	result.SchemaVersion = d.schemaVersion
	result.RequestHash, err = acceptedformat.SubmissionHash(request, result.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("batch request hash: %w", err)
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
			return nil, fmt.Errorf("batch event %s: %w", event.App, err)
		}
		plan, refusal := d.routingTenant().planAcceptedEvent(event)
		if refusal != nil {
			return nil, fmt.Errorf("batch event plan %s: %w", event.App, refusal)
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
			return nil, fmt.Errorf("batch direct rows: %w", err)
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
			return nil, fmt.Errorf("batch image %s/%s: %w", typ, id, err)
		}
		row := acceptedformat.BatchRow{Image: image, App: et.Info().App, Authorities: slices.Clone(d.writers[ref]), HistoryBase: image.HistoryRef.Base}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, fmt.Errorf("batch predecessor %s/%s: %w", typ, id, err)
		}
		result.Rows = append(result.Rows, row)
	}
	result.Digest, err = acceptedformat.DigestBatch(result)
	if err != nil {
		return nil, fmt.Errorf("batch digest: %w", err)
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

type preparedAcceptedBatch struct {
	installation *Tenant
	result       acceptedformat.Batch
	records      *records.Store
	ledgers      map[string]*platform.Ledger
	changes      map[string]*pb.ChangeRecord
	key          string
	raw          []byte
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
	if len(result.Claims) > 0 {
		if _, err := t.sourceFacts(sub.GetTarget().GetType(), sub.GetTarget().GetId(), receipt.GetChangeId(), &preparedSourceClaim{origin: result.Origin, claims: result.Claims}); err != nil {
			return nil, err
		}
	}
	key := result.App + "/" + sub.GetIdempotencyKey()
	if saved := t.Committed.Answer(key); len(saved) > 0 {
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
		return nil, nil
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
		if _, refused := t.Committed.Refusal(decision.Event.App + "/" + r.GetSubmission().GetIdempotencyKey()); refused {
			return nil, fmt.Errorf("record batch reused a refused child key")
		}
		changes[decision.Event.App+"/"+r.GetChangeId()] = r
	}
	draft := t.Projection.ForkRecords()
	if err := t.validateAcceptedControl(result.Control); err != nil {
		return nil, err
	}
	if err := t.notices.Check(result.Notices); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedIntents(result.Intents); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedObservations(result.Observations); err != nil {
		return nil, err
	}
	if err := runtimekernel.CheckStates(t.states, result.States); err != nil {
		return nil, err
	}
	if err := t.validateOperationCancellations(result.Operations); err != nil {
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
	var installation *Tenant
	if len(result.Definitions) > 0 {
		installation, err = t.prepareBatchDefinitions(draft, result.Definitions)
		if err != nil {
			return nil, err
		}
		draft = installation.Projection
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
		if err := runtimekernel.ValidateUniqueReplacements(draft, result.Unique); err != nil {
			return nil, err
		}
	}
	if err := t.sequences.Check(result.SequenceBases, result.Sequences); err != nil {
		return nil, err
	}
	checks := map[string]*kernel.ChangeLog{}
	for app, ledger := range ledgers {
		checks[app] = ledger.ForkAcceptedChanges()
	}
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if changed, err := checks[decision.Event.App].ApplyAccepted(r); err != nil || !changed {
			return nil, fmt.Errorf("record batch ledger validation: %v (new=%t)", err, changed)
		}
	}
	return &preparedAcceptedBatch{result: result, records: draft, ledgers: ledgers, changes: changes, key: key, raw: raw, installation: installation}, nil
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
	if err := t.Projection.PromoteRecords(draft); err != nil {
		return false, err
	}
	if installed := prepared.installation; installed != nil {
		t.setDefinitions(installed.definitions)
		t.owner = t.rehome(installed.owner)
		registry, err := t.assetRegistryLocked()
		if err != nil {
			return false, err
		}
		for _, installation := range result.Definitions {
			plan, err := releaseowner.InstallationPlan(registry, installation)
			if err != nil {
				return false, err
			}
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
				return false, err
			}
		}
	}
	if len(result.Claims) > 0 {
		answer := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(result.Receipt, answer); err != nil {
			return false, err
		}
		sub := answer.GetSubmission()
		if err := t.applySourceClaims(sub.GetTarget().GetType(), sub.GetTarget().GetId(), answer.GetChangeId(), result.At, &preparedSourceClaim{origin: result.Origin, claims: result.Claims}); err != nil {
			return false, err
		}
	}
	if slices.ContainsFunc(result.Rows, func(row acceptedformat.BatchRow) bool { return row.Type == build.AppType }) {
		t.DefinitionEpoch.Add(1)
	}
	if err := t.applyAcceptedControl(result.Control); err != nil {
		return false, err
	}
	if err := runtimekernel.ApplyStates(t.states, result.States); err != nil {
		return false, err
	}
	if err := t.applyOperationCancellations(result.Operations); err != nil {
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
		t.Committed.SaveDecisionDefinition(child.Event.App, acceptedformat.ReceiptID(child.Receipt), child.Definition)
	}
	if result.SchemaVersion != nil {
		_, request, _ := strings.Cut(key, "/")
		t.Committed.SaveSchemaVersion(result.App, request, result.SchemaVersion)
	}
	answer := new(pb.ChangeRecord)
	_ = protojson.Unmarshal(result.Receipt, answer)
	input, _ := acceptedformat.BatchSubmission(result, answer)
	if result.HasSavedAnswer(input) {
		t.Committed.SaveAnswer(key, raw)
	}
	return true, nil
}

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
		_ = protojson.Unmarshal(decision.Receipt, r)
		t.publishAccepted(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed},
			decision.Event, 2)
	}
}

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
		queued, _ := t.outbound.Get(source.ID)
		current := queued.Effect
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
