package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/internal/construction"

	"context"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/host"
	"platformserver/internal/records"
	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

// sealResult preserves the complete decision. The compact record format is
// only an encoding optimisation; its writer must account for every effect.
// The batch format also handles history that no longer fits a compact result.
func (d *stagedDecision) sealResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.checkFinalConstraints(); err != nil {
		return nil, err
	}
	owner := d.tenant.app(app)
	if owner == nil || request.GetAuthority() != app {
		return nil, fmt.Errorf("decision has no matching submission owner")
	}
	action, declared := owner.Manifest().Actions.Action(request.GetSchema().GetName())
	if !declared || action.Target != request.GetTarget().GetType() {
		return nil, fmt.Errorf("submission target differs from its declared action")
	}
	if len(d.businessFacts) == 0 && !strings.HasPrefix(request.GetCorrelationId(), "correction:") && proto.Equal(request, receipt.GetSubmission()) {
		if raw, err := d.result(app, receipt, at); err == nil {
			return raw, nil
		}
	}
	return d.batchResult(app, request, receipt, at)
}

func (d *stagedDecision) result(app string, receipt *pb.ChangeRecord, submittedAt time.Time) ([]byte, error) {
	if d.control != nil || d.failure != nil || len(d.allocated) > 0 || d.noticeChanged || len(d.noticeEffects) > 0 ||
		len(d.intents) > 0 || len(d.observations) > 0 || len(d.states) > 0 ||
		len(d.deliveries) > 0 || len(d.operationCancels) > 0 ||
		d.observed && len(d.tenant.observers) > 0 {
		return nil, fmt.Errorf("decision needs a result carrying all state and intents")
	}
	if receipt == nil || len(d.events) != 1 || len(d.records.writes) != 1 || len(d.logs) != 1 {
		return nil, fmt.Errorf("one accepted record and event are required")
	}
	accepted := false
	for _, log := range d.logs {
		accepted = proto.Equal(log.Last(receipt.GetSubmission().GetTenantId()), receipt)
	}
	if !accepted {
		return nil, fmt.Errorf("decision has no staged kernel receipt")
	}
	typ, id := receipt.GetSubmission().GetTarget().GetType(), receipt.GetSubmission().GetTarget().GetId()
	ref := typ + "/" + id
	if !d.records.writes[ref] || d.events[0].App != app || !proto.Equal(d.events[0].Record, receipt) ||
		len(d.events[0].Changed) != 1 || d.events[0].Changed[0] != ref {
		return nil, fmt.Errorf("decision has other record or event effects")
	}
	image, err := func() (records.Image, error) {
		d.records.mu.Lock()
		defer d.records.mu.Unlock()
		et := d.records.types[typ]
		if et == nil || et.info.App != app || et.rows.Get(id) == nil {
			return records.Image{}, fmt.Errorf("decision has no declared record image")
		}
		if et.rows.Get(id).Projection != nil {
			return records.Image{}, fmt.Errorf("versioned migration row requires the complete record batch")
		}
		var prior *records.Row
		if old := d.records.parent.types[typ]; old != nil {
			prior = old.rows.Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.rows.Get(id), prior)
		if err == nil {
			image.Predecessor, err = records.Predecessor(typ, id, prior)
		}
		return image, err
	}()
	if err != nil {
		return nil, err
	}
	raw, err := protojson.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	hash, err := resultformat.SubmissionHash(receipt.GetSubmission(), d.schemaVersion)
	if err != nil {
		return nil, err
	}
	version := resultformat.RecordVersion
	kind := "record"
	if publisher, ok := d.tenant.app(app).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "publication"
	} else if construction.CallbackFreeTransition(d.tenant.app(app).Manifest(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "pure-transition"
	}
	plan, refusal := d.tenant.planAcceptedEvent(d.events[0])
	if refusal != nil {
		return nil, refusal
	}
	result := resultformat.Record{Version: version, Tenant: receipt.GetSubmission().GetTenantId(),
		Definition: d.definitions[app+"/"+receipt.GetChangeId()],
		Kind:       kind, App: app, At: submittedAt.UTC(), RequestHash: hash, Receipt: raw, SchemaVersion: d.schemaVersion,
		Row:   image,
		Event: plan, Authorizations: slices.Clone(d.authorizations)}
	result.Unique = d.savedUniqueChanges()
	result.Digest, err = resultformat.DigestRecord(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := resultformat.DecodeRecord(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// JSONB can reorder object keys; normalize even raw record and receipt JSON
// before computing the corruption-detection digest. This is not an
// authentication tag against an actor who can rewrite the database.

// applyAcceptedResult reconstructs one record and its kernel receipt without
// calling application decision code. A version-three publication additionally
// installs the committed definition. The tenant lock must be held.
func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	result, receipt, err := resultformat.DecodeRecord(raw)
	if err != nil {
		return false, err
	}
	if result.Tenant != t.ID || l == nil || !slices.ContainsFunc(l.Declarations(), func(d *pb.AuthorityDeclaration) bool {
		return d.GetTenantId() == result.Tenant && d.GetAuthorityId() == result.App && d.GetDataClass() == result.Row.Type
	}) {
		return false, fmt.Errorf("accepted result belongs to another tenant or authority")
	}
	s := t.records
	draft := s.forkRecords()
	draft.mu.Lock()
	et := draft.types[result.Row.Type]
	if et == nil || et.info.App != result.App {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted result needs installed entity %s", result.Row.Type)
	}
	value := reflect.New(et.info.Go).Elem()
	decoded, err := records.DecodeFormattedImage(et.info, result.Row.Value, result.Row.Projection, draft.readRelease, result.Row.Format)
	if err != nil {
		draft.mu.Unlock()
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Addr().Interface()); err != nil {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record image: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record has trailing data")
	}
	if err := checkRunApplicationBindings(value.Interface()); err != nil {
		draft.mu.Unlock()
		return false, err
	}
	rec := records.Header(value)
	if rec.ID != result.Row.ID || rec.Revision != receipt.GetRevision() ||
		rec.Changed.By != receipt.GetSubmission().GetPrincipalId() ||
		rec.Changed.Change != receipt.GetChangeId() ||
		!rec.Changed.At.Equal(receipt.GetRecordedTime().AsTime()) {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record image differs from receipt")
	}
	var publisher platform.AcceptedPublisher
	if result.Version == 3 || result.Version == 5 && result.Kind == "publication" {
		schema := receipt.GetSubmission().GetSchema().GetName()
		var ok bool
		publisher, ok = t.app(result.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted publication has no installer")
		}
		if err := publisher.ValidateAcceptedPublication(schema, decoded); err != nil {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted publication is invalid: %w", err)
		}
	}
	prior := et.rows.Get(rec.ID)
	if prior != nil && reflect.DeepEqual(prior.Value.Interface(), value.Interface()) &&
		records.ImageAlreadyApplied(prior, result.Row) {
		draft.mu.Unlock()
		changed, err := l.ApplyAcceptedChange(receipt)
		if err == nil {
			t.committed.saveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
			t.committed.saveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
		}
		return changed, err
	}
	if result.Version == 5 {
		if err := records.CheckHistoryPredecessor(result.Row, prior); err != nil {
			draft.mu.Unlock()
			return false, err
		}
		before, err := records.Predecessor(result.Row.Type, result.Row.ID, prior)
		if err != nil || before != result.Row.Predecessor || prior != nil && !reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created) || prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted record predecessor differs: %v", err)
		}
	} else {
		verb, _ := strings.CutPrefix(receipt.GetSubmission().GetSchema().GetName(), result.Row.Type+".")
		before := len(result.Row.History) - 1
		if verb == "create" && prior != nil || verb != "create" && prior == nil ||
			prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) ||
			prior == nil && before != 0 || prior != nil && (len(prior.RecordHistory()) != before ||
			before > 0 && !reflect.DeepEqual(prior.RecordHistory(), result.Row.History[:before]) ||
			!reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created)) {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted record is missing its predecessor")
		}
	}
	recovered := &records.Row{Value: value, Format: result.Row.Format, History: records.CopyHistory(result.Row.History)}
	if result.Version == 5 {
		if err := records.AppendRowHistory(recovered, prior, result.Row.History); err != nil {
			draft.mu.Unlock()
			return false, err
		}
	}
	recovered.RetainOriginal(result.Row.Value)
	et.setRow(rec.ID, recovered)
	draft.writes[result.Row.Type+"/"+rec.ID] = true
	draft.recentChanges.Remember(result.App+"/"+receipt.GetChangeId(), result.Row.Type+"/"+rec.ID)
	if et.knowledge {
		draft.dirty.Set(result.Row.Type+"/"+rec.ID, true)
	}
	draft.mu.Unlock()
	if result.Version == 5 {
		if err := validateUniqueReplacements(draft, result.Unique); err != nil {
			return false, err
		}
	}
	// An invalid receipt cannot expose the staged row. With the tenant lock
	// held, promote cannot race another supported write after this check.
	changed, err := l.ApplyAcceptedChange(receipt)
	if err != nil {
		return false, err
	}
	if err := s.promoteRecords(draft); err != nil {
		return false, err
	}
	if result.Row.Type == construction.AppType {
		t.defGen.Add(1)
	}
	t.committed.saveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
	t.committed.saveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
	if changed && publisher != nil {
		if err := publisher.ApplyAcceptedPublication(receipt.GetSubmission().GetSchema().GetName(), decoded); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func (d *stagedDecision) inputResult(app, name string, m platform.Member, body []byte, answer any, at time.Time) ([]byte, error) {
	if d.failure != nil || len(d.requests) > 0 {
		return nil, fmt.Errorf("connector input contains effects not represented by its result")
	}
	hash, err := resultformat.InputHash(d.tenant.ID, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := resultformat.Input{Version: 2, Kind: "input-result", Tenant: d.tenant.ID, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Deliveries: slices.Clone(d.deliveries), Notices: d.savedNotices(), Authorizations: slices.Clone(d.authorizations)}
	if message, ok := answer.(proto.Message); ok {
		result.Answer, err = protojson.Marshal(message)
	} else {
		result.Answer, err = json.Marshal(answer)
	}
	if err != nil {
		return nil, err
	}
	if len(d.events) > 0 {
		receipt, ok := answer.(*pb.ChangeRecord)
		if !ok || receipt.GetSubmission().GetAuthority() != app {
			return nil, fmt.Errorf("connector input has decisions without a matching answer")
		}
		result.Changes, err = d.batchResult(app, receipt.GetSubmission(), receipt, at)
		if err != nil {
			return nil, err
		}
		// The child batch owns the entire staged state; do not apply its marks,
		// notices or record images twice from the enclosing input.
		result.Deliveries, result.Notices = nil, nil
		result.Digest, err = resultformat.DigestInput(result)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if _, err := resultformat.DecodeInput(raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if d.control != nil || len(d.intents) > 0 || len(d.observations) > 0 || len(d.allocated) > 0 || len(d.operationCancels) > 0 {
		return nil, fmt.Errorf("connector input contains effects without a decision")
	}
	// A legacy input retry may read its saved receipt through a private log.
	// Permit that read only when every receipt still equals the live ledger;
	// a newly accepted decision without a published event cannot disappear.
	for ledger, staged := range d.logs {
		if !proto.Equal(ledger.LastAccepted(d.tenant.ID), staged.Last(d.tenant.ID)) {
			return nil, fmt.Errorf("connector input contains an unpublished decision")
		}
	}
	plan, err := d.savedChangePlan()
	if err != nil {
		return nil, err
	}
	result.States, result.Deliveries, result.Notices, result.Unique = plan.States, plan.Deliveries, plan.Notices, plan.Unique
	result.Rows, err = captureDirectRows(d, app, nil, nil, "connector input")
	if err != nil {
		return nil, err
	}
	result.Digest, err = resultformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := resultformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeAcceptedInputRefusal(tenant, app, name string, m platform.Member, body []byte, at time.Time, refusal *kernel.Error) ([]byte, error) {
	hash, err := resultformat.InputHash(tenant, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := resultformat.Input{Version: 2, Kind: "input-result", Tenant: tenant, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Refusal: refusal}
	result.Digest, err = resultformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := resultformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	if m.ID == "" || m.Tenant != t.ID || name == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connector input has an invalid identity")
	}
	hash, hashErr := resultformat.InputHash(t.ID, a.Manifest().ID, name, m.ID, body)
	if hashErr != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	key := "input:" + hash
	if saved := t.committed.input(a.Manifest().ID + "/" + key); len(saved) != 0 {
		return t.answerAcceptedInput(a, saved)
	}
	draft := t.newStagedDecision()
	entry := t.authorizeEntry(m, a.Manifest().ID, "input:"+name, "action", now)
	draft.authorizations = append(draft.authorizations, resultformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: "input:" + name}, Decision: entry, Roles: slices.Clone(m.RolesIn(a.Manifest().ID))})
	answer, refusal, unsupported := decideAcceptedInput(a, draft, m, name, body, now)
	if unsupported {
		return nil, refusal
	}
	var raw []byte
	var err error
	if refusal != nil {
		raw, err = encodeAcceptedInputRefusal(t.ID, a.Manifest().ID, name, m, body, now, refusal)
	} else {
		raw, err = draft.inputResult(a.Manifest().ID, name, m, body, answer, now)
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input cannot produce a bounded accepted result")
	}
	result, err := resultformat.DecodeInput(raw)
	if err == nil {
		_, err = t.prepareAcceptedChanges(result.ChangePlan())
	}
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	principal, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = draft.flowVersions()
	}
	committed, err := t.acceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: principal, Body: raw, At: now, Versions: versions}, key, hash)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return t.finishCommittedInput(a, m, name, key, hash, committed)
}

