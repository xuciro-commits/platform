package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
)

func checkNamedQuery(q platform.NamedQuery, info platform.EntityInfo) error {
	if err := q.Check(); err != nil {
		return err
	}
	if q.Object != info.Type {
		return fmt.Errorf("query source object is unavailable")
	}
	if _, err := compileDomain(info, q.Domain); err != nil {
		return err
	}
	if q.By != "" {
		f, ok := info.Field(q.By)
		if !ok || f.Type != "reference" {
			return fmt.Errorf("query parent input needs a readable reference")
		}
	}
	for _, key := range q.Sort {
		name := strings.TrimPrefix(key, "-")
		if name == "id" || name == "created" || name == "changed" {
			continue
		}
		if _, ok := info.Field(name); !ok {
			return fmt.Errorf("query sort field %s is unavailable", name)
		}
	}
	return nil
}

func (h hostView) ValidateInstallQuery(q platform.NamedQuery) error {
	info, ok := h.t.entity(q.Object)
	if !ok || info.App != h.app.Manifest().ID {
		return fmt.Errorf("query needs its owner's published object")
	}
	return checkNamedQuery(q, info)
}

func (h hostView) InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("query needs a retained published version")
	}
	if err := h.ValidateInstallQuery(q); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetQuery, Name: q.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".query-" + strconv.Itoa(version), ContractVersion: 1, Requires: []platform.AssetRef{{App: ref.App, Kind: platform.AssetObject, Name: q.Object}}, Query: &q}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		old := h.t.definitions[i]
		if old.Source != "tenant" {
			return fmt.Errorf("query %s is owned by code", ref)
		}
		def.QueryVersions = maps.Clone(old.QueryVersions)
		if def.QueryVersions == nil {
			def.QueryVersions = map[string]platform.NamedQuery{}
		}
		def.QueryVersions[old.Version] = *old.Query
		if retained, ok := def.QueryVersions[def.Version]; ok && !sameQuery(retained, q) {
			return fmt.Errorf("query version bytes cannot change")
		}
		def.QueryVersions[def.Version] = q
		h.t.definitions[i] = def
	} else {
		def.QueryVersions = map[string]platform.NamedQuery{def.Version: q}
		h.t.definitions = append(h.t.definitions, def)
	}
	slices.SortFunc(h.t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

func sameQuery(a, b platform.NamedQuery) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
