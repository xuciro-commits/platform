package host

import (
	"encoding/json"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
	"time"
)

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

func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	return runtimekernel.ApplyDirectRows(t.Projection, rows, member, at, checkRunApplicationBindings)
}

func (d *stagedDecision) stateApp(app platform.App) platform.App {
	id := app.Manifest().ID
	if existing := d.PrivateApps[id]; existing != nil {
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
	if d.PrivateApps == nil {
		d.PrivateApps = map[string]platform.App{}
	}
	d.PrivateApps[id] = fork
	(hostView{t: d.tenant, app: fork, decision: d}).bindOwner(fork)
	if ownsState {
		copy, ok := fork.(platform.AcceptedStateApp)
		if !ok {
			unsupportedStagedEffect()
		}
		if d.States == nil {
			d.States = map[string]platform.AcceptedStateApp{}
			d.StateBases = map[string]json.RawMessage{}
		}
		d.States[id], d.StateBases[id] = copy, slices.Clone(before)
	}
	return fork
}

func (d *stagedDecision) attachPrivateHosts() {
	for _, app := range d.PrivateApps {
		(hostView{t: d.tenant, app: app, decision: d}).bindOwner(app)
	}
}
