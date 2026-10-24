package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (d *stagedDecision) TxnTransport() platform.TxnTransport {
	return hostView{t: d.tenant, app: d.tenant.app(construction.ID), decision: d}
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
	snapshot *recordStore
}

func (h hostView) BeginTxnCalls(c platform.Caller, at time.Time) (platform.TxnCalls, *kernel.Error) {
	if h.decision == nil || !c.Staging() || c.App != construction.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction calls require their original private owner")
	}
	return &txnCalls{decision: h.decision, caller: c, snapshot: h.decision.records.forkRecords()}, nil
}

// CheckTxnCall resolves the actual synchronous owner, rather than accepting a
// capability whose side happens to be labelled pure but only runs as a job.
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

func (h hostView) QueueTxnJournal(c platform.Caller, receipt *pb.ChangeRecord, step string, journal platform.Journal, reverseAction string, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != construction.ID || receipt.GetSubmission().GetAuthority() != c.App || step == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction journal intents require their original private owner")
	}
	if h.t.app(platform.BooksApp) == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The books owner is unavailable")
	}
	key := receipt.GetChangeId() + ":" + step
	journal.ID, journal.Source = "jnl-"+key, c.App+"/"+receipt.GetChangeId()
	x := platform.Effect{ID: fmt.Sprintf("%s:%s:journal:%s:%s", c.Tenant, c.App, key, construction.BooksEndpoint), App: c.App, Endpoint: construction.BooksEndpoint, Event: c.App + "/journal", Key: key, Target: target(receipt.GetSubmission()), At: at, Due: at, State: "pending", Body: string(platform.Raw(journal))}
	if step == "journal" {
		key = receipt.GetChangeId()
		journal.ID = "jnl-" + key
		typ := receipt.GetSubmission().GetTarget().GetType()
		x.ID, x.Key = fmt.Sprintf("%s:%s:journal:%s:%s", c.Tenant, c.App, key, construction.BooksEndpoint), key
		var entry map[string]any
		_ = json.Unmarshal(platform.Raw(journal), &entry)
		x.Body = h.t.bookBody(entry, typ, receipt.GetSubmission().GetTarget().GetId(), reverseAction)
	} else if reverseAction != "" {
		var entry map[string]any
		_ = json.Unmarshal(platform.Raw(journal), &entry)
		x.Body = h.t.bookBody(entry, receipt.GetSubmission().GetTarget().GetType(), receipt.GetSubmission().GetTarget().GetId(), reverseAction)
	}
	return h.queueTxnIntent(c, receipt, x)
}

func (h hostView) queueTxnIntent(c platform.Caller, receipt *pb.ChangeRecord, effect platform.Effect) *kernel.Error {
	typ := receipt.GetSubmission().GetTarget().GetType()
	applications, err := h.t.bindApplications(platform.AssetRef{App: c.App, Kind: platform.AssetObject, Name: typ}, h.t.releases.active, "")
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

var _ host.TransactionHost = hostView{}
