package objects

import (
	"encoding/json"

	"platformkernel/kernel"
	"platformserver/platform"
)

type ContextReader struct {
	Detail   func(sections ...string) (RecordView, *kernel.Error)
	Info     func() platform.EntityInfo
	Links    func(entity string) []string
	Readable func(ref string) bool
	Flows    func(q platform.Query) ([]any, bool)
	Tasks    func(q platform.Query) ([]any, bool)
}

func (r ContextReader) Read(typ, id string) (ContextView, *kernel.Error) {
	view, err := r.Detail(RecordSectionHistory, RecordSectionLinks)
	if err != nil {
		return ContextView{}, err
	}
	out := RecordContext(typ, view.Record, view.History, view.Related, r.Info())
	for _, ref := range r.Links(typ + "/" + id) {
		if r.Readable(ref) {
			out.Links = append(out.Links, ref)
		}
	}
	key, _ := json.Marshal([]any{[]any{"key", "=", id}})
	if instances, ok := r.Flows(platform.Query{Domain: key, Sort: []string{"id"}}); ok {
		out.AddFlows(instances)
	}
	about, _ := json.Marshal([]any{[]any{"ref", "=", typ + "/" + id}})
	if tasks, ok := r.Tasks(platform.Query{Domain: about, Sort: []string{"id"}}); ok {
		out.AddTasks(tasks)
	}
	return out, nil
}
