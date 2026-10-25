package platform

import (
	"encoding/json"
	"fmt"
	"time"

	"platformkernel/kernel"
)

// SystemDefinitionHost reuses the ordinary release owner inside a system
// decision. Preparation is private; application consumes only accepted bytes.
type SystemDefinitionHost interface {
	FreezeSystemDefinitions(Caller, []AssetRef, time.Time) (json.RawMessage, *kernel.Error)
	PrepareSystemDefinitions(Caller, json.RawMessage, string, time.Time) (json.RawMessage, *kernel.Error)
	ValidateSystemDefinitions(json.RawMessage) error
	ApplySystemDefinitions(json.RawMessage) error
}

type AttachedSystemDefinitions interface {
	AttachSystemDefinitions(SystemDefinitionHost)
}

// SystemDefinitionCandidate binds a source change to exact owner-produced
// candidate bytes. Subsequent edits of its drafts cannot change this selection.
type SystemDefinitionCandidate struct {
	ID    string `json:"id"`
	Bytes []byte `json:"bytes"`
}

// Earlier system records embedded a candidate as JSON. SQL value projections
// may reorder that JSON; reconstruct it with the same historical candidate
// owner and require its original identity. New records always keep opaque bytes.
func (saved *SystemDefinitionCandidate) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ID    string          `json:"id"`
		Bytes json.RawMessage `json:"bytes"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	saved.ID = wire.ID
	if len(wire.Bytes) > 0 && wire.Bytes[0] == '"' {
		return json.Unmarshal(wire.Bytes, &saved.Bytes)
	}
	var legacy releaseBytes
	if err := json.Unmarshal(wire.Bytes, &legacy); err != nil {
		return err
	}
	if legacy.Format != 1 {
		return fmt.Errorf("unsupported retained system candidate")
	}
	roots := make([]AssetRef, len(legacy.Assets))
	for i, asset := range legacy.Assets {
		roots[i] = asset.Ref
	}
	checked, err := candidate(roots, legacy.Assets, true)
	if err != nil {
		return err
	}
	if checked.ID != wire.ID {
		return fmt.Errorf("retained system candidate identity differs")
	}
	saved.Bytes = checked.Bytes
	return nil
}
