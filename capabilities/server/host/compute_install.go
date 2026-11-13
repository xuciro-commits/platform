package host

import (
	"context"
	"fmt"
	"platformserver/services/functions/runtime"
	"slices"
	"strconv"

	"platformserver/platform"
)

func (h hostView) ValidateInstallOperation(c platform.Caller, o platform.Operation) error {
	if c.Replaying {
		return h.validateOperation(o, platform.NativeRules{})
	}
	return h.validateOperation(o, h.t.services.Rules)
}
func (h hostView) validateOperation(o platform.Operation, engine platform.RuleEngine) error {
	return compute.ValidateOperation(o, h.app, engine, h.RuleQuery, h.t.Entity, func(module string) bool {
		return h.t.files().Exists(context.Background(), artifactKey(h.t.ID, module))
	})
}
func (h hostView) InstallOperation(c platform.Caller, o platform.Operation, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 {
		return fmt.Errorf("operation requires a published owner version")
	}
	var engine platform.RuleEngine = h.t.services.Rules
	if c.Replaying {
		engine = platform.NativeRules{}
	}
	if err := h.validateOperation(o, engine); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetCompute, Name: o.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".compute-" + strconv.Itoa(version), ContractVersion: 1, Operation: &o}
	def, err := h.t.describer().DescribeDefinition(def)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 && h.t.definitions[i].Source != "tenant" {
		return fmt.Errorf("operation %s is owned by code", o.Name)
	}
	h.t.storeDefinition(i, def)
	return nil
}
