package platformserver

import (
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// Capture the same full record image and predecessor for connector inputs,
// effect answers and callback observations, in the supplied stable order.
func captureDirectRows(d *stagedDecision, owner string, refs []string, exclude map[string]bool, source string) ([]resultformat.InputRow, error) {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	if refs == nil {
		refs = slices.Sorted(maps.Keys(d.records.writes))
	}
	var rows []resultformat.InputRow
	for _, ref := range refs {
		if exclude[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.info.App != owner || et.rows.Get(id) == nil {
			return nil, fmt.Errorf("%s writes a foreign or undeclared record", source)
		}
		var prior *records.Row
		if old := d.records.parent.types[typ]; old != nil {
			prior = old.rows.Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.rows.Get(id), prior)
		if err != nil {
			return nil, err
		}
		row := resultformat.InputRow{Image: image, App: owner}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// prepareAcceptedDirectRows verifies and forks record changes caused by a
// top-level input without a child ledger decision (connector facts or an
// external effect's answer). The caller promotes only after all other owners
// of the same result have passed their predecessor checks.
func (t *Tenant) prepareAcceptedDirectRows(rows []resultformat.InputRow, member string, at time.Time) (*recordStore, error) {
	return t.prepareAcceptedDirectRowsOn(t.records, rows, member, at)
}

func (t *Tenant) prepareAcceptedDirectRowsOn(base *recordStore, rows []resultformat.InputRow, member string, at time.Time) (*recordStore, error) {
	draft := base.forkRecords()
	draft.mu.Lock()
	defer draft.mu.Unlock()
	for _, image := range rows {
		et := draft.types[image.Type]
		if et == nil || et.info.App != image.App {
			return nil, fmt.Errorf("direct result needs missing record type %s", image.Type)
		}
		prior := et.rows.Get(image.ID)
		before, previousHistory, revision := "", 0, uint32(0)
		if image.HistoryRef != nil {
			if err := records.CheckHistoryPredecessor(image.Image, prior); err != nil {
				return nil, err
			}
			var err error
			before, err = records.Predecessor(image.Type, image.ID, prior)
			if err != nil {
				return nil, err
			}
			if before != image.Before {
				return nil, fmt.Errorf("direct record image predecessor differs")
			}
			if prior != nil {
				revision = records.Header(prior.Value).Revision
			}
		} else {
			if prior != nil {
				row, err := records.FullImage(image.Type, image.ID, prior)
				if err != nil {
					return nil, err
				}
				before, err = acceptance.Digest(row)
				if err != nil {
					return nil, err
				}
				previousHistory, revision = len(prior.RecordHistory()), records.Header(prior.Value).Revision
			}
			if image.Before != before || len(image.History) <= previousHistory {
				return nil, fmt.Errorf("direct record predecessor differs")
			}
			if prior != nil {
				old, err := acceptance.Digest(image.History[:previousHistory])
				actual, actualErr := acceptance.Digest(prior.RecordHistory())
				if err != nil || actualErr != nil || old != actual {
					return nil, fmt.Errorf("direct record history predecessor differs")
				}
			}
		}
		for _, change := range image.History[previousHistory:] {
			if change.Change != "" || change.Schema == "" || change.By != member || !change.At.Equal(at) {
				return nil, fmt.Errorf("direct record has a foreign change")
			}
		}
		value := reflect.New(et.info.Go).Elem()
		decode, err := records.DecodeFormattedImage(et.info, image.Value, image.Projection, draft.readRelease, image.Format)
		if err != nil {
			return nil, err
		}
		if err := acceptance.DecodeJSON(decode, value.Addr().Interface(), "direct record"); err != nil {
			return nil, err
		}
		if err := checkRunApplicationBindings(value.Interface()); err != nil {
			return nil, err
		}
		rec := records.Header(value)
		last := image.History[len(image.History)-1]
		if rec.ID != image.ID || rec.Revision != revision || rec.Changed != (platform.Stamp{
			By: member, At: at, Change: ""}) ||
			prior != nil && rec.Created != records.Header(prior.Value).Created ||
			prior == nil && rec.Created != (platform.Stamp{By: member,
				At: image.History[0].At, Change: ""}) ||
			last.Schema == "" {
			return nil, fmt.Errorf("direct record image differs from history")
		}
		recovered := &records.Row{Value: value, Format: image.Format, History: records.CopyHistory(image.History), Projection: records.CloneProjection(image.Projection), Extraction: image.Extraction}
		if image.HistoryRef != nil {
			if err := records.AppendRowHistory(recovered, prior, image.History); err != nil {
				return nil, err
			}
		}
		recovered.RetainOriginal(image.Value)
		et.setRow(image.ID, recovered)
		draft.writes[image.Type+"/"+image.ID] = true
		if et.knowledge {
			draft.dirty.Set(image.Type+"/"+image.ID, true)
		}
	}
	return draft, nil
}
