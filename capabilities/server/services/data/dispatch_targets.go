package data

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type DispatchTarget struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (o *Owner) DispatchTargets(c platform.Caller, member platform.Member, id string, at time.Time, admits func(platform.Member) *platform.Error, readable func(platform.Member, string, time.Time) bool) ([]DispatchTarget, *platform.Error) {
	if refusal := admits(member); refusal != nil {
		return nil, refusal
	}
	run, found := platform.Get[platform.DispatchRun](c, id)
	if !found || member.Agent || !member.Holds(App, run.Definition.ReviewerRole) || !readable(member, run.Subject, at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the current dispatch reviewer may choose an external target")
	}
	out := []DispatchTarget{}
	for offset := 0; ; {
		connections, total, refusal := platform.Find[Connection](o.host.Automation(c, App), platform.Query{Domain: json.RawMessage(`[["state","=","ready"]]`), Sort: []string{"title", "id"}, Limit: 500, Offset: offset})
		if refusal != nil {
			return nil, refusal
		}
		for _, connection := range connections {
			if slices.Contains([]string{"http", "odata"}, connection.Kind) {
				out = append(out, DispatchTarget{ID: connection.ID, Title: connection.Title})
			}
		}
		offset += len(connections)
		if offset >= total {
			return out, nil
		}
	}
}
