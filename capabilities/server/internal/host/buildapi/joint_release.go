package buildapi

import (
	"platformserver/platform"
)

// JointDraftRef names one saved draft in a joint candidate (ADR-0048 D1).
type JointDraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}
