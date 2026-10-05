package platformserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/flow"
	"platformserver/apps/relations"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The context graph (ADR-0021 D5): one read that grounds a person or an agent
// in a record — its fields and history, the records it references and that
// reference it, its links across apps, the flows and the tasks about it.
// Within the reader's scope: a person reads with their own grants, an app's
// automation reads its own app (ADR-0050 D2). There is no host fallback — a
// read without a reader answers nothing, so a flow's agent can never quietly
// read as the host did before the review (AI-01).

type ContextView struct {
	Type       string         `json:"type"`
	Record     any            `json:"record"`
	History    []RecordChange `json:"history"`
	References []string       `json:"references"` // "<field>: <type>/<id>"
	Related    []Related      `json:"related"`
	Links      []string       `json:"links"`
	Flows      []FlowSummary  `json:"flows"`
	Tasks      []TaskSummary  `json:"tasks"`
}

type FlowSummary struct {
	ID    string           `json:"id"`
	Flow  string           `json:"flow"`
	State string           `json:"state"`
	Trace []flow.TraceLine `json:"trace"` // the last lines: why it moved
}

type TaskSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Answer string `json:"answer,omitempty"`
}

// host reads everything: every app's roles, tenant scope. It is the host's own
// view for indexing and maintenance, not a reader: reads taken for a person or
// an agent are taken as that person or as the agent's own app (ADR-0050 D2).
func (t *Tenant) host() platform.Member {
	m := platform.Member{ID: "app:" + AgentApp, Tenant: t.ID, Roles: map[string]string{}}
	for _, a := range t.apps {
		m.Roles[a.Manifest().ID] = "*"
	}
	return m
}

// appReader is the member an app's automation reads as: exactly that app's
// records, and none of any other app's. It is a strict subset of host(): the
// same roles for the app's own types, no role in any other app. An agent a
// flow runs reads as its app (ADR-0050 D2, review AI-01).
func (t *Tenant) appReader(app string) *platform.Member {
	return &platform.Member{ID: "app:" + app, Tenant: t.ID, Roles: map[string]string{app: platform.AnyMember}}
}

func (t *Tenant) Context(reader *platform.Member, typ, id string, now time.Time) (ContextView, *kernel.Error) {
	if reader == nil || reader.Tenant != t.ID {
		return ContextView{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	m := *reader
	view, err := t.RecordOf(m, typ, id, now)
	if err != nil {
		return ContextView{}, err
	}
	out := ContextView{Type: typ, Record: view.Record, History: view.History, Related: view.Related, References: []string{}, Links: []string{}, Flows: []FlowSummary{}, Tasks: []TaskSummary{}}
	if len(out.History) > 20 {
		out.History = out.History[:20]
	}
	t.records.mu.Lock()
	et := t.records.types[typ]
	t.records.mu.Unlock()
	v := reflect.ValueOf(view.Record)
	for _, f := range et.info.Fields {
		if f.Ref == "" {
			continue
		}
		x := v.FieldByIndex(f.Index)
		if x.Kind() != reflect.Slice { // a reference, or references
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
			if t.Readable(m, ref, now) {
				out.Links = append(out.Links, ref)
			}
		}
	}
	if t.procs != nil {
		key, _ := json.Marshal([]any{[]any{"key", "=", id}})
		instances, err := t.Records(m, flow.InstanceType, platform.Query{Domain: key, Sort: []string{"id"}}, now)
		if err == nil {
			for _, record := range instances.Records {
				x := record.(flow.FlowInstance)
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
		tasks, err := t.Records(m, work.TaskType, platform.Query{Domain: about, Sort: []string{"id"}}, now)
		if err == nil {
			for _, record := range tasks.Records {
				x := record.(work.WorkTask)
				out.Tasks = append(out.Tasks, TaskSummary{ID: x.ID, Title: x.Title, State: x.State, Answer: x.Answer})
			}
		}
	}
	return out, nil
}

// Hit is a record a search found.
type Hit struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Search finds records of every type the reader may read by text: the global
// search of the workspace and an agent's search tool. Without a reader it
// finds nothing (ADR-0050 D2).
func (t *Tenant) Search(reader *platform.Member, q string, now time.Time) []Hit {
	if reader == nil || reader.Tenant != t.ID {
		return nil
	}
	m := *reader
	out := []Hit{}
	if strings.TrimSpace(q) == "" {
		return out
	}
	for _, info := range t.Entities(m) {
		if info.Type == relations.FollowType { // who follows what is no answer to a search
			continue
		}
		query := platform.Query{Search: q, Limit: 5}
		if rest, named := t.names(info, q); named { // "open opportunities" searches opportunities for "open" (ADR-0023 D1)
			query.Search = rest
		}
		page, err := t.Records(m, info.Type, query, now)
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

// protocolActions are the actions a protocol declares, from any app providing it.
func (t *Tenant) protocolActions(protocol string) []platform.Action {
	for _, a := range t.apps {
		for _, p := range a.Manifest().Provides {
			if p.Protocol.ID() == protocol {
				return p.Protocol.Actions
			}
		}
	}
	return nil
}
