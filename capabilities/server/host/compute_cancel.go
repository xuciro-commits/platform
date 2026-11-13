package host

import (
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/functions/runtime"

	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (d *stagedDecision) CancelOperation(c platform.Caller, r *pb.ChangeRecord, id string) *kernel.Error {
	record, refusal := compute.CheckCancel(d.tenant.ID, c, r, id, d.tenant.operationEffect)
	if refusal != nil || !record {
		return refusal
	}
	if d.OperationCancels == nil {
		d.OperationCancels = map[string]bool{}
	}
	d.OperationCancels[id] = true
	return nil
}
func (t *Tenant) operationCancellation(id string) (acceptedformat.OperationCancellation, error) {
	x, ok := t.operationEffect(id)
	if !ok || x.Endpoint != acceptedformat.OperationEndpoint {
		return acceptedformat.OperationCancellation{}, fmt.Errorf("operation cancellation has no intent")
	}
	after := x
	after.State, after.Error = "discarded", "Cancelled"
	after.Output = nil
	r := acceptedformat.OperationCancellation{Before: x, After: after}
	var err error
	r.PriorWork, r.Work, err = compute.CancelWork(t.forkWorkState(), id)
	return r, err
}
func (d *stagedDecision) savedOperationCancellations() ([]acceptedformat.OperationCancellation, error) {
	ids := []string{}
	for id := range d.OperationCancels {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := []acceptedformat.OperationCancellation{}
	for _, id := range ids {
		r, err := d.tenant.operationCancellation(id)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
func (t *Tenant) validateOperationCancellations(saved []acceptedformat.OperationCancellation) error {
	seen := map[string]bool{}
	for _, r := range saved {
		if seen[r.Before.ID] {
			return fmt.Errorf("duplicate operation cancellation")
		}
		seen[r.Before.ID] = true
		if r.Before.Endpoint != acceptedformat.OperationEndpoint || platform.EffectSettled(r.Before.State) || !strings.HasPrefix(r.Before.ID, t.ID+":") {
			return fmt.Errorf("invalid operation cancellation")
		}
		expected, err := t.operationCancellation(r.Before.ID)
		if err != nil {
			return err
		}
		actualHash, _ := platform.CanonicalDigest(r)
		expectedHash, _ := platform.CanonicalDigest(expected)
		if actualHash != expectedHash {
			return fmt.Errorf("operation cancellation predecessor differs")
		}
	}
	return nil
}
func (t *Tenant) applyOperationCancellations(saved []acceptedformat.OperationCancellation) error {
	for _, r := range saved {
		if len(r.Work) > 0 {
			if err := compute.RestoreWork(t.works, r.Work); err != nil {
				return err
			}
		}
		t.outbound.Update(r.Before.ID, func(x *actions.Queued) { x.Effect, x.Sending = r.After, false })
		t.cancels.Cancel(r.Before.ID)
	}
	return nil
}
func (h hostView) Operation(app, name string, version int) (platform.Operation, int, bool) {
	owner := h.t.App(app)
	if owner == nil {
		return platform.Operation{}, 0, false
	}
	return compute.OperationDefinition(owner, name, version)
}
