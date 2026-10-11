package platformserver

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// Callback observations have no new kernel change receipt. Their authority is
// the host-delivered, already accepted effect and the exact owner callback,
// with the same direct-row preimage/history checks as connector/effect results.
type acceptedCallbackRow struct {
	Row    acceptedInputRow `json:"row"`
	Source platform.Effect  `json:"source"`
}
type callbackWrite struct {
	Source  platform.Effect
	History int
}

func (d *stagedDecision) savedCallbackRows(published map[string]bool) ([]acceptedCallbackRow, error) {
	var rows []acceptedCallbackRow
	for _, ref := range slices.Sorted(maps.Keys(d.records.writes)) {
		if published[ref] {
			continue
		}
		callback, ok := d.callbackWrites[ref]
		if !ok {
			return nil, fmt.Errorf("batch event omits a written record outside an effect callback")
		}
		direct, err := captureDirectEffectRows(d, callback.Source.App, publishedOnly(d.records.writes, ref))
		if err != nil || len(direct) != 1 {
			return nil, fmt.Errorf("callback row cannot be captured: %v", err)
		}
		if len(direct[0].History) != callback.History {
			return nil, fmt.Errorf("callback row has a write outside its source callback")
		}
		if _, err := d.tenant.prepareAcceptedDirectRows(direct, "app:"+callback.Source.App, d.at); err != nil {
			return nil, err
		}
		rows = append(rows, acceptedCallbackRow{Row: direct[0], Source: callback.Source})
	}
	return rows, nil
}

func publishedOnly(writes map[string]bool, ref string) map[string]bool {
	ignored := maps.Clone(writes)
	delete(ignored, ref)
	return ignored
}

// Validate sources and direct rows in private views before any ledger or
// business/control state in the containing result is promoted.
func (t *Tenant) prepareCallbackRows(target *recordStore, rows []acceptedCallbackRow, at time.Time) error {
	seen := map[string]bool{}
	for _, callback := range rows {
		row, source := callback.Row, callback.Source
		ref := row.Type + "/" + row.ID
		if seen[ref] || row.App == "" || source.ID == "" || source.App != row.App {
			return fmt.Errorf("callback row has an invalid source or owner")
		}
		seen[ref] = true
		if _, ok := t.app(row.App).(platform.Answerer); !ok {
			return fmt.Errorf("callback row has no effect answer owner")
		}
		t.opsMu.Lock()
		var current platform.Effect
		for _, effect := range t.outbound {
			if effect.ID == source.ID {
				current = effect.Effect
				break
			}
		}
		t.opsMu.Unlock()
		actual, err := canonicalDigest(current)
		expected, expectedErr := canonicalDigest(source)
		if current.ID == "" || err != nil || expectedErr != nil || actual != expected {
			return fmt.Errorf("callback row source effect predecessor differs")
		}
		private, err := t.prepareAcceptedDirectRows([]acceptedInputRow{row}, "app:"+row.App, at)
		if err != nil {
			return err
		}
		typ, id, _ := strings.Cut(ref, "/")
		private.mu.Lock()
		image := private.types[typ].rows[id]
		private.mu.Unlock()
		target.mu.Lock()
		target.types[typ].rows[id] = image
		target.writes[ref] = true
		if target.types[typ].knowledge {
			target.dirty[ref] = true
		}
		target.mu.Unlock()
	}
	return nil
}
