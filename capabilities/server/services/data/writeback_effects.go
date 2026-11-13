package data

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (o *Owner) WritebackEffects(tenant string, c platform.Caller, e platform.Event, preparing bool, credential func(WritebackRequest) *kernel.Error) ([]platform.Effect, *kernel.Error) {
	s := e.Record.GetSubmission()
	schema := s.GetSchema().GetName()
	writebacks, _, refusal := platform.Find[Writeback](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	if preparing && refusal != nil {
		return nil, refusal
	}
	var planned []platform.Effect
	for _, w := range writebacks {
		eventSchema, on := w.Schema(), w.On
		if w.Published != "" {
			var asset platform.ReleaseAsset
			var definition WritebackDefinition
			if DecodeDescriptor([]byte(w.Published), &asset) != nil || DecodeDescriptor(asset.Body, &definition) != nil {
				if preparing {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The writeback publication is unavailable")
				}
				continue
			}
			eventSchema, on = definition.Object+"."+definition.On, definition.On
		}
		if on == "confirmation" || on == "dispatch" || on == "transaction" || eventSchema != schema {
			continue
		}
		w, err := o.ExecutionWriteback(w)
		if err != nil {
			if preparing {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
			}
			continue
		}
		if w.On == "confirmation" || w.On == "dispatch" || w.On == "transaction" || w.Schema() != schema {
			continue
		}
		conn, ok := platform.Get[Connection](c, w.Connection)
		if !ok {
			if preparing {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The writeback connection is not configured")
			}
			continue
		}
		if preparing {
			conn, err = o.ExecutionConnection(conn)
		} else {
			conn, err = o.ConnectionConfiguration(conn)
		}
		if err != nil {
			if preparing {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
			}
			continue
		}
		req, err := w.Request(conn, s.GetTarget().GetId(), s.GetPayload())
		if err != nil {
			if preparing {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
			}
			continue
		}
		if preparing {
			if refusal := credential(req); refusal != nil {
				return nil, refusal
			}
		}
		body, _ := json.Marshal(req)
		at := e.Record.GetRecordedTime().AsTime()
		planned = append(planned, platform.Effect{ID: fmt.Sprintf("%s:%s:%s:%s", tenant, App, e.Record.GetChangeId(), w.ID),
			Endpoint: WritebackEndpoint + conn.ID, Event: "writeback/" + w.Name, App: App, Key: e.Record.GetChangeId(), Target: s.GetTarget().GetType() + "/" + s.GetTarget().GetId(), At: at,
			State: "pending", Due: at, Body: string(body)})
	}
	return planned, nil
}

type IntegrationEffect struct {
	ID       string    `json:"id"`
	Endpoint string    `json:"endpoint"`
	Event    string    `json:"event"`
	State    string    `json:"state"`
	Due      time.Time `json:"due"`
	Last     time.Time `json:"last,omitzero"`
	Error    string    `json:"error,omitempty"`
}

func IntegrationEffects(effects []platform.Effect) []IntegrationEffect {
	out := []IntegrationEffect{}
	for _, effect := range effects {
		if effect.App != App || !strings.HasPrefix(effect.Endpoint, WritebackEndpoint) ||
			!strings.HasPrefix(effect.Event, "writeback/") && !slices.Contains([]string{App + "/confirmation", App + "/dispatch", App + "/transaction"}, effect.Event) {
			continue
		}
		item := IntegrationEffect{ID: effect.ID, Endpoint: effect.Endpoint, Event: effect.Event, State: effect.State, Due: effect.Due, Last: effect.Last}
		if effect.Error != "" {
			item.Error = "The last delivery attempt failed"
			if effect.PublicMessage != "" {
				item.Error = effect.PublicMessage
			}
		}
		out = append(out, item)
	}
	return out
}

func TxnWritebackEffect(c platform.Caller, receipt *pb.ChangeRecord, step string, target platform.TxnRef, raw json.RawMessage, at time.Time, private bool, credential func(WritebackRequest) *kernel.Error, bind func(typ string) ([]platform.AssetBinding, error), declared func() []platform.Effect) (platform.Effect, *kernel.Error) {
	if !private || !c.Staging() || c.App != App || receipt.GetSubmission().GetAuthority() != c.App || step == "" {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction effects require their original private owner")
	}
	var request WritebackRequest
	if json.Unmarshal(raw, &request) != nil || request.Version != 1 || request.Connection == "" || request.Writeback == "" || request.Dispatch != "" || request.Confirmation != "" || target.Type == "" || target.ID == "" {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Transaction delivery has no frozen writeback request")
	}
	if refusal := credential(request); refusal != nil {
		return platform.Effect{}, refusal
	}
	key := receipt.GetChangeId() + ":" + step
	endpoint := WritebackEndpoint + request.Connection
	effect := platform.Effect{ID: fmt.Sprintf("%s:%s:transaction:%s:%s", c.Tenant, c.App, key, endpoint), App: c.App, Endpoint: endpoint, Event: c.App + "/transaction", Key: key, Target: target.Type + "/" + target.ID, At: at, Due: at, State: "pending", Body: string(raw)}
	applications, err := bind(receipt.GetSubmission().GetTarget().GetType())
	if err != nil {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	effect.Applications = applications
	if slices.ContainsFunc(declared(), func(existing platform.Effect) bool { return existing.ID == effect.ID }) {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A transaction step declared the same outbound intent twice")
	}
	if !strings.HasPrefix(effect.Target, c.App+".") {
		return platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Transaction effects cannot target another authority")
	}
	return effect, nil
}
