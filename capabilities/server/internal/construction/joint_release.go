package construction

import (
	"encoding/json"

	"platformserver/platform"
)

const JointDraftLimit = 128

// JointDraftRef names one saved draft in a joint candidate.
type JointDraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}

// DraftChange reads an owner declaration, not additional publication state.
type DraftChange struct {
	Ref        platform.AssetRef  `json:"ref"`
	Type       string             `json:"type"`
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Variant    string             `json:"variant,omitempty"`
	Published  bool               `json:"published"`
	Changed    platform.Stamp     `json:"changed"`
	Fields     []DraftChangeField `json:"fields"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

// Lists carry names only; the selected draft's read includes field values.
type DraftChangeField struct {
	Name   string          `json:"name"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}
