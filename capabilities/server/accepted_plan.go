package platformserver

import (
	"fmt"
	"maps"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
)

func (d *stagedDecision) savedChangePlan() (resultformat.ChangePlan, error) {
	plan := resultformat.ChangePlan{Notices: d.savedNotices(), Intents: slices.Clone(d.intents),
		Deliveries: slices.Clone(d.deliveries), Sequences: maps.Clone(d.allocated),
		SequenceBases: maps.Clone(d.sequenceBases), Unique: d.savedUniqueChanges()}
	var err error
	plan.States, err = d.savedStates()
	if err == nil {
		plan.Control, err = d.savedControl()
	}
	return plan, err
}

func (r acceptedWork) ChangePlan() resultformat.ChangePlan {
	return resultformat.ChangePlan{At: r.At, Changes: r.Changes, States: r.States,
		Control: r.Control, Intents: r.Intents, Notices: r.Notices, Unique: r.Unique, CheckUnique: r.Version >= 3}
}

// This preparation changes no live owner. In particular a valid child cannot
// advance its ledger before the outer result's remaining owners are checked.
type preparedAcceptedChanges struct {
	plan    resultformat.ChangePlan
	records *recordStore
	child   *preparedAcceptedBatch
}

func (t *Tenant) prepareAcceptedChanges(plan resultformat.ChangePlan) (*preparedAcceptedChanges, error) {
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
	if err := t.sequences.check(plan.SequenceBases, plan.Sequences); err != nil {
		return nil, err
	}
	prepared := &preparedAcceptedChanges{plan: plan}
	var err error
	if len(plan.Changes) > 0 {
		batch, _, err := resultformat.DecodeBatch(plan.Changes)
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
			prepared.records.parent = base.parent
			prepared.records.baseGeneration = base.baseGeneration
			maps.Copy(prepared.records.writes, base.writes)
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
			records = t.records.forkRecords()
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
		if err := t.records.promoteRecords(p.records); err != nil {
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
	t.sequences.set(p.plan.Sequences)
	return nil
}

func (p *preparedAcceptedChanges) publish(t *Tenant) {
	if p.child != nil {
		t.publishAcceptedBatch(p.child.result)
	}
}
