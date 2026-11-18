package host

import records "platformserver/kernel/records"

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/services/actions"
	build "platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (d *stagedDecision) TxnTransport() platform.TxnTransport {
	return hostView{t: d.tenant, app: d.tenant.App(build.ID), decision: d}
}

func (h hostView) TakeTxnStep() *platform.Error {
	var d *runtimekernel.Decision
	if h.decision != nil {
		d = &h.decision.Decision
	}
	return d.TakeTxnStep()
}

func (h hostView) BeginTxnCalls(c platform.Caller, at time.Time) (platform.TxnCalls, *platform.Error) {
	if h.decision == nil || !c.Staging() || c.App != build.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction calls require their original private owner")
	}
	return &actions.TxnCalls{Caller: c, Snapshot: h.decision.Records.ForkRecords(), Current: h.decision.Records, Check: hostView{t: h.decision.tenant}.CheckTxnCall,
		Read: func(store *records.Store, c platform.Caller, call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *platform.Error) {
			return h.decision.tenant.queryReads(store).QueryVersion(c, call.Ref.App, call.Ref.Name, call.Version, inputs, at)
		}}, nil
}

func (t *Tenant) submitAutomated(app platform.Service, caller platform.Caller, sub *pb.Submission, now time.Time) *platform.Error {
	sub.PrincipalId = caller.ID
	_, err := t.accept(app, caller.Member, sub, now, acceptOptions{automation: true})
	return err
}

func (h hostView) CheckTxnCall(call platform.TxnCall) error {
	return actions.CheckTxnCall(call, h.t.definitions, h.t.App, PublicationResolver())
}

func (h hostView) QueueTxnEffect(c platform.Caller, receipt *pb.ChangeRecord, step string, target platform.TxnRef, raw json.RawMessage, at time.Time) *platform.Error {
	effect, refusal := dataowner.TxnWritebackEffect(c, receipt, step, target, raw, at, h.decision != nil, h.t.checkWritebackCredential,
		func(typ string) ([]platform.AssetBinding, error) {
			return h.t.bindApplications(platform.AssetRef{App: c.App, Kind: platform.AssetObject, Name: typ}, h.t.releases.Active(), "")
		}, func() []platform.Effect { return h.decision.Intents })
	if refusal != nil {
		return refusal
	}
	h.decision.Intents = append(h.decision.Intents, effect)
	return nil
}

var _ platform.TransactionHost = hostView{}

func (h hostView) TransactionRuntime(context protocol.BusinessRuntimeContext) protocol.TransactionExecutor {
	return h.t.services.NewTransactionExecutor(context)
}
