package host

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

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
		base := t.Projection
		if prepared.child != nil {
			base = prepared.child.records
		}
		prepared.records, err = t.prepareAcceptedDirectRowsOn(base, plan.Rows, plan.Member, plan.At)
		if err != nil {
			return nil, err
		}
		if prepared.child != nil {
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
			records = t.Projection.ForkRecords()
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
		if err := t.Projection.PromoteRecords(p.records); err != nil {
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

func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	return t.prepareAcceptedDirectRowsOn(t.Projection, rows, member, at)
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
	}
	if err == nil {
		fork, err = app.Fork(app.AcceptedLedger())
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
		owner := t.states[state.App]
		if owner == nil || seen[state.App] || state.Before == "" || len(state.Image) == 0 {
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
		if err := t.states[state.App].ApplyAcceptedState(state.Image); err != nil {
			return err
		}
	}
	return nil
}

func uniqueReplacements(changes map[string]map[string]string) []persistence.StateUnique {
	out := make([]persistence.StateUnique, 0, len(changes))
	for _, ref := range slices.Sorted(maps.Keys(changes)) {
		typ, id, _ := strings.Cut(ref, "/")
		out = append(out, persistence.StateUnique{Type: typ, ID: id, Keys: changes[ref]})
	}
	return out
}

func (d *stagedDecision) savedUniqueChanges() []persistence.StateUnique {
	d.records.Lock()
	defer d.records.Unlock()
	return uniqueReplacements(d.records.UniqueChanges())
}

func validateUniqueReplacements(store *records.Store, saved []persistence.StateUnique) error {
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
