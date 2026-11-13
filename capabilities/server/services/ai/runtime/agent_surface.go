package agents

import (
	"encoding/json"

	"platformserver/platform"
)

type SurfaceSource struct {
	Builder    string
	Call       *platform.AgentAction
	Conditions func(ref platform.AssetRef) []string
}

func (s SurfaceSource) Surface(app string, definitions []platform.Definition) platform.AgentSurface {
	var out platform.AgentSurface
	for _, definition := range definitions {
		if app != "" && definition.Ref.App != app || definition.Ref.App == s.Builder && definition.Source == "code" && definition.Function == nil {
			continue
		}
		switch {
		case definition.Entity != nil:
			out.Entities = append(out.Entities, *definition.Entity)
		case definition.Action != nil:
			action := platform.AgentAction{Owner: definition.Ref.App, Action: *definition.Action, Conditions: s.Conditions(definition.Ref)}
			out.Actions = append(out.Actions, action)
		case definition.Function != nil:
			if s.Call != nil {
				out.Functions = append(out.Functions, platform.AgentFunction{Owner: definition.Ref.App, Function: *definition.Function, Call: *s.Call})
			}
		}
	}
	return out
}

func QueryObservation(object string, records, page any) platform.AgentObservation {
	raw, _ := json.Marshal(records)
	var ids []struct{ ID string }
	_ = json.Unmarshal(raw, &ids)
	out := platform.AgentObservation{}
	for _, row := range ids {
		out.Sources = append(out.Sources, object+"/"+row.ID)
	}
	out.Body, _ = json.Marshal(page)
	return out
}

func ObjectObservation(object string, page any) platform.AgentObservation {
	body, _ := json.Marshal(page)
	var rows struct {
		Records []struct{ ID string } `json:"records"`
	}
	_ = json.Unmarshal(body, &rows)
	out := platform.AgentObservation{Body: body}
	for _, row := range rows.Records {
		if object != "" && row.ID != "" {
			out.Sources = append(out.Sources, object+"/"+row.ID)
		}
	}
	return out
}
