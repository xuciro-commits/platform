package platformserver

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func (h hostView) ValidateInstallOperation(o platform.Operation) error {
	if err := o.Check(); err != nil {
		return err
	}
	for _, role := range o.Roles {
		if !slices.Contains(h.app.Manifest().AllRoles(), role) {
			return fmt.Errorf("operation %s names an undeclared role", o.Name)
		}
	}
	if o.Binding.Kind == "wasm" && !h.t.files().Exists(context.Background(), artifactKey(h.t.ID, o.Binding.Module)) {
		return fmt.Errorf("operation %s has no retained module", o.Name)
	}
	if o.Binding.Kind == "native" {
		if _, ok := h.app.(platform.OperationExecutor); !ok {
			return fmt.Errorf("operation %s has no native executor", o.Name)
		}
	}
	return nil
}
func (h hostView) InstallOperation(c platform.Caller, o platform.Operation, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 {
		return fmt.Errorf("operation requires a published owner version")
	}
	if err := h.ValidateInstallOperation(o); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetCompute, Name: o.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".compute-" + strconv.Itoa(version), ContractVersion: 1, Requires: []platform.AssetRef{}, Operation: &o}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		if h.t.definitions[i].Source != "tenant" {
			return fmt.Errorf("operation %s is owned by code", o.Name)
		}
		h.t.definitions[i] = def
	} else {
		h.t.definitions = append(h.t.definitions, def)
	}
	slices.SortFunc(h.t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}
