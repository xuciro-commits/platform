package host

import (
	"encoding/json"
	"fmt"
	"maps"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (d *stagedDecision) savedChangePlan() (acceptedformat.ChangePlan, error) {
	plan := acceptedformat.ChangePlan{Notices: d.notices.Saved(), Intents: slices.Clone(d.intents),
		Sequences:     maps.Clone(d.allocated),
		SequenceBases: maps.Clone(d.sequenceBases), Unique: d.savedUniqueChanges()}
	var err error
	plan.States, err = runtimekernel.SavedStates(d.states, d.stateBases)
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
	if err := t.notices.Check(plan.Notices); err != nil {
		return nil, err
	}
	if err := t.validateAcceptedIntents(plan.Intents); err != nil {
		return nil, err
	}
	if err := runtimekernel.CheckStates(t.states, plan.States); err != nil {
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
		if err := runtimekernel.CheckDirectAndChild(plan.Rows, batch); err != nil {
			return nil, err
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
		prepared.records, err = runtimekernel.ApplyDirectRows(base, plan.Rows, plan.Member, plan.At, checkRunApplicationBindings)
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
		if err := runtimekernel.ValidateUniqueReplacements(records, plan.Unique); err != nil {
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
	if err := runtimekernel.ApplyStates(t.states, p.plan.States); err != nil {
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
	return runtimekernel.ApplyDirectRows(t.Projection, rows, member, at, checkRunApplicationBindings)
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

func (d *stagedDecision) savedUniqueChanges() []persistence.StateUnique {
	d.records.Lock()
	defer d.records.Unlock()
	return runtimekernel.UniqueReplacements(d.records.UniqueChanges())
}
