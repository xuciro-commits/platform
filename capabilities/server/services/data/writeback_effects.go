package data

import (
	"encoding/json"
	"fmt"

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
