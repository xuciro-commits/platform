package host

import (
	"encoding/json"
	"fmt"
	"platformserver/services/actions"
	dataowner "platformserver/services/data"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type IntegrationEffect struct {
	ID       string    `json:"id"`
	Endpoint string    `json:"endpoint"`
	Event    string    `json:"event"`
	State    string    `json:"state"`
	Due      time.Time `json:"due"`
	Last     time.Time `json:"last,omitzero"`
	Error    string    `json:"error,omitempty"`
}

func (t *Tenant) integrationEffects(m platform.Member, now time.Time) ([]IntegrationEffect, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	if !m.Holds(build.ID, build.Builder) && !m.Holds(build.ID, build.Integrator) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	out := []IntegrationEffect{}
	for _, effect := range t.Effects(now) {
		writeback := strings.HasPrefix(effect.Event, "writeback/") && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		confirmation := effect.Event == build.ID+"/confirmation" && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		dispatch := effect.Event == build.ID+"/dispatch" && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		transaction := effect.Event == build.ID+"/transaction" && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		if effect.App != build.ID || !writeback && !confirmation && !dispatch && !transaction {
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
	return out, nil
}

func (t *Tenant) writebackEffects(e platform.Event, preparing bool) ([]platform.Effect, *kernel.Error) {
	if t.App(build.ID) == nil {
		return nil, nil
	}
	s := e.Record.GetSubmission()
	schema := s.GetSchema().GetName()
	if e.App != build.ID {
		return nil, nil
	}
	c := t.automation(build.ID, false)
	writebacks, _, refusal := platform.Find[dataowner.Writeback](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	if preparing && refusal != nil {
		return nil, refusal
	}
	var planned []platform.Effect
	owner := t.DataOwner()
	for _, w := range writebacks {
		eventSchema, on := w.Schema(), w.On
		if w.Published != "" {
			var asset platform.ReleaseAsset
			var definition dataowner.WritebackDefinition
			if dataowner.DecodeDescriptor([]byte(w.Published), &asset) != nil || dataowner.DecodeDescriptor(asset.Body, &definition) != nil {
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
		w, err := owner.ExecutionWriteback(w)
		if err != nil {
			if preparing {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
			}
			continue
		}
		if w.On == "confirmation" || w.On == "dispatch" || w.On == "transaction" || w.Schema() != schema {
			continue
		}
		conn, ok := platform.Get[dataowner.Connection](c, w.Connection)
		if !ok {
			if preparing {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The writeback connection is not configured")
			}
			continue
		}
		if preparing {
			conn, err = owner.ExecutionConnection(conn)
		} else {
			conn, err = owner.ConnectionConfiguration(conn)
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
			if refusal := t.checkWritebackCredential(req); refusal != nil {
				return nil, refusal
			}
		}
		body, _ := json.Marshal(req)
		at := e.Record.GetRecordedTime().AsTime()
		planned = append(planned, platform.Effect{ID: fmt.Sprintf("%s:%s:%s:%s", t.ID, build.ID, e.Record.GetChangeId(), w.ID),
			Endpoint: dataowner.WritebackEndpoint + conn.ID, Event: "writeback/" + w.Name, App: build.ID, Key: e.Record.GetChangeId(), Target: target(s), At: at,
			State: "pending", Due: at, Body: string(body)})
	}
	return planned, nil
}

func (t *Tenant) checkWritebackCredential(request dataowner.WritebackRequest) *kernel.Error {
	if request.Secret != "" {
		if value, ok := t.CredentialValue(request.Secret, "connection/"+request.Connection, request.URL); !ok || strings.TrimSpace(string(value)) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The connection credential is not configured or permitted for this target")
		}
	}
	return nil
}

func (t *Tenant) connectionEndpoints() []*actions.Endpoint {
	var out []*actions.Endpoint
	for _, x := range t.outbound.Select(nil) {
		if !strings.HasPrefix(x.Endpoint, dataowner.WritebackEndpoint) || platform.EffectSettled(x.State) {
			continue
		}
		if !slices.ContainsFunc(out, func(ep *actions.Endpoint) bool { return ep.ID == x.Endpoint }) {
			out = append(out, &actions.Endpoint{ID: x.Endpoint, Kind: "connection"})
		}
	}
	return out
}
