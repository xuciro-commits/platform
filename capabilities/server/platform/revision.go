package platform

import "encoding/json"

type ReleaseAsset struct {
	Ref             AssetRef        `json:"ref"`
	ContractVersion int             `json:"contractVersion"`
	SourceVersion   string          `json:"sourceVersion"`
	Requires        []AssetRef      `json:"requires"`
	Provides        []Capability    `json:"provides,omitempty"`
	Body            json.RawMessage `json:"body"`
}

type FlowReleaseDescriptor struct {
	Native       bool            `json:"native,omitempty"`
	Name         string          `json:"name"`
	Subject      AssetRef        `json:"subject"`
	Actions      []AssetRef      `json:"actions"`
	Definition   json.RawMessage `json:"definition"`
	Functions    []AssetBinding  `json:"functions,omitempty"`
	Operations   []AssetBinding  `json:"operations,omitempty"`
	Queries      []AssetBinding  `json:"queries,omitempty"`
	Dependencies []AssetRef      `json:"dependencies,omitempty"`
}

type AssetBinding struct {
	Ref           AssetRef `json:"ref"`
	SourceVersion string   `json:"sourceVersion"`
}

type ReleaseCandidate struct {
	ID     string
	Bytes  []byte
	Assets []ReleaseAsset
}
