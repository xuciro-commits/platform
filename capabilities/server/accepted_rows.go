package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"time"

	"platformserver/platform"
)

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
		prior := et.rows[image.ID]
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
			previousHistory, revision = len(prior.history), recordOf(prior.value).Revision
		}
		if image.Before != before || len(image.History) <= previousHistory {
			return nil, fmt.Errorf("direct record predecessor differs")
		}
		if prior != nil {
			old, err := canonicalDigest(image.History[:previousHistory])
			actual, actualErr := canonicalDigest(prior.history)
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
		decoder := json.NewDecoder(bytes.NewReader(image.Value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(value.Addr().Interface()); err != nil {
			return nil, err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("direct record has trailing data")
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
		et.rows[image.ID] = &row{value: value, history: copyHistory(image.History)}
		draft.writes[image.Type+"/"+image.ID] = true
		if et.knowledge {
			draft.dirty[image.Type+"/"+image.ID] = true
		}
	}
	return draft, nil
}
