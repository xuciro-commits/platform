package enterprise

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"strings"
	"time"
)

func pinRecord(c platform.Caller, ref string, now time.Time) (map[string]any, *kernel.Error) {
	typ, id, _ := strings.Cut(strings.TrimPrefix(ref, "record:"), "/")
	raw, err := c.ReadRecord(typ, id, now)
	if err != nil {
		return nil, err
	}
	var row map[string]any
	if json.Unmarshal(raw, &row) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid record")
	}
	return row, nil
}

// Pins are projected through their original record scopes; cached labels never
// grant a reader more access or leave stale names after a business rename.
func readableModel(c platform.Caller, m Model, now time.Time) Model {
	for i := range m.Views {
		pins := []Pin{}
		for _, pin := range m.Views[i].Pins {
			row, err := pinRecord(c, pin.Ref, now)
			if err != nil {
				continue
			}
			pin.Label = refID(pin.Ref)
			for _, key := range []string{"name", "title", "number", "code"} {
				if label, ok := row[key].(string); ok && label != "" {
					pin.Label = label
					break
				}
			}
			pins = append(pins, pin)
		}
		m.Views[i].Pins = pins
	}
	return m
}
