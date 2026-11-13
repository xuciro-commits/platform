package actions

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type TxnCalls struct {
	Caller   platform.Caller
	Snapshot *records.Store
	Current  *records.Store
	Check    func(platform.TxnCall) error
	Read     func(store *records.Store, c platform.Caller, call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error)
}

func (q *TxnCalls) Call(call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	if err := q.Check(call); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	store := q.Snapshot
	if call.View == "current" {
		store = q.Current
	}
	return q.Read(store, q.Caller, call, inputs, at)
}