func (t *Tenant) finishCommittedInput(a platform.AcceptedInputApp, m platform.Member, name, key, hash string, committed []byte) (answer any, refusal *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed connector result: %v", failure))
			answer = nil
			refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	result, applied, err := t.applyAcceptedInput(committed)
	if err != nil || result.Key != key || result.RequestHash != hash || result.Member != m.ID ||
		result.Name != name || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("committed connector input differs from request: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	t.acceptedResultApplied()
	if applied && result.Refusal == nil {
		t.audit.remember(AuditEntry{At: result.At, Member: m.ID, App: result.App, Action: "input:" + name})
		t.enqueue(result.At)
		t.changedOwner(result.App)
	}
	return t.answerAcceptedInput(a, committed)
}

func decideAcceptedInput(a platform.AcceptedInputApp, draft *stagedDecision, m platform.Member, name string, body []byte, at time.Time) (answer any, refusal *kernel.Error, unsupported bool) {
	defer func() {
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			answer = nil
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input uses an effect outside the accepted-result boundary")
			unsupported = true
		}
	}()
	answer, refusal = inputApp(draft.stateApp(a), platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) answerAcceptedInput(a platform.AcceptedInputApp, raw []byte) (any, *kernel.Error) {
	result, err := resultformat.DecodeInput(raw)
	if err != nil || result.Tenant != t.ID || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("saved connector answer is incompatible: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if result.Refusal != nil {
		return nil, result.Refusal
	}
	answer, err := a.DecodeAcceptedInput(result.Answer)
	if err != nil {
		t.quarantine(fmt.Errorf("saved connector answer cannot be decoded: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return answer, nil
}

func (t *Tenant) applyAcceptedInput(raw []byte) (resultformat.Input, bool, error) {
	result, err := resultformat.DecodeInput(raw)
	if err != nil {
		return result, false, err
	}
	app, ok := t.app(result.App).(platform.AcceptedInputApp)
	if result.Tenant != t.ID || !ok || !slices.Contains(app.AcceptedInputs(), result.Name) {
		return result, false, fmt.Errorf("connector result belongs to another input")
	}
	if result.Refusal == nil {
		if _, err := app.DecodeAcceptedInput(result.Answer); err != nil {
			return result, false, fmt.Errorf("connector result has an invalid answer: %w", err)
		}
	}
	if prior := t.committed.input(result.App + "/" + result.Key); len(prior) > 0 {
		saved, err := resultformat.DecodeInput(prior)
		if err != nil || saved.Digest != result.Digest {
			return result, false, fmt.Errorf("connector input key belongs to another result")
		}
		return result, false, nil
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return result, false, err
	}
	if err := prepared.apply(t); err != nil {
		return result, false, err
	}
	prepared.publish(t)
	if result.Refusal != nil {
		t.refused(result.Member, result.Name, result.Refusal, result.At)
	}
	t.committed.saveInput(result.App+"/"+result.Key, raw)
	return result, true, nil
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
	host.Attach(fork, hostView{t: d.tenant, app: fork, decision: d})
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
		host.Attach(app, hostView{t: d.tenant, app: app, decision: d})
	}
}

// Version choices belong to the private Flow instance that actually decided.
// They are saved with the result before the live registry is changed.
func (d *stagedDecision) flowVersions() map[string]int {
	if private, ok := d.privateApps[platform.FlowApp].(host.Processes); ok {
		return private.Versions()
	}
	return nil
}

func (d *stagedDecision) savedStates() ([]resultformat.State, error) {
	var out []resultformat.State
	for _, id := range slices.Sorted(maps.Keys(d.states)) {
		owner := d.states[id]
		image, err := owner.AcceptedState()
		if err != nil {
			return nil, err
		}
		if err := owner.ValidateAcceptedState(image); err != nil {
			return nil, err
		}
		before, err := acceptance.Digest(d.stateBases[id])
		if err != nil {
			return nil, err
		}
		after, err := acceptance.Digest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			out = append(out, resultformat.State{App: id, Before: before, Image: image})
		}
	}
	return out, nil
}

func (t *Tenant) validateAcceptedStates(states []resultformat.State) error {
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
		hash, err := acceptance.Digest(prior)
		if err != nil || hash != state.Before {
			return fmt.Errorf("accepted state predecessor differs for %s", state.App)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedStates(states []resultformat.State) error {
	for _, state := range states {
		if err := t.app(state.App).(platform.AcceptedStateApp).ApplyAcceptedState(state.Image); err != nil {
			return err
		}
	}
	return nil
}

// acceptResult is the storage boundary for every new authoritative result.
// Callers stage first and apply only the returned bytes. Record is the legacy
// local recording callback; production journals use AcceptResult to report
// failures. The durable journal resolves ambiguous commits under the original
// identity; an unresolved commit stops only this tenant's generation.
func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.quarantined() {
		return nil, fmt.Errorf("tenant %s is stopped", t.ID)
	}
	if t.recordReadError != nil {
		return nil, t.recordReadError
	}
	if t.AcceptResult != nil {
		before := int64(0)
		if t.runtimePosition != nil {
			before = t.runtimePosition()
			t.committed.releasePending()
		}
		raw, err := t.AcceptResult(entry, key, hash)
		if err != nil && (errors.Is(err, journal.ErrCommitUnknown) ||
			t.runtimePosition != nil && t.runtimePosition() > before) {
			t.quarantine(fmt.Errorf("accepted result commit could not be resolved: %w", err))
		}
		if err == nil && t.runtimePosition != nil && t.runtimePosition() > before {
			delta, decodeErr := acceptedStorageDelta(raw)
			if decodeErr != nil {
				t.quarantine(fmt.Errorf("committed state delta: %w", decodeErr))
				return nil, decodeErr
			}
			t.committed.holdPending(delta)
		}
		return raw, err
	}
	if t.Record != nil {
		t.Record(entry)
	}
	return entry.Body, nil
}

func (t *Tenant) recordsAcceptedResults() bool { return t.AcceptResult != nil || t.Record != nil }

// acceptedResultApplied advances the record read view only after the complete
// top-level durable result has been applied. Nested batches and partial owner
// application must keep the prior position until their enclosing result ends.
// The tenant writer lock covers acceptance, application and this transition.
func (t *Tenant) acceptedResultApplied() {
	t.records.mu.Lock()
	if t.records.currentPosition != nil {
		t.records.appliedPosition = t.records.currentPosition()
	}
	if t.currentReadScopes {
		t.records.useCurrentRows()
	}
	t.records.mu.Unlock()
	if t.currentReadScopes {
		t.closeRecordRead()
		t.openRecordRead(context.Background())
	}
}
