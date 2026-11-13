package host

import records "platformserver/kernel/records"

import (
	"encoding/json"
	dataowner "platformserver/services/data"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/services/actions"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (d *stagedDecision) TxnTransport() platform.TxnTransport {
	return hostView{t: d.tenant, app: d.tenant.App(build.ID), decision: d}
}

func (h hostView) TakeTxnStep() *kernel.Error {
	if h.decision != nil && h.decision.Failure != nil {
		return h.decision.Failure
	}
	if h.decision == nil || h.decision.TransactionSteps >= platform.TxnMaxExecutionSteps {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction execution exceeds its step budget")
	}
	h.decision.TransactionSteps++
	return nil
}

type txnCalls struct {
	decision *stagedDecision
	caller   platform.Caller
	snapshot *records.Store
}

func (h hostView) BeginTxnCalls(c platform.Caller, at time.Time) (platform.TxnCalls, *kernel.Error) {
	if h.decision == nil || !c.Staging() || c.App != build.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction calls require their original private owner")
	}
	return &txnCalls{decision: h.decision, caller: c, snapshot: h.decision.Records.ForkRecords()}, nil
}

func (t *Tenant) submitAutomated(app platform.App, caller platform.Caller, sub *pb.Submission, now time.Time) *kernel.Error {
	sub.PrincipalId = caller.ID
	_, err := t.accept(app, caller.Member, sub, now, acceptOptions{automation: true})
	return err
}

func (h hostView) CheckTxnCall(call platform.TxnCall) error {
	return actions.CheckTxnCall(call, h.t.definitions, h.t.App)
}

func (q *txnCalls) Call(call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	view := hostView{t: q.decision.tenant}
	if err := view.CheckTxnCall(call); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	store := q.snapshot
	if call.View == "current" {
		store = q.decision.Records
	}
	return q.decision.tenant.readQueryVersionFrom(store, q.caller, call.Ref.App, call.Ref.Name, call.Version, inputs, at)
}

func (h hostView) QueueTxnEffect(c platform.Caller, receipt *pb.ChangeRecord, step string, target platform.TxnRef, raw json.RawMessage, at time.Time) *kernel.Error {
	effect, refusal := dataowner.TxnWritebackEffect(c, receipt, step, target, raw, at, h.decision != nil, h.t.checkWritebackCredential)
	if refusal != nil {
		return refusal
	}
	typ := receipt.GetSubmission().GetTarget().GetType()
	applications, err := h.t.bindApplications(platform.AssetRef{App: c.App, Kind: platform.AssetObject, Name: typ}, h.t.releases.Active(), "")
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	effect.Applications = applications
	if slices.ContainsFunc(h.decision.Intents, func(existing platform.Effect) bool { return existing.ID == effect.ID }) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A transaction step declared the same outbound intent twice")
	}
	if !strings.HasPrefix(effect.Target, c.App+".") {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction effects cannot target another authority")
	}
	h.decision.Intents = append(h.decision.Intents, effect)
	return nil
}

var _ platform.TransactionHost = hostView{}

func (h hostView) TransactionRuntime(context platform.BusinessRuntimeContext) platform.TransactionExecutor {
	return h.t.services.NewTransactionExecutor(context)
}
