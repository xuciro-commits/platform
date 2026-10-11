package buildapi

import (
	"encoding/json"
)

// ReleasePublication changes only publication metadata beside the mutable draft.
// The host commits these images with the release pointer in one release-result.
type ReleasePublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}
