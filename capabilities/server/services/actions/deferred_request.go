package actions

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
)

const DeferredRequestEndpoint = protocol.DeferredRequestEndpoint

type deferredRequest struct {
	Protocol   string         `json:"protocol"`
	Action     string         `json:"action"`
	Provider   string         `json:"provider"`
	Reply      string         `json:"reply,omitempty"`
	Submission *pb.Submission `json:"submission"`
}

func (r Router) PlanDeferredRequest(c platform.Caller, record *pb.ChangeRecord, q platform.Request) (platform.Effect, *platform.Error) {
	if c.Automation || !r.consumes(c, q.Protocol) {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A deferred capability request requires an authorized member of a consuming application")
	}
	binding, ok := r.Resolve(q.Protocol)
	if !ok {
		if q.Optional {
			return platform.Effect{}, nil
		}
		return platform.Effect{}, r.Unavailable(q.Protocol)
	}
	key := q.Target
	if q.Key != "" {
		key = q.Key
	}
	sub, ok := r.Submission(c.ID, q.Protocol, q.Action, q.Target, platform.Raw(q.Payload), key)
	if !ok || !c.InScope(sub.GetSchema().GetName()) {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The requested capability action is unavailable or outside the caller's scope")
	}
	sub.CorrelationId = record.GetSubmission().GetIdempotencyKey()
	body, err := json.Marshal(deferredRequest{Protocol: q.Protocol, Action: q.Action, Provider: binding.App() + "/" + binding.EntityType(), Reply: q.Reply, Submission: sub})
	if err != nil {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid deferred capability request")
	}
	at := record.GetRecordedTime().AsTime()
	state := "pending"
	if r.Bindings != nil && r.Bindings.Paused(q.Protocol) {
		state = "held"
	}
	return platform.Effect{ID: fmt.Sprintf("%s:%s:capability-request:%s:%s", r.Tenant, c.App, key, DeferredRequestEndpoint), Endpoint: DeferredRequestEndpoint, Event: c.App + "/capability-request", App: c.App, Key: key, Target: record.GetSubmission().GetTarget().GetType() + "/" + record.GetSubmission().GetTarget().GetId(), At: at, Due: at, State: state, Body: string(body)}, nil
}

func (r Router) DeliverDeferredRequest(x platform.Effect, now time.Time, member func(string) (platform.Member, bool), prior func(platform.Service, *pb.Submission) (*pb.ChangeRecord, *platform.Error, bool), submit func(platform.Member, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error), readProvider func(func(Router) bool) (bool, *platform.Error)) platform.Outcome {
	result := func(state, detail string) platform.Outcome {
		return platform.Outcome{Effect: x.ID, Result: state, Detail: detail}
	}
	var request deferredRequest
	if json.Unmarshal([]byte(x.Body), &request) != nil || request.Submission == nil {
		return result("rejected", "Invalid accepted capability request")
	}
	sub := request.Submission
	sub.IdempotencyKey = fmt.Sprintf("%s:%d", x.ID, x.Attempts+1)
	settle := func(record *pb.ChangeRecord, refusal *platform.Error) platform.Outcome {
		if refusal != nil {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
				return result("unknown", "The accepted result does not match the original request")
			}
			return platform.Outcome{Effect: x.ID, Result: "rejected", Detail: refusal.Message, PublicMessage: refusal.Message}
		}
		if record == nil {
			return result("unknown", "No accepted result was returned")
		}
		return platform.Outcome{Effect: x.ID, Result: "delivered", Answer: platform.Raw(map[string]string{"change": record.GetChangeId(), "target": sub.GetTarget().GetType() + "/" + sub.GetTarget().GetId()})}
	}
	if record, refusal, known := prior(r.Host.App(sub.GetAuthority()), sub); known {
		return settle(record, refusal)
	}
	m, found := member(sub.GetPrincipalId())
	if !found || !m.Active() || !m.InScope(sub.GetSchema().GetName()) {
		return result("rejected", "The original caller is no longer authorized")
	}
	available, readErr := readProvider(func(r Router) bool {
		binding, ok := r.Resolve(request.Protocol)
		current, served := r.Submission(m.ID, request.Protocol, request.Action, sub.GetTarget().GetId(), sub.GetPayload(), sub.GetIdempotencyKey())
		return r.Host.OwnerActive(x.App) && ok && binding.App()+"/"+binding.EntityType() == request.Provider && served && current.GetSchema().GetName() == sub.GetSchema().GetName() && current.GetAuthority() == sub.GetAuthority() && current.GetTarget().GetType() == sub.GetTarget().GetType()
	})
	if readErr != nil {
		return result("unknown", "The capability provider could not be read consistently; reconcile the original request before retrying")
	}
	if !available {
		return result("rejected", "The originally selected capability provider is unavailable")
	}
	record, refusal := submit(m, sub, now)
	if record != nil && refusal == nil {
		return settle(record, nil)
	}
	if record, refusal, known := prior(r.Host.App(sub.GetAuthority()), sub); known {
		return settle(record, refusal)
	}
	return result("unknown", "No durable acceptance or refusal was found; reconcile the original request before retrying")
}

func DeferredRequestEndpoints(endpoints []Endpoint) []Endpoint {
	return append([]Endpoint{{ID: DeferredRequestEndpoint, Kind: DeferredRequestEndpoint}}, endpoints...)
}

func SyncInFlight(store *records.Store, queued []Queued) {
	if store == nil {
		return
	}
	for _, et := range store.SortedTypes() {
		info := et.Info()
		if info.InFlight == "" {
			continue
		}
		ownerType := strings.TrimSuffix(info.Type, ".inflight")
		var entries []records.Entry[*records.Row]
		seen := map[string]bool{}
		for _, q := range queued {
			if q.Endpoint != DeferredRequestEndpoint || protocol.EffectSettled(q.State) {
				continue
			}
			var req deferredRequest
			if json.Unmarshal([]byte(q.Body), &req) != nil || req.Submission == nil || req.Protocol != info.InFlight || !strings.HasSuffix(req.Provider, "/"+ownerType) {
				continue
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(req.Submission.GetPayload(), &fields) != nil || fields == nil {
				fields = map[string]json.RawMessage{}
			}
			rowID := strings.NewReplacer("/", "-", ":", "-").Replace(q.Key)
			if rowID == "" {
				rowID = strings.NewReplacer("/", "-", ":", "-").Replace(q.ID)
			}
			if seen[rowID] {
				continue
			}
			seen[rowID] = true
			reason := q.PublicMessage
			if reason == "" {
				reason = q.Error
			}
			fields["id"] = platform.Raw(rowID)
			fields["action"] = platform.Raw(req.Action)
			fields["state"] = platform.Raw(q.State)
			if reason != "" {
				fields["error"] = platform.Raw(reason)
			}
			val := reflect.New(info.Go).Elem()
			if json.Unmarshal(platform.Raw(fields), val.Addr().Interface()) != nil {
				continue
			}
			changedAt := q.Last
			if changedAt.IsZero() {
				changedAt = q.At
			}
			val.Field(0).Set(reflect.ValueOf(platform.Record{ID: rowID, Revision: 1, Created: platform.Stamp{At: q.At}, Changed: platform.Stamp{At: changedAt}}))
			entries = append(entries, records.Entry[*records.Row]{Key: rowID, Value: &records.Row{Value: val}})
		}
		slices.SortFunc(entries, func(a, b records.Entry[*records.Row]) int { return strings.Compare(a.Key, b.Key) })
		_ = store.SetProjectionRows(info.Type, entries)
	}
}
