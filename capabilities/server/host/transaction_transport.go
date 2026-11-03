package host

import records "platformserver/kernel/records"

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (d *stagedDecision) TxnTransport() platform.TxnTransport {
	return hostView{t: d.tenant, app: d.tenant.app(build.ID), decision: d}
}

func (h hostView) TakeTxnStep() *kernel.Error {
	if h.decision != nil && h.decision.failure != nil {
		return h.decision.failure
	}
	if h.decision == nil || h.decision.transactionSteps >= platform.TxnMaxExecutionSteps {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction execution exceeds its step budget")
	}
	h.decision.transactionSteps++
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
	return &txnCalls{decision: h.decision, caller: c, snapshot: h.decision.records.ForkRecords()}, nil
}

func (t *Tenant) submitAutomated(app platform.App, caller platform.Caller, sub *pb.Submission, now time.Time) *kernel.Error {
	sub.PrincipalId = caller.ID
	_, err := t.submitAccepted(app, caller.Member, sub, now, true)
	return err
}

func (h hostView) CheckTxnCall(call platform.TxnCall) error {
	if call.Ref.Check() != nil || call.Ref.Kind != platform.AssetQuery || call.Version < 0 || !slices.Contains([]string{"", "snapshot", "current"}, call.View) {
		return fmt.Errorf("Transaction calls need a synchronous pure query with snapshot or current reads")
	}
	owner := h.t.app(call.Ref.App)
	if owner == nil {
		return fmt.Errorf("Transaction query owner is unavailable")
	}
	for _, definition := range h.t.definitions {
		if definition.Ref != call.Ref || definition.Query == nil {
			continue
		}
		if call.Version == 0 && definition.Source == "code" {
			return nil
		}
		if call.Version > 0 && definition.Source == "tenant" && definition.QueryVersion(owner.Manifest().Version+".query-"+strconv.Itoa(call.Version)) != nil {
			return nil
		}
	}
	return fmt.Errorf("Transaction query needs its declared retained version")
}

func (q *txnCalls) Call(call platform.TxnCall, inputs json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	view := hostView{t: q.decision.tenant}
	if err := view.CheckTxnCall(call); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	store := q.snapshot
	if call.View == "current" {
		store = q.decision.records
	}
	return q.decision.tenant.readQueryVersionFrom(store, q.caller, call.Ref.App, call.Ref.Name, call.Version, inputs, at)
}

func (h hostView) QueueTxnEffect(c platform.Caller, receipt *pb.ChangeRecord, step string, target platform.TxnRef, raw json.RawMessage, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != build.ID || receipt.GetSubmission().GetAuthority() != c.App || step == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction effects require their original private owner")
	}
	var request build.WritebackRequest
	if json.Unmarshal(raw, &request) != nil || request.Version != 1 || request.Connection == "" || request.Writeback == "" || request.Dispatch != "" || request.Confirmation != "" || target.Type == "" || target.ID == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Transaction delivery has no frozen writeback request")
	}
	if refusal := h.t.checkWritebackCredential(request); refusal != nil {
		return refusal
	}
	key := receipt.GetChangeId() + ":" + step
	endpoint := build.WritebackEndpoint + request.Connection
	x := platform.Effect{ID: fmt.Sprintf("%s:%s:transaction:%s:%s", c.Tenant, c.App, key, endpoint), App: c.App, Endpoint: endpoint, Event: c.App + "/transaction", Key: key, Target: target.Type + "/" + target.ID, At: at, Due: at, State: "pending", Body: string(raw)}
	return h.queueTxnIntent(c, receipt, x)
}

func (h hostView) queueTxnIntent(c platform.Caller, receipt *pb.ChangeRecord, effect platform.Effect) *kernel.Error {
	typ := receipt.GetSubmission().GetTarget().GetType()
	applications, err := h.t.bindApplications(platform.AssetRef{App: c.App, Kind: platform.AssetObject, Name: typ}, h.t.releases.Active(), "")
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	effect.Applications = applications
	if slices.ContainsFunc(h.decision.intents, func(existing platform.Effect) bool { return existing.ID == effect.ID }) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A transaction step declared the same outbound intent twice")
	}
	if !strings.HasPrefix(effect.Target, c.App+".") {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction effects cannot target another authority")
	}
	h.decision.intents = append(h.decision.intents, effect)
	return nil
}

var _ platform.TransactionHost = hostView{}

func (h hostView) TransactionRuntime(context platform.BusinessRuntimeContext) platform.TransactionExecutor {
	return h.t.services.NewTransactionExecutor(context)
}
