package host

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) host() platform.Member {
	m := platform.Member{ID: "host:maintenance", Tenant: t.ID, Roles: map[string]string{}}
	for _, a := range t.apps {
		m.Roles[a.Manifest().ID] = "*"
	}
	return m
}

func (t *Tenant) appReader(app string) *platform.Member {
	return &platform.Member{ID: "app:" + app, Tenant: t.ID, Roles: map[string]string{app: platform.AnyMember}}
}

func (t *Tenant) contextRead(reader *platform.Member, typ, id string, now time.Time) (objects.ContextView, *kernel.Error) {
	if reader == nil || reader.Tenant != t.ID {
		return objects.ContextView{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	m := *reader
	view, err := t.recordDetail(m, typ, id, now, objects.RecordSectionHistory, objects.RecordSectionLinks)
	if err != nil {
		return objects.ContextView{}, err
	}
	t.Projection.Lock()
	et := t.Projection.Type(typ)
	t.Projection.Unlock()
	out := objects.RecordContext(typ, view.Record, view.History, view.Related, et.Info())
	if t.linker != nil {
		for _, ref := range t.linker.Links(t.caller(m, t.linker.(platform.App), false), typ+"/"+id) {
			if t.readable(m, ref, now) {
				out.Links = append(out.Links, ref)
			}
		}
	}
	if t.procs != nil {
		key, _ := json.Marshal([]any{[]any{"key", "=", id}})
		if instances, err := t.recordsRead(m, platform.FlowInstanceType, platform.Query{Domain: key, Sort: []string{"id"}}, now); err == nil {
			out.AddFlows(instances.Records)
		}
	}
	if t.tasks != nil {
		about, _ := json.Marshal([]any{[]any{"ref", "=", typ + "/" + id}})
		if tasks, err := t.recordsRead(m, platform.WorkTaskType, platform.Query{Domain: about, Sort: []string{"id"}}, now); err == nil {
			out.AddTasks(tasks.Records)
		}
	}
	return out, nil
}

func (t *Tenant) searchRead(reader *platform.Member, q string, now time.Time) []build.Hit {
	if reader == nil || reader.Tenant != t.ID {
		return nil
	}
	m := *reader
	search := build.Search{Resources: t.resourceDirectory(m).Resources, Entities: t.Entities(m), Names: t.names,
		Read: func(typ string, query platform.Query) ([]any, *kernel.Error) {
			page, err := t.recordsRead(m, typ, query, now)
			return page.Records, err
		}}
	if owner := t.App(build.ID); owner != nil {
		search.Owned = owner.Manifest().Entities
	}
	return search.Find(q)
}

func (t *Tenant) protocolActions(protocol string) []platform.Action {
	for _, a := range t.LiveApps() {
		for _, p := range a.Manifest().Provides {
			if p.Protocol.ID() == protocol {
				return p.Protocol.Actions
			}
		}
	}
	return nil
}
