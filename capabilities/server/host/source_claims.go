package host

import (
	"context"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) sourcePreparation() dataowner.SourcePreparation {
	return dataowner.SourcePreparation{
		Record: func(typ, id string, required bool) (dataowner.SourceRecord, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (dataowner.SourceRecord, *kernel.Error) {
				history, fields, archived, change, refusal := t.objectOwner(t.Projection).SourceState(typ, id, required)
				return dataowner.SourceRecord{History: history, Fields: fields, Archived: archived, Change: change}, refusal
			})
		},
		Objects: func(object string) (platform.EntityInfo, map[string]bool, *kernel.Error) {
			type answer struct {
				info platform.EntityInfo
				rows map[string]bool
			}
			value, refusal := recordRead(t, context.Background(), func(t *Tenant) (answer, *kernel.Error) {
				info, rows, refusal := t.objectOwner(t.Projection).SourceObjectStates(object)
				return answer{info: info, rows: rows}, refusal
			})
			return value.info, value.rows, refusal
		},
		Authorize: func(m platform.Member, app, typ, id, action, kind string, fields []string, at time.Time) *kernel.Error {
			caller := platform.NewCaller(runtime{t: t, scoped: true}, m, app, false, false)
			decision := t.authorizeFrom(t.Projection, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: kind, Type: typ, Record: id, Action: action, Fields: fields}, at, false)
			if !decision.Allow || fields != nil && len(decision.Withheld) > 0 {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, decision.Reason)
			}
			return nil
		},
		Selected: func(typ, id, attribute string, history []records.RecordChange, incoming *acceptedformat.SourceClaim) dataowner.SelectedSourceValue {
			selected := t.objectOwner(t.Projection).SelectedSourceValue(typ, id, attribute, history, incoming)
			return dataowner.SelectedSourceValue{Kind: selected.Kind, Value: selected.Value, Connector: selected.Connector, SourceTime: selected.SourceTime}
		},
		Restores: func(typ, id, change string) bool {
			return t.objectOwner(t.Projection).SourceRestores(typ, id, change)
		},
		Deletes: func(typ, id, connector string) bool {
			return t.objectOwner(t.Projection).SourceDeletionRemoves(typ, id, connector)
		},
		Claimed: func(object, id, connector string, info platform.EntityInfo) bool {
			return t.Claims.ClaimedBySource(t.ID, object, id, connector, info)
		},
	}
}

func (t *Tenant) declaredRows(object, kind, source string, input []map[string]any, now time.Time) ([]dataowner.SourceRow, *platform.DataSourceBinding, error) {
	type answer struct {
		rows    []dataowner.SourceRow
		binding *platform.DataSourceBinding
	}
	value, err := readWithError(t, context.Background(), func(t *Tenant) (answer, error) {
		var info platform.EntityInfo
		t.Projection.Lock()
		if entity := t.Projection.Type(object); entity != nil {
			info = entity.Info()
		}
		t.Projection.Unlock()
		rows, binding, err := dataowner.DeclaredSourceRows(info, kind, source, input, now, func(id string) string {
			if resolved, failure := t.Claims.Identity.Resolve(&pb.EntityRef{Type: object, Id: id}); failure == nil && len(resolved) == 1 && resolved[0].Type == object {
				return resolved[0].ID
			}
			return id
		})
		return answer{rows: rows, binding: binding}, err
	})
	return value.rows, value.binding, err
}

func (t *Tenant) prepareRows(member platform.Member, object, producer string, rows []dataowner.SourceRow, now time.Time) (int, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (int, *kernel.Error) {
		merged := 0
		resolver := t.DataOwner().MatchResolver(t.automation(build.ID, false), object, producer, func(offset int) ([]any, int, *kernel.Error) {
			page, err := t.recordsRead(member, object, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, now)
			return page.Records, page.Total, err
		})
		if resolver != nil {
			for i := range rows {
				resolver.Resolve(&rows[i])
			}
			merged = resolver.Merged
		}
		t.Projection.Lock()
		defer t.Projection.Unlock()
		entity := t.Projection.Type(object)
		dataowner.AssignVerbs(rows, func(id string) bool { return entity != nil && entity.Rows().Get(id) != nil })
		return merged, nil
	})
}

func (t *Tenant) absentSourceRows(object string, binding *platform.DataSourceBinding, current []dataowner.SourceRow, now time.Time) ([]dataowner.SourceRow, *kernel.Error) {
	return t.sourcePreparation().Absent(object, binding, current, now)
}
