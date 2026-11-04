package host

import (
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

func (h hostView) ValidateInstallInterface(i platform.Interface) error {
	if err := i.Check(h.app.Manifest().ID); err != nil {
		return err
	}
	for _, et := range h.t.records.SortedTypes() {
		info := et.Info()
		if slices.Contains(info.Implements, i.Name) {
			if err := i.Implements(info); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h hostView) InstallInterface(c platform.Caller, i platform.Interface, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if err := h.ValidateInstallInterface(i); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("interface needs a retained version")
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetInterface, Name: i.Name}
	definition := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".interface-" + strconv.Itoa(version), ContractVersion: 1, Interface: &i}
	definition, err := h.t.describeDefinition(definition)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if index >= 0 && h.t.definitions[index].Source != "tenant" {
		return fmt.Errorf("interface %s is owned by code", ref)
	}
	h.t.storeDefinition(index, definition)
	return nil
}
