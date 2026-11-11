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

func (t *Tenant) changeApply() runtimekernel.ChangeApply {
	return runtimekernel.ChangeApply{Batch: t.batchApply(),
		Check: func(plan acceptedformat.ChangePlan) error {
			if err := t.validateAcceptedControl(plan.Control); err != nil {
				return err
			}
			if err := t.notices.Check(plan.Notices); err != nil {
				return err
			}
			if err := t.validateAcceptedIntents(plan.Intents); err != nil {
				return err
			}
			if err := runtimekernel.CheckStates(t.states, plan.States); err != nil {
				return err
			}
			return t.sequences.Check(plan.SequenceBases, plan.Sequences)
		},
		Apply: func(plan acceptedformat.ChangePlan) error {
			if err := t.applyAcceptedControl(plan.Control); err != nil {
				return err
			}
			if err := runtimekernel.ApplyStates(t.states, plan.States); err != nil {
				return err
			}
			t.applyAcceptedNotices(plan.Notices)
			t.installAcceptedIntents(plan.Intents)
			t.sequences.Set(plan.Sequences)
			return nil
		}}
}

func (t *Tenant) prepareAcceptedChanges(plan acceptedformat.ChangePlan) (*runtimekernel.PreparedChanges, error) {
	return t.PrepareChanges(plan, t.changeApply())
}

func (t *Tenant) applyAcceptedChanges(p *runtimekernel.PreparedChanges) error {
	return t.PromoteChanges(p, t.changeApply())
}

func (t *Tenant) publishAcceptedChanges(p *runtimekernel.PreparedChanges) {
	if p.Child != nil {
		t.publishAcceptedBatch(p.Child.Result)
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
