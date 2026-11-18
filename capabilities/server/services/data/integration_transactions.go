package data

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (o *Owner) CheckTransactionWriteback(step platform.TxnStep, object string) error {
	record, found := o.integration.Record(WritebackType + "/" + step.Effect.Writeback)
	w, valid := record.(Writeback)
	if !found || !valid {
		return fmt.Errorf("Transaction writeback is unavailable")
	}
	w, err := o.ExecutionWriteback(w)
	if err != nil {
		return err
	}
	if w.State != "published" || w.On != "transaction" || w.Object != object || len(w.Result) > 0 {
		return fmt.Errorf("Effect steps require a published transaction-only writeback without answer mutations")
	}
	return nil
}

func (o *Owner) TransactionEffect(c platform.Caller, id string) (protocol.TransactionEffectDefinition, *platform.Error) {
	w, found := platform.Get[Writeback](c, id)
	if !found {
		return protocol.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction writeback is unavailable")
	}
	w, err := o.ExecutionWriteback(w)
	if err != nil {
		return protocol.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	conn, found := platform.Get[Connection](c, w.Connection)
	if !found {
		return protocol.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction effect connection is unavailable")
	}
	conn, err = o.ExecutionConnection(conn)
	if err != nil {
		return protocol.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	return protocol.TransactionEffectDefinition{Request: func(id string, payload json.RawMessage) (json.RawMessage, *platform.Error) {
		request, err := o.PrepareWritebackRequest(w, conn, id, payload)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		return platform.Raw(request), nil
	}}, nil
}
