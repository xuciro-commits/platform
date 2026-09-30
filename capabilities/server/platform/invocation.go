package platform

import "encoding/json"

// CapabilityInvocation selects an existing owner entry. It does not grant a
// second execution authority or allow the caller to replace its identity.
type CapabilityInvocation struct {
	Ref              AssetRef           `json:"ref"`
	Key              string             `json:"key"`
	Inputs           json.RawMessage    `json:"inputs"`
	Target           string             `json:"target,omitempty"`
	Version          int                `json:"version,omitempty"`
	ExpectedRevision *uint32            `json:"expectedRevision,omitempty"`
	Sources          []string           `json:"sources,omitempty"`
	Record           string             `json:"record,omitempty"`
	Bindings         map[string]Binding `json:"bindings,omitempty"`
}

type CapabilityResult struct {
	Ref    AssetRef        `json:"ref"`
	State  string          `json:"state"`
	Call   string          `json:"call,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}
