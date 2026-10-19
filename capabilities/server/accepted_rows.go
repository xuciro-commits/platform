package platformserver

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// Capture the same full record image and predecessor for connector inputs,
// effect answers and callback observations, in the supplied stable order.
func captureDirectRows(d *stagedDecision, owner string, refs []string, exclude map[string]bool, source string) ([]acceptedInputRow, error) {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	if refs == nil {
		refs = slices.Sorted(maps.Keys(d.records.writes))
	}
	var rows []acceptedInputRow
	for _, ref := range refs {
		if exclude[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.info.App != owner || et.rows.get(id) == nil {
			return nil, fmt.Errorf("%s writes a foreign or undeclared record", source)
		}
		image, err := acceptedRowOf(typ, id, et.rows.get(id))
		if err != nil {
			return nil, err
		}
		row := acceptedInputRow{acceptedRow: image, App: owner}
		if prior := d.records.parent.types[typ]; prior != nil && prior.rows.get(id) != nil {
			old, err := acceptedRowOf(typ, id, prior.rows.get(id))
			if err != nil {
				return nil, err
			}
			row.Before, err = canonicalDigest(old)
			if err != nil {
				return nil, err
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// prepareAcceptedDirectRows verifies and forks record changes caused by a
// top-level input without a child ledger decision (connector facts or an
// external effect's answer). The caller promotes only after all other owners
// of the same result have passed their predecessor checks.
func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedInputRow, member string, at time.Time) (*recordStore, error) {
	draft := t.records.forkRecords()
	draft.mu.Lock()
	defer draft.mu.Unlock()
	for _, image := range rows {
		et := draft.types[image.Type]
		if et == nil || et.info.App != image.App {
			return nil, fmt.Errorf("direct result needs missing record type %s", image.Type)
		}
		prior := et.rows.get(image.ID)
		before, previousHistory, revision := "", 0, uint32(0)
		if prior != nil {
			row, err := acceptedRowOf(image.Type, image.ID, prior)
			if err != nil {
				return nil, err
			}
			before, err = canonicalDigest(row)
			if err != nil {
				return nil, err
			}
			previousHistory, revision = len(prior.recordHistory()), recordOf(prior.value).Revision
		}
		if image.Before != before || len(image.History) <= previousHistory {
			return nil, fmt.Errorf("direct record predecessor differs")
		}
		if prior != nil {
			old, err := canonicalDigest(image.History[:previousHistory])
			actual, actualErr := canonicalDigest(prior.recordHistory())
			if err != nil || actualErr != nil || old != actual {
				return nil, fmt.Errorf("direct record history predecessor differs")
			}
		}
		for _, change := range image.History[previousHistory:] {
			if change.Change != "" || change.Schema == "" || change.By != member || !change.At.Equal(at) {
				return nil, fmt.Errorf("direct record has a foreign change")
			}
		}
		value := reflect.New(et.info.Go).Elem()
		if err := decodeAcceptedJSON(image.Value, value.Addr().Interface(), "direct record"); err != nil {
			return nil, err
		}
		if err := checkRunApplicationBindings(value.Interface()); err != nil {
			return nil, err
		}
		rec := recordOf(value)
		last := image.History[len(image.History)-1]
		if rec.ID != image.ID || rec.Revision != revision || rec.Changed != (platform.Stamp{
			By: member, At: at, Change: ""}) ||
			prior != nil && rec.Created != recordOf(prior.value).Created ||
			prior == nil && rec.Created != (platform.Stamp{By: member,
				At: image.History[0].At, Change: ""}) ||
			last.Schema == "" {
			return nil, fmt.Errorf("direct record image differs from history")
		}
		recovered := &row{value: value, history: copyHistory(image.History)}
		recovered.retainOriginal(image.Value)
		et.setRow(image.ID, recovered)
		draft.writes[image.Type+"/"+image.ID] = true
		if et.knowledge {
			draft.dirty.set(image.Type+"/"+image.ID, true)
		}
	}
	return draft, nil
}
