package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// accepted_store.go consolidated into this owner

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

// accepted_rows.go consolidated into this owner

// Capture the same full record image and predecessor for connector inputs,
// effect answers and callback observations, in the supplied stable order.
func captureDirectRows(d *stagedDecision, owner string, refs []string, exclude map[string]bool, source string) ([]resultformat.InputRow, error) {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	if refs == nil {
		refs = slices.Sorted(maps.Keys(d.records.writes))
	}
	var rows []resultformat.InputRow
	for _, ref := range refs {
		if exclude[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.info.App != owner || et.rows.Get(id) == nil {
			return nil, fmt.Errorf("%s writes a foreign or undeclared record", source)
		}
		var prior *records.Row
		if old := d.records.parent.types[typ]; old != nil {
			prior = old.rows.Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.rows.Get(id), prior)
		if err != nil {
			return nil, err
		}
		row := resultformat.InputRow{Image: image, App: owner}
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
func (t *Tenant) prepareAcceptedDirectRows(rows []resultformat.InputRow, member string, at time.Time) (*recordStore, error) {
	return t.prepareAcceptedDirectRowsOn(t.records, rows, member, at)
}

func (t *Tenant) prepareAcceptedDirectRowsOn(base *recordStore, rows []resultformat.InputRow, member string, at time.Time) (*recordStore, error) {
	draft := base.forkRecords()
	draft.mu.Lock()
	defer draft.mu.Unlock()
	for _, image := range rows {
		et := draft.types[image.Type]
		if et == nil || et.info.App != image.App {
			return nil, fmt.Errorf("direct result needs missing record type %s", image.Type)
		}
		prior := et.rows.Get(image.ID)
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
				before, err = acceptance.Digest(row)
				if err != nil {
					return nil, err
				}
				previousHistory, revision = len(prior.RecordHistory()), records.Header(prior.Value).Revision
			}
			if image.Before != before || len(image.History) <= previousHistory {
				return nil, fmt.Errorf("direct record predecessor differs")
			}
			if prior != nil {
				old, err := acceptance.Digest(image.History[:previousHistory])
				actual, actualErr := acceptance.Digest(prior.RecordHistory())
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
		value := reflect.New(et.info.Go).Elem()
		decode, err := records.DecodeFormattedImage(et.info, image.Value, image.Projection, draft.readRelease, image.Format)
		if err != nil {
			return nil, err
		}
		if err := acceptance.DecodeJSON(decode, value.Addr().Interface(), "direct record"); err != nil {
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
		et.setRow(image.ID, recovered)
		draft.writes[image.Type+"/"+image.ID] = true
		if et.knowledge {
			draft.dirty.Set(image.Type+"/"+image.ID, true)
		}
	}
	return draft, nil
}

// accepted_callback_rows.go consolidated into this owner

// Callback observations have no new kernel change receipt. Their authority is
// the host-delivered, already accepted effect and the exact owner callback,
// with the same direct-row preimage/history checks as connector/effect results.
type callbackWrite struct {
	Source  platform.Effect
	History int
}

func (d *stagedDecision) savedCallbackRows(published map[string]bool) ([]resultformat.CallbackRow, error) {
	var rows []resultformat.CallbackRow
	for _, ref := range slices.Sorted(maps.Keys(d.records.writes)) {
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
		rows = append(rows, resultformat.CallbackRow{Row: direct[0], Source: callback.Source})
	}
	return rows, nil
}

// Validate sources and direct rows in private views before any ledger or
// business/control state in the containing result is promoted.
func (t *Tenant) prepareCallbackRows(target *recordStore, rows []resultformat.CallbackRow, at time.Time) error {
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
		actual, err := acceptance.Digest(current)
		expected, expectedErr := acceptance.Digest(source)
		if current.ID == "" || err != nil || expectedErr != nil || actual != expected {
			return fmt.Errorf("callback row source effect predecessor differs")
		}
		private, err := t.prepareAcceptedDirectRows([]resultformat.InputRow{row}, "app:"+row.App, at)
		if err != nil {
			return err
		}
		typ, id, _ := strings.Cut(ref, "/")
		private.mu.Lock()
		image := private.types[typ].rows.Get(id)
		private.mu.Unlock()
		target.mu.Lock()
		target.types[typ].setRow(id, image)
		target.writes[ref] = true
		if target.types[typ].knowledge {
			target.dirty.Set(ref, true)
		}
		target.mu.Unlock()
	}
	return nil
}

// accepted_unique.go consolidated into this owner

func uniqueReplacements(changes map[string]map[string]string) []journal.StateUnique {
	out := make([]journal.StateUnique, 0, len(changes))
	for _, ref := range slices.Sorted(maps.Keys(changes)) {
		typ, id, _ := strings.Cut(ref, "/")
		out = append(out, journal.StateUnique{Type: typ, ID: id, Keys: changes[ref]})
	}
	return out
}

func (d *stagedDecision) savedUniqueChanges() []journal.StateUnique {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	return uniqueReplacements(d.records.uniqueChanges())
}

func validateUniqueReplacements(store *recordStore, saved []journal.StateUnique) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	expected := store.uniqueChanges()
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

// accepted_usage.go consolidated into this owner

// A direct model call has no owned effect. Its immutable usage observation is
// accepted before changing the meter, using the same predecessor as effects.

func (t *Tenant) usagePredecessor() (string, error) {
	if t.ai == nil {
		return "", fmt.Errorf("usage needs an AI meter")
	}
	raw, err := t.ai.Snapshot()
	if err != nil {
		return "", err
	}
	return acceptance.Digest(raw)
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *kernel.Error {
	// Usage observes a completed call. Its principal is the meter owner; a
	// later revocation of the attributed caller cannot erase an incurred cost.
	c := platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + platform.AIApp, Tenant: m.Tenant}, platform.AIApp, false, true)
	decision := t.authorizeNative(c, "usage:observe", "derive", nil, m.ID != "" && m.ID == u.Member && resultformat.ValidUsage(u))
	if refusal := authorizationRefusal(decision); refusal != nil {
		return refusal
	}
	before, err := t.usagePredecessor()
	if err != nil || m.Tenant != t.ID || m.ID != u.Member || !resultformat.ValidUsage(u) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	r := resultformat.Usage{Version: 1, Kind: "usage-result", Tenant: t.ID, App: platform.AIApp, Member: m.ID, At: u.At.UTC(), Before: before, Usage: u}
	r.Authorizations = []resultformat.AuthorizationEvidence{{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Action: "usage:observe"}, Decision: decision, Roles: slices.Clone(c.RolesHere())}}
	r.RequestHash, err = resultformat.UsageHash(before, u)
	if err != nil {
		return unknown()
	}
	r.Key = "usage:" + r.RequestHash
	r.Digest, err = resultformat.DigestUsage(r)
	if err != nil {
		return unknown()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return unknown()
	}
	if _, err := resultformat.DecodeUsage(raw); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	principal, _ := json.Marshal(m)
	committed, err := t.acceptResult(Entry{App: platform.AIApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	saved, err := resultformat.DecodeUsage(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed model usage differs: %v", err))
		return unknown()
	}
	if _, err := t.applyAcceptedUsage(committed); err != nil {
		t.quarantine(err)
		return unknown()
	}
	t.acceptedResultApplied()
	t.operationsChanged(platform.AIApp)
	return nil
}

func (t *Tenant) applyAcceptedUsage(raw []byte) (resultformat.Usage, error) {
	r, err := resultformat.DecodeUsage(raw)
	if err != nil {
		return r, err
	}
	before, err := t.usagePredecessor()
	if err != nil || r.Tenant != t.ID || before != r.Before {
		return r, fmt.Errorf("model usage predecessor differs: %v", err)
	}
	t.ai.Meter(r.Usage)
	return r, nil
}
