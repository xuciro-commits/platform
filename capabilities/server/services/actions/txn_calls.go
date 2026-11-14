package actions

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type TxnCalls struct {
	Caller   platform.Caller
	Snapshot *records.Store
	Current  *records.Store
	Check    func(platform.TxnCall) error
	Read     func(store *records.Store, c platform.Caller, call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *platform.Error)
}

func (q *TxnCalls) Call(call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *platform.Error) {
	if err := q.Check(call); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	store := q.Snapshot
	if call.View == "current" {
		store = q.Current
	}
	return q.Read(store, q.Caller, call, inputs, at)
}

func CaptureTxnIteration(tenant string, c platform.Caller, receipt *pb.ChangeRecord, captured []platform.TxnIteration, iteration platform.TxnIteration, readable func(ref string) bool) (platform.TxnIteration, *platform.Error) {
	fail := func(message string) (platform.TxnIteration, *platform.Error) {
		return iteration, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message)
	}
	if !c.Staging() || c.Replaying || receipt.GetChangeId() == "" || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || receipt.GetSubmission().GetTenantId() != tenant {
		return fail("Iteration capture is outside its private authority")
	}
	iteration.Decision = c.App + "/" + receipt.GetChangeId()
	if err := acceptedformat.ValidateTxnIterations("", append(slices.Clone(captured), iteration)); err != nil {
		return fail(err.Error())
	}
	for _, id := range iteration.IDs {
		if !readable(iteration.Object + "/" + id) {
			return fail("Iteration member is unavailable")
		}
	}
	iteration.IDs = slices.Clone(iteration.IDs)
	return iteration, nil
}
