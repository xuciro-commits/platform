package host

import (
	"context"
	"encoding/json"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	aigateway "platformserver/services/ai/gateway"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	"slices"
	"time"
)

func (h *Host) mcp(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
	aigateway.Tools{Host: t, Now: h.Now}.Serve(w, r, m)
}
func (t *Tenant) TenantID() string { return t.ID }
func (t *Tenant) ModelToolCatalog(m platform.Member) protocol.ModelToolCatalog {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (protocol.ModelToolCatalog, *platform.Error) {
		var entities []platform.RecordTypeInfo
		for _, def := range t.Definitions(m) {
			if def.Entity != nil && (def.Source == "code" || build.StoreProtocolAssetActive(t.Projection, t.ID, def.Ref)) {
				entities = append(entities, *def.Entity)
			}
		}
		actions := slices.DeleteFunc(slices.Clone(t.Catalog(m)), func(a platform.Action) bool { return build.ResourceActionRetired(t.Projection, a.Schema, a.Target) })
		return protocol.ModelToolCatalog{Entities: entities, Actions: actions, Applications: t.Applications(m), HiddenAIFields: records.HiddenAIFields(t.Projection)}, nil
	})
	return value
}
func (t *Tenant) QueryModelObjects(m platform.Member, q protocol.ModelObjectQuery, now time.Time) (json.RawMessage, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (json.RawMessage, *platform.Error) {
		if len(q.Groups) > 0 || len(q.Measures) > 0 {
			out, err := t.AggregateObjectSet(m, objects.ObjectSetRequest{Set: q.Set, Groups: q.Groups, Measures: q.Measures}, now)
			return platform.Raw(out), err
		}
		page, _, err := t.loadObjectSet(t.Projection, platform.NewCaller(runtime{t: t}, m, "", false, false), q.Set, objects.Window{Sort: q.Sort, Offset: q.Offset, Limit: q.Limit, Bounded: true}, now)
		return platform.Raw(page), err
	})
}
func (t *Tenant) SubmitModelAction(ctx context.Context, m platform.Member, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue) {
	sub.Authority, _ = recordRead(t, ctx, func(t *Tenant) (string, *platform.Error) { return t.owner["action:"+sub.GetSchema().GetName()], nil })
	if sub.Authority == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Action is unavailable"), nil
	}
	return t.submit(ctx, m, sub, at, submitOptions{})
}
