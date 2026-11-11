package kernel

import (
	"fmt"

	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
)

type ChangeApply struct {
	Batch BatchApply
	Check func(acceptedformat.ChangePlan) error
	Apply func(acceptedformat.ChangePlan) error
}

type PreparedChanges struct {
	plan    acceptedformat.ChangePlan
	records *records.Store
	Child   *PreparedBatch
}

func (s *Sequence) PrepareChanges(plan acceptedformat.ChangePlan, h ChangeApply) (*PreparedChanges, error) {
	if err := h.Check(plan); err != nil {
		return nil, err
	}
	prepared := &PreparedChanges{plan: plan}
	var err error
	if len(plan.Changes) > 0 {
		batch, _, err := acceptedformat.DecodeBatch(plan.Changes)
		if err != nil || batch.Tenant != s.ID || !batch.At.Equal(plan.At) {
			return nil, fmt.Errorf("accepted result has an invalid child decision: %v", err)
		}
		owner := h.Batch.App(batch.App)
		if owner == nil {
			return nil, fmt.Errorf("accepted result needs missing child authority")
		}
		if err := CheckDirectAndChild(plan.Rows, batch); err != nil {
			return nil, err
		}
		prepared.Child, err = s.PrepareBatch(owner.AcceptedLedger(), plan.Changes, h.Batch)
		if err != nil || prepared.Child == nil {
			return nil, fmt.Errorf("accepted child result: %v (already applied=%t)", err, prepared.Child == nil)
		}
	}
	if len(plan.Rows) > 0 {
		base := s.Projection
		if prepared.Child != nil {
			base = prepared.Child.Records
		}
		prepared.records, err = ApplyDirectRows(base, plan.Rows, plan.Member, plan.At, h.Batch.CheckBindings)
		if err != nil {
			return nil, err
		}
		if prepared.Child != nil {
			prepared.records.ExtendDecision(base)
			prepared.Child.Records = prepared.records
			prepared.records = nil
		}
	}
	if plan.CheckUnique {
		store := prepared.records
		if prepared.Child != nil {
			store = prepared.Child.Records
		}
		if store == nil {
			store = s.Projection.ForkRecords()
		}
		if err := ValidateUniqueReplacements(store, plan.Unique); err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

func (s *Sequence) PromoteChanges(p *PreparedChanges, h ChangeApply) error {
	if p.Child != nil {
		if applied, err := s.PromoteBatch(p.Child, h.Batch); err != nil || !applied {
			return fmt.Errorf("accepted child application: %v (applied=%t)", err, applied)
		}
	}
	if p.records != nil {
		if err := s.Projection.PromoteRecords(p.records); err != nil {
			return err
		}
	}
	return h.Apply(p.plan)
}
