package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) CancelOperation(c platform.Caller, r *pb.ChangeRecord, id string) *kernel.Error {
	if r == nil || c.Tenant != d.tenant.ID || r.GetSubmission().GetAuthority() != c.App {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Cancellation needs its accepted owner decision")
	}
	d.tenant.opsMu.Lock()
	i := slices.IndexFunc(d.tenant.outbound, func(x *effect) bool { return x.ID == id && x.Endpoint == resultformat.OperationEndpoint })
	if i < 0 {
		d.tenant.opsMu.Unlock()
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation call was not found")
	}
	x := d.tenant.outbound[i].Effect
	d.tenant.opsMu.Unlock()
	if settled(x.State) {
		return nil
	}
	var b operationBinding
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
func (t *Tenant) operationCancellation(id string) (resultformat.OperationCancellation, error) {
	t.opsMu.Lock()
	i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == id && x.Endpoint == resultformat.OperationEndpoint })
	if i < 0 {
		t.opsMu.Unlock()
		return resultformat.OperationCancellation{}, fmt.Errorf("operation cancellation has no intent")
	}
	x := t.outbound[i].Effect
	t.opsMu.Unlock()
	after := x
	after.State, after.Error = "discarded", "Cancelled"
	after.Output = nil
	r := resultformat.OperationCancellation{Before: x, After: after}
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
func (d *stagedDecision) savedOperationCancellations() ([]resultformat.OperationCancellation, error) {
	ids := []string{}
	for id := range d.operationCancels {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := []resultformat.OperationCancellation{}
	for _, id := range ids {
		r, err := d.tenant.operationCancellation(id)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
func (t *Tenant) validateOperationCancellations(saved []resultformat.OperationCancellation) error {
	seen := map[string]bool{}
	for _, r := range saved {
		if seen[r.Before.ID] {
			return fmt.Errorf("duplicate operation cancellation")
		}
		seen[r.Before.ID] = true
		if r.Before.Endpoint != resultformat.OperationEndpoint || settled(r.Before.State) || !strings.HasPrefix(r.Before.ID, t.ID+":") {
			return fmt.Errorf("invalid operation cancellation")
		}
		expected, err := t.operationCancellation(r.Before.ID)
		if err != nil {
			return err
		}
		actualHash, _ := acceptance.Digest(r)
		expectedHash, _ := acceptance.Digest(expected)
		if actualHash != expectedHash {
			return fmt.Errorf("operation cancellation predecessor differs")
		}
	}
	return nil
}
func (t *Tenant) applyOperationCancellations(saved []resultformat.OperationCancellation) error {
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
		t.opsMu.Lock()
		for _, x := range t.outbound {
			if x.ID == r.Before.ID {
				x.Effect, x.sending = r.After, false
			}
		}
		t.opsMu.Unlock()
		t.cancels.cancel(r.Before.ID)
	}
	return nil
}
func (h hostView) Operation(app, name string, version int) (platform.Operation, int, bool) {
	owner := h.t.app(app)
	if owner == nil {
		return platform.Operation{}, 0, false
	}
	return operationDefinition(owner, name, version)
}
