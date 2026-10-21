package journal

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
)

// ReplaceStateRecordValue preserves the opaque record envelope and checks the
// exact predecessor sealed by its owner. It never changes or invents history.
func ReplaceStateRecordValue(image json.RawMessage, replacement StateReplacement) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(image, &envelope); err != nil {
		return nil, err
	}
	before, err := acceptance.Digest(envelope["value"])
	if err != nil || before != replacement.Before {
		return nil, fmt.Errorf("installation predecessor differs: %s/%s", replacement.Type, replacement.ID)
	}
	var oldHeader, newHeader struct {
		ID       string `json:"id"`
		Revision uint32 `json:"revision"`
	}
	if json.Unmarshal(envelope["value"], &oldHeader) != nil || json.Unmarshal(replacement.Value, &newHeader) != nil || oldHeader != newHeader || newHeader.ID != replacement.ID {
		return nil, fmt.Errorf("installation record identity differs: %s/%s", replacement.Type, replacement.ID)
	}
	envelope["value"] = replacement.Value
	return json.Marshal(envelope)
}
