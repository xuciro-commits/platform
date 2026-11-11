package host

import (
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/functions/runtime"

	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (d *stagedDecision) CancelOperation(c platform.Caller, r *pb.ChangeRecord, id string) *kernel.Error {
	if r == nil || c.Tenant != d.tenant.ID || r.GetSubmission().GetAuthority() != c.App {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Cancellation needs its accepted owner decision")
	}
	queued, ok := d.tenant.outbound.Get(id)
	if !ok || queued.Endpoint != acceptedformat.OperationEndpoint {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation call was not found")
	}
	x := queued.Effect
	if platform.EffectSettled(x.State) {
		return nil
	}
	var b compute.OperationBinding
	_ = json.Unmarshal([]byte(x.Body), &b)
	if c.Automation {
		target := r.GetSubmission().GetTarget()
		if c.App != "flow" || x.Target != target.GetType()+"/"+target.GetId() {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Flow can cancel only its own operation")
		}
	} else if c.ID != b.Member {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation belongs to another member")
	}
	if d.operationCancels == nil {
		d.operationCancels = map[string]bool{}
	}
	d.operationCancels[id] = true
	return nil
}
func (t *Tenant) operationCancellation(id string) (acceptedformat.OperationCancellation, error) {
	queued, ok := t.outbound.Get(id)
	if !ok || queued.Endpoint != acceptedformat.OperationEndpoint {
		return acceptedformat.OperationCancellation{}, fmt.Errorf("operation cancellation has no intent")
	}
	x := queued.Effect
	after := x
	after.State, after.Error = "discarded", "Cancelled"
	after.Output = nil
	r := acceptedformat.OperationCancellation{Before: x, After: after}
	works := t.forkWorkState()
	if work, err := works.Get(id); err == nil {
		r.PriorWork, _ = protojson.Marshal(work)
		if work.GetState() == pb.WorkState_WORK_STATE_RUNNING {
			if err := works.Cancel(id); err != nil {
				return r, err
			}
		}
		work, _ = works.Get(id)
		r.Work, _ = protojson.Marshal(work)
	}
	return r, nil
}
func (d *stagedDecision) savedOperationCancellations() ([]acceptedformat.OperationCancellation, error) {
	ids := []string{}
	for id := range d.operationCancels {
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
			work := &pb.Work{}
			if err := protojson.Unmarshal(r.Work, work); err != nil {
				return err
			}
			all := t.works.All()
			for i, prior := range all {
				if prior.GetWorkId() == work.GetWorkId() {
					all[i] = work
				}
			}
			t.works.Restore(all)
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
