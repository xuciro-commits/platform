package acceptedformat

import (
	"fmt"
	"time"

	"platformserver/platform"
)

type CompositionRequest struct {
	Selection      map[string]string `json:"selection"`
	Reason         string            `json:"reason"`
	At             time.Time         `json:"at"`
	DisabledOwners []string          `json:"disabledOwners,omitempty"`
}

func DecodeControl(raw []byte, v any) error {
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return fmt.Errorf("control image is empty or too large")
	}
	return platform.DecodeJSON(raw, v, "control image")
}
