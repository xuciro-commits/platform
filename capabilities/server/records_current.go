package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"platformserver/internal/records"
	"platformserver/journal"
)

// currentRows resolves committed records through the coordinator's fixed SQL
// view. The collection itself retains only private replacements, never fetched
// values or a tenant-wide copy of the current table.
type currentRows struct {
	store *recordStore
	typ   string
}

func (r *currentRows) view() journal.StateReadView {
	if r.store.readView == nil {
		panic("SQL record read has no consistent view")
	}
	return r.store.readView
}

func (r *currentRows) Get(id string) *records.Row {
	view := r.view()
	current, err := view.StateCurrent(context.Background(), r.store.tenant, r.typ, id)
	if err != nil {
		panic(fmt.Errorf("record %s/%s: %w", r.typ, id, err))
	}
	if len(current.Image) == 0 {
		return nil
	}
	var image recordState
	if err := json.Unmarshal(current.Image, &image); err != nil {
		panic(err)
	}
	et := r.store.types[r.typ]
	decoded, err := records.DecodeFormattedImage(et.info, image.Value, image.Projection, r.store.readRelease, image.Format)
	if err != nil {
		panic(err)
	}
	value := reflect.New(et.info.Go).Elem()
	if err := json.Unmarshal(decoded, value.Addr().Interface()); err != nil {
		panic(err)
	}
	records.EmptyLists(value)
	if records.Header(value).ID != id {
		panic("SQL current record identity differs")
	}
	row := &records.Row{Value: value, Format: image.Format, HistoryCount: current.HistoryCount, HistoryHead: current.HistoryHead, Projection: image.Projection, Extraction: image.Extraction}
	row.RetainOriginal(image.Value)
	count := current.HistoryCount
	row.LoadHistory = func(after int) []records.RecordChange {
		list, err := view.StateHistory(context.Background(), r.store.tenant, r.typ, id, after, count)
		if err != nil {
			panic(fmt.Errorf("record history %s/%s: %w", r.typ, id, err))
		}
		out := make([]records.RecordChange, len(list))
		for i, raw := range list {
			if err := json.Unmarshal(raw, &out[i]); err != nil {
				panic(err)
			}
		}
		return out
	}
	return row
}

func (r *currentRows) IDs(after string, limit int) []string {
	ids, err := r.view().StateCandidates(context.Background(), r.store.tenant, r.typ, nil, after, limit, r.store.appliedPosition)
	if err != nil {
		panic(fmt.Errorf("record candidates: %w", err))
	}
	return ids
}

// useCurrentRows is called only after the existing images have been checked
// against SQL, or after the complete accepted changes have been applied there.
// A private decision keeps its immutable overlay until its parent accepts it.
func (s *recordStore) useCurrentRows() {
	for typ, et := range s.types {
		et.rows = records.SourceRows(&currentRows{store: s, typ: typ})
		et.referenceIndexes = nil
		et.uniqueIndexes = nil
		et.fullImage = false
	}
}

// bindReadView does not own or close the view: the tenant coordinator keeps
// its lifetime shared by nested private decisions and their owner reads.
func (s *recordStore) bindReadView(view journal.StateReadView) { s.readView = view }

// Legacy reconstruction still reads its original store until current rows are
// admitted. Serving reads and private decisions use the coordinator's view.
func (s *recordStore) stateReader() interface {
	StateCandidates(context.Context, string, string, []journal.StateFilter, string, int, int64) ([]string, error)
} {
	if s.readView != nil {
		return s.readView
	}
	return s.historyStore
}
