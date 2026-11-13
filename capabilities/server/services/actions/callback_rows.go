package actions

import (
	"fmt"
	"strings"
	"time"

	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type CallbackRows struct {
	Answerer func(app string) bool
	Pending  func(id string) platform.Effect
	Prepare  func(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error)
}

func (c CallbackRows) Apply(target *records.Store, rows []acceptedformat.CallbackRow, at time.Time) error {
	seen := map[string]bool{}
	for _, callback := range rows {
		row, source := callback.Row, callback.Source
		ref := row.Type + "/" + row.ID
		if seen[ref] || row.App == "" || source.ID == "" || source.App != row.App {
			return fmt.Errorf("callback row has an invalid source or owner")
		}
		seen[ref] = true
		if !c.Answerer(row.App) {
			return fmt.Errorf("callback row has no effect answer owner")
		}
		current := c.Pending(source.ID)
		actual, err := platform.CanonicalDigest(current)
		expected, expectedErr := platform.CanonicalDigest(source)
		if current.ID == "" || err != nil || expectedErr != nil || actual != expected {
			return fmt.Errorf("callback row source effect predecessor differs")
		}
		private, err := c.Prepare([]acceptedformat.InputRow{row}, "app:"+row.App, at)
		if err != nil {
			return err
		}
		typ, id, _ := strings.Cut(ref, "/")
		private.Lock()
		image := private.Type(typ).Rows().Get(id)
		private.Unlock()
		target.Lock()
		target.Type(typ).SetRow(id, image)
		target.TrackWrite(ref)
		if target.Type(typ).Knowledge() {
			target.MarkDirty(ref)
		}
		target.Unlock()
	}
	return nil
}
