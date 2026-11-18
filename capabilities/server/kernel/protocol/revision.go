package protocol

import (
	"encoding/json"
	"platformserver/platform"
)

type FlowReleaseDescriptor struct {
	Native       bool                    `json:"native,omitempty"`
	Name         string                  `json:"name"`
	Subject      platform.AssetRef       `json:"subject"`
	Actions      []platform.AssetRef     `json:"actions"`
	Definition   json.RawMessage         `json:"definition"`
	Functions    []platform.AssetBinding `json:"functions,omitempty"`
	Operations   []platform.AssetBinding `json:"operations,omitempty"`
	Queries      []platform.AssetBinding `json:"queries,omitempty"`
	Dependencies []platform.AssetRef     `json:"dependencies,omitempty"`
}

type ReleaseCandidate struct {
	ID     string
	Bytes  []byte
	Assets []platform.ReleaseAsset
}
