package platform

import (
	"fmt"
	"strings"
)

// AssetKind identifies the existing executor behind a code-defined asset.
// More kinds can be added as their canonical platform owner is built.
type AssetKind string

const (
	AssetObject AssetKind = "object"
	AssetAction AssetKind = "action"
)

// AssetRef is the stable identity shared by code and construction surfaces.
// Its three parts avoid collisions between apps and between kinds. A published
// revision will bind this identity to an immutable definition in a later batch.
type AssetRef struct {
	App  string    `json:"app"`
	Kind AssetKind `json:"kind"`
	Name string    `json:"name"`
}

func (r AssetRef) String() string { return r.App + "/" + string(r.Kind) + "/" + r.Name }

func (r AssetRef) Check() error {
	if r.App == "" || r.Name == "" || strings.Contains(r.App, "/") || strings.Contains(r.Name, "/") || r.Kind != AssetObject && r.Kind != AssetAction {
		return fmt.Errorf("asset reference %q needs an app, supported kind and name", r.String())
	}
	return nil
}

// Definition is one installed code asset as the reader may discover it.
// Entity and Action reuse the same descriptions as the existing record/action
// APIs; this registry is an index over those owners, not another executor.
type Definition struct {
	Ref             AssetRef    `json:"ref"`
	Source          string      `json:"source"`  // code, until published definitions exist
	Version         string      `json:"version"` // installed app manifest version, not a published revision
	ContractVersion int         `json:"contractVersion"`
	Requires        []AssetRef  `json:"requires"`
	Entity          *EntityInfo `json:"entity,omitempty"`
	Action          *Action     `json:"action,omitempty"`
}
