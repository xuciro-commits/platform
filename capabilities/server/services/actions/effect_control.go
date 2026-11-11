package actions

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type EffectControl struct {
	Outbox    *Outbox
	Endpoints *Endpoints
	Agents    platform.AgentRuntime
	Told      func(platform.Effect, platform.Outcome, time.Time, bool)
	Valid     func(Endpoint) bool
	Reviewed  func(Queued) bool
}

func (e EffectControl) RemoveEndpoint(c platform.Caller, id string, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if !e.Endpoints.Has(id) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if e.Outbox.Any(func(x Queued) bool { return x.Endpoint == id && x.State == "unknown" }) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Resolve unknown deliveries before removing their endpoint")
	}
	return func(*pb.ChangeRecord) {
		e.Endpoints.Remove(id)
		var dropped []platform.Effect
		e.Outbox.UpdateAll(func(x *Queued) {
			if x.Endpoint == id && !platform.EffectSettled(x.State) {
				x.State = "discarded"
				dropped = append(dropped, x.Effect)
			}
		})
		for _, x := range dropped {
			e.Told(x, platform.Outcome{Effect: x.ID, Result: "discarded", Detail: "its endpoint was removed by " + c.ID}, now, c.Replaying)
		}
	}, nil
}

func (e EffectControl) AddEndpoint(id string, payload []byte) (func(*pb.ChangeRecord), *kernel.Error) {
	var ep Endpoint
	if json.Unmarshal(payload, &ep) != nil || id == "" || !e.Valid(ep) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if e.Endpoints.Has(id) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	ep.ID = id
	return func(*pb.ChangeRecord) { e.Endpoints.Add(ep) }, nil
}

func (e EffectControl) find(id string) (Queued, bool, *kernel.Error) {
	x, found := e.Outbox.Get(id)
	if found && e.Reviewed(x) {
		return x, found, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Manage this command through its dispatch review and declared reviewer role")
	}
	return x, found, nil
}

func (e EffectControl) Approve(c platform.Caller, id string, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	x, found, refusal := e.find(id)
	if refusal != nil {
		return nil, refusal
	}
	if !found || x.State != "held" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if c.Agent {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return func(r *pb.ChangeRecord) {
		e.Outbox.Update(id, func(q *Queued) { q.State, q.Due = "pending", now })
		if x.Run != "" && e.Agents != nil {
			e.Agents.RecordAgentSignal(c, r, x.Run, platform.Signal{At: now, Kind: "approved", By: c.ID, Detail: x.Event}, now)
		}
	}, nil
}

func (e EffectControl) Discard(c platform.Caller, id string, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	x, found, refusal := e.find(id)
	if refusal != nil {
		return nil, refusal
	}
	if found && x.State == "unknown" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An unknown delivery needs reconciliation or a recorded human decision")
	}
	if !found || platform.EffectSettled(x.State) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return func(r *pb.ChangeRecord) {
		held := false
		e.Outbox.Update(id, func(q *Queued) {
			held = q.State == "held"
			q.State = "discarded"
			x = *q
		})
		if held && x.Run != "" && e.Agents != nil {
			e.Agents.RecordAgentSignal(c, r, x.Run, platform.Signal{At: now, Kind: "discarded", By: c.ID, Detail: x.Event}, now)
		}
		if e.Agents != nil {
			e.Agents.AgentEffectEnded(c, r, x.Effect, platform.Outcome{Result: "discarded", Detail: "discarded by " + c.ID}, now)
		}
		e.Told(x.Effect, platform.Outcome{Effect: x.ID, Result: "discarded", Detail: "discarded by " + c.ID}, now, c.Replaying)
	}, nil
}

func (e EffectControl) Resolve(c platform.Caller, id string, payload []byte, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	x, found, refusal := e.find(id)
	if refusal != nil {
		return nil, refusal
	}
	var input struct {
		Outcome, Reason string
		Answer          json.RawMessage
	}
	if !found || x.State != "unknown" || x.Sending || c.Agent || json.Unmarshal(payload, &input) != nil || !slices.Contains([]string{"delivered", "rejected"}, input.Outcome) || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 4096 || len(input.Answer) > 64<<10 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An unknown delivery needs a human result and bounded evidence")
	}
	return func(*pb.ChangeRecord) {
		e.Outbox.Update(id, func(q *Queued) {
			q.State, q.Error, q.Last = input.Outcome, "Resolved by "+c.ID+": "+input.Reason, now
			x = *q
		})
		e.Told(x.Effect, platform.Outcome{Effect: x.ID, Result: input.Outcome, Detail: x.Error, Answer: input.Answer}, now, c.Replaying)
	}, nil
}

func (e EffectControl) Retry(id string, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	x, found, refusal := e.find(id)
	if refusal != nil {
		return nil, refusal
	}
	if !found || (x.State != "failed" && x.State != "rejected") {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return func(*pb.ChangeRecord) {
		e.Outbox.Update(id, func(q *Queued) {
			q.State, q.Due, q.Since = "pending", now, q.Attempts
			q.CallbackError = ""
		})
	}, nil
}
