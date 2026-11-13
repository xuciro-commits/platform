package objects

import (
	"reflect"

	"platformserver/kernel/records"
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

func RecordContext(typ string, record any, history []records.RecordChange, related []Related, info platform.EntityInfo) ContextView {
	out := ContextView{Type: typ, Record: record, History: history, Related: related, References: []string{}, Links: []string{}, Flows: []FlowSummary{}, Tasks: []TaskSummary{}}
	if len(out.History) > 20 {
		out.History = out.History[:20]
	}
	v := reflect.ValueOf(record)
	for _, f := range info.Fields {
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
	return out
}

func (c *ContextView) AddFlows(records []any) {
	for _, record := range records {
		x := record.(platform.FlowInstance)
		trace := x.Trace
		if len(trace) > 8 {
			trace = trace[len(trace)-8:]
		}
		c.Flows = append(c.Flows, FlowSummary{ID: x.ID, Flow: x.Flow, State: x.State, Trace: trace})
	}
}

func (c *ContextView) AddTasks(records []any) {
	for _, record := range records {
		x := record.(platform.WorkTask)
		c.Tasks = append(c.Tasks, TaskSummary{ID: x.ID, Title: x.Title, State: x.State, Answer: x.Answer})
	}
}
