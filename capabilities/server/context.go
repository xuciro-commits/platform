package platformserver

import (
	"encoding/json"
	"platformserver/kernel/records"
	"reflect"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ContextView struct {
	Type       string                 `json:"type"`
	Record     any                    `json:"record"`
	History    []records.RecordChange `json:"history"`
	References []string               `json:"references"`
	Related    []Related              `json:"related"`
	Links      []string               `json:"links"`
	Flows      []FlowSummary          `json:"flows"`
	Tasks      []TaskSummary          `json:"tasks"`
}

type FlowSummary struct {
	ID    string                   `json:"id"`
	Flow  string                   `json:"flow"`
	State string                   `json:"state"`
	Trace []platform.FlowTraceLine `json:"trace"`
}

type TaskSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Answer string `json:"answer,omitempty"`
}

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

func (t *Tenant) contextRead(reader *platform.Member, typ, id string, now time.Time) (ContextView, *kernel.Error) {
	if reader == nil || reader.Tenant != t.ID {
		return ContextView{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	m := *reader
	view, err := t.recordOf(m, typ, id, now)
	if err != nil {
		return ContextView{}, err
	}
	out := ContextView{Type: typ, Record: view.Record, History: view.History, Related: view.Related, References: []string{}, Links: []string{}, Flows: []FlowSummary{}, Tasks: []TaskSummary{}}
	if len(out.History) > 20 {
		out.History = out.History[:20]
	}
	t.records.Lock()
	et := t.records.Type(typ)
	t.records.Unlock()
	v := reflect.ValueOf(view.Record)
	for _, f := range et.Info().Fields {
		if f.Ref == "" {
			continue
		}
		x := v.FieldByIndex(f.Index)
		if x.Kind() != reflect.Slice {
			x = reflect.Append(reflect.MakeSlice(reflect.SliceOf(x.Type()), 0, 1), x)
		}
		for i := range x.Len() {
			if ref := x.Index(i).String(); ref != "" {
				out.References = append(out.References, f.Name+": "+f.Ref+"/"+ref)
			}
		}
	}
	if t.linker != nil {
		for _, ref := range t.linker.Links(t.caller(m, t.linker.(platform.App), false), typ+"/"+id) {
			if t.readable(m, ref, now) {
				out.Links = append(out.Links, ref)
			}
		}
	}
	if t.procs != nil {
		key, _ := json.Marshal([]any{[]any{"key", "=", id}})
		instances, err := t.recordsRead(m, platform.FlowInstanceType, platform.Query{Domain: key, Sort: []string{"id"}}, now)
		if err == nil {
			for _, record := range instances.Records {
				x := record.(platform.FlowInstance)
				trace := x.Trace
				if len(trace) > 8 {
					trace = trace[len(trace)-8:]
				}
				out.Flows = append(out.Flows, FlowSummary{ID: x.ID, Flow: x.Flow, State: x.State, Trace: trace})
			}
		}
	}
	if t.tasks != nil {
		about, _ := json.Marshal([]any{[]any{"ref", "=", typ + "/" + id}})
		tasks, err := t.recordsRead(m, platform.WorkTaskType, platform.Query{Domain: about, Sort: []string{"id"}}, now)
		if err == nil {
			for _, record := range tasks.Records {
				x := record.(platform.WorkTask)
				out.Tasks = append(out.Tasks, TaskSummary{ID: x.ID, Title: x.Title, State: x.State, Answer: x.Answer})
			}
		}
	}
	return out, nil
}

type Hit struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (t *Tenant) searchRead(reader *platform.Member, q string, now time.Time) []Hit {
	if reader == nil || reader.Tenant != t.ID {
		return nil
	}
	m := *reader
	out := []Hit{}
	if strings.TrimSpace(q) == "" {
		return out
	}
	for _, info := range t.Entities(m) {
		if info.Type == platform.FollowType {
			continue
		}
		query := platform.Query{Search: q, Limit: 5}
		if rest, named := t.names(info, q); named {
			query.Search = rest
		}
		page, err := t.recordsRead(m, info.Type, query, now)
		if err != nil {
			continue
		}
		for _, r := range page.Records {
			v := reflect.ValueOf(r)
			hit := Hit{Type: info.Type, ID: v.FieldByName("ID").String()}
			if f, ok := info.Field(info.Display); ok {
				hit.Title = v.FieldByIndex(f.Index).String()
			}
			out = append(out, hit)
		}
	}
	return out
}

func (t *Tenant) protocolActions(protocol string) []platform.Action {
	for _, a := range t.liveApps() {
		for _, p := range a.Manifest().Provides {
			if p.Protocol.ID() == protocol {
				return p.Protocol.Actions
			}
		}
	}
	return nil
}
