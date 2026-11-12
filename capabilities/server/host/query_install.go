package host

import (
	"fmt"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/objects"
	"strconv"
)

func checkNamedQuery(q platform.NamedQuery, info platform.EntityInfo, engine platform.RuleEngine) error {
	return objects.CheckNamedQuery(q, info, engine)
}

func (h hostView) ValidateInstallQuery(q platform.NamedQuery) error {
	if q.Interface != "" {
		_, err := h.t.bindInterfaceQuery(q)
		return err
	}
	info, ok := h.t.Entity(q.Object)
	if !ok || info.App != h.app.Manifest().ID {
		return fmt.Errorf("query needs its owner's published object")
	}
	return checkNamedQuery(q, info, h.t.services.Rules)
}

func (h hostView) InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("query needs a retained published version")
	}
	if q.Interface != "" {
		bound, err := h.t.bindInterfaceQuery(q)
		if err != nil {
			return err
		}
		q = bound
	}
	if err := h.ValidateInstallQuery(q); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetQuery, Name: q.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".query-" + strconv.Itoa(version), ContractVersion: 1, Query: &q}
	def, err := h.t.describer().DescribeDefinition(def)
	if err != nil {
		return err
	}
	i, versions, err := releaseowner.RetainVersion(h.t.definitions, ref, fmt.Sprintf("query %s is owned by code", ref), "query version bytes cannot change", def.Version, q, func(old platform.Definition) (map[string]platform.NamedQuery, *platform.NamedQuery) {
		return old.QueryVersions, old.Query
	})
	if err != nil {
		return err
	}
	def.QueryVersions = versions
	h.t.storeDefinition(i, def)
	return nil
}
