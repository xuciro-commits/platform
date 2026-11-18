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

type AssetBinding struct {
	Ref           AssetRef `json:"ref"`
	SourceVersion string   `json:"sourceVersion"`
}
