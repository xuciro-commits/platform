package host

import (
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

func (h hostView) ValidateInstallObjectTypeGroup(g platform.ObjectTypeGroup) error {
	if err := g.Check(); err != nil {
		return err
	}
	for _, member := range g.Members {
		info, ok := h.Entity(member.Name)
		if !ok || info.App != member.App {
			return fmt.Errorf("Object type group member %s needs a published object type", member)
		}
	}
	return nil
}

func (h hostView) InstallObjectTypeGroup(c platform.Caller, g platform.ObjectTypeGroup, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if err := h.ValidateInstallObjectTypeGroup(g); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("Object type group needs a retained published version")
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetObjectTypeGroup, Name: g.Name}
	definition := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".objecttypegroup-" + strconv.Itoa(version), ContractVersion: 1, ObjectTypeGroup: &g}
	definition, err := h.t.describeDefinition(definition)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if index >= 0 && h.t.definitions[index].Source != "tenant" {
		return fmt.Errorf("Object type group is owned by code")
	}
	h.t.storeDefinition(index, definition)
	return nil
}
