package host

import (
	"platformserver/kernel/protocol"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
)

func (t *Tenant) host() platform.Member {
	m := platform.Member{ID: "host:maintenance", Tenant: t.ID, Roles: map[string]string{}}
	for _, a := range t.apps {
		m.Roles[a.Manifest().ID] = "*"
	}
	return m
}

func (t *Tenant) contextRead(reader *platform.Member, typ, id string, now time.Time) (objects.ContextView, *platform.Error) {
	if reader == nil || reader.Tenant != t.ID {
		return objects.ContextView{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	m := *reader
	records := func(present bool, typ string) func(q platform.Query) ([]any, bool) {
		return func(q platform.Query) ([]any, bool) {
			if !present {
				return nil, false
			}
			page, err := t.objectOwner(t.Projection).Records(m, typ, q, now)
			return page.Records, err == nil
		}
	}
	return objects.ContextReader{
		Detail: func(sections ...string) (objects.RecordView, *platform.Error) {
			return t.objectOwner(t.Projection).RecordDetail(m, typ, id, now, sections...)
		},
		Info: func() platform.RecordTypeInfo {
			t.Projection.Lock()
			et := t.Projection.Type(typ)
			t.Projection.Unlock()
			return et.Info()
		},
		Links: func(entity string) []string {
			if t.Linker == nil {
				return nil
			}
			return t.Linker.Links(t.caller(m, t.Linker.(platform.Service), false), entity)
		},
		Readable: func(ref string) bool { return t.readable(m, ref, now) },
		Flows:    records(t.Processes != nil, protocol.FlowInstanceType),
		Tasks:    records(t.Tasks != nil, protocol.WorkTaskType),
	}.Read(typ, id)
}

func (t *Tenant) searchRead(reader *platform.Member, q string, now time.Time) []build.Hit {
	if reader == nil || reader.Tenant != t.ID {
		return nil
	}
	m := *reader
	search := build.Search{Resources: t.resourceDirectory(m).Resources, Entities: t.Entities(m), Names: t.names,
		Read: func(typ string, query platform.Query) ([]any, *platform.Error) {
			page, err := t.objectOwner(t.Projection).Records(m, typ, query, now)
			return page.Records, err
		}}
	return search.Find(q)
}
