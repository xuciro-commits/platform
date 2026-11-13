package ontology

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"platformserver/platform"
)

type DefinitionInstall struct {
	Manifest    func() platform.Manifest
	Definitions func() []platform.Definition
	Describe    func(platform.Definition) (platform.Definition, error)
	Store       func(int, platform.Definition)
	Entity      func(string) (platform.EntityInfo, bool)
	Types       func() []platform.EntityInfo
	Bind        func(platform.NamedQuery) (platform.NamedQuery, error)
	Query       func(platform.NamedQuery, platform.EntityInfo) error
	Link        func(platform.LinkType) error
	Constrain   func(platform.LinkType) error
}

func RetainedDefinitionVersions[T any](history map[string]T, previousVersion string, previous *T, version string, current T) (map[string]T, bool) {
	versions := maps.Clone(history)
	if versions == nil {
		versions = map[string]T{}
	}
	if previous != nil {
		versions[previousVersion] = *previous
	}
	if prior, ok := versions[version]; ok && string(platform.Raw(prior)) != string(platform.Raw(current)) {
		return nil, false
	}
	return versions, true
}

func RetainVersion[T any](definitions []platform.Definition, ref platform.AssetRef, owned, changed, version string, current T, history func(platform.Definition) (map[string]T, *T)) (int, map[string]T, error) {
	i := slices.IndexFunc(definitions, func(d platform.Definition) bool { return d.Ref == ref })
	versions := map[string]T{}
	if i >= 0 {
		old := definitions[i]
		if old.Source != "tenant" {
			return i, nil, errors.New(owned)
		}
		past, previous := history(old)
		var ok bool
		if versions, ok = RetainedDefinitionVersions(past, old.Version, previous, version, current); !ok {
			return i, nil, errors.New(changed)
		}
	}
	versions[version] = current
	return i, versions, nil
}

func retain[T any](in DefinitionInstall, def platform.Definition, owned, changed string, current T, history func(platform.Definition) (map[string]T, *T), set func(*platform.Definition, map[string]T) error) error {
	def, err := in.Describe(def)
	if err != nil {
		return err
	}
	i, versions, err := RetainVersion(in.Definitions(), def.Ref, owned, changed, def.Version, current, history)
	if err != nil {
		return err
	}
	if err := set(&def, versions); err != nil {
		return err
	}
	in.Store(i, def)
	return nil
}

func (in DefinitionInstall) definition(kind platform.AssetKind, name, suffix string, version, contract int) platform.Definition {
	m := in.Manifest()
	return platform.Definition{Ref: platform.AssetRef{App: m.ID, Kind: kind, Name: name}, Source: "tenant", Version: m.Version + "." + suffix + "-" + strconv.Itoa(version), ContractVersion: contract}
}

func (in DefinitionInstall) PropertyType(p platform.PropertyType, version int) error {
	if err := p.Check(); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("property type needs a retained version")
	}
	d := in.definition(platform.AssetPropertyType, p.Name, "property", version, 1)
	d.PropertyType, d.PropertyVersions = &p, map[string]platform.PropertyType{}
	return retain(in, d, "property type is owned by code", "property version bytes cannot change", p, func(old platform.Definition) (map[string]platform.PropertyType, *platform.PropertyType) {
		return old.PropertyVersions, old.PropertyType
	}, func(d *platform.Definition, versions map[string]platform.PropertyType) error {
		d.PropertyVersions = versions
		return nil
	})
}

func (in DefinitionInstall) ValueType(v platform.ValueType, version int) error {
	if err := v.Check(); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("value type needs a retained version")
	}
	d := in.definition(platform.AssetValueType, v.Name, "value", version, 1)
	d.ValueType, d.ValueVersions = &v, map[string]platform.ValueType{}
	return retain(in, d, "value type is owned by code", "value type version bytes cannot change", v, func(old platform.Definition) (map[string]platform.ValueType, *platform.ValueType) {
		return old.ValueVersions, old.ValueType
	}, func(d *platform.Definition, versions map[string]platform.ValueType) error {
		d.ValueVersions = versions
		return nil
	})
}

func (in DefinitionInstall) ValidateLinkType(l platform.LinkType) error {
	parent, ok := in.Entity(l.Parent.Name)
	if !ok {
		return fmt.Errorf("link parent is unavailable")
	}
	child, ok := in.Entity(l.Child.Name)
	if !ok || child.App != in.Manifest().ID {
		return fmt.Errorf("link type needs its child owner's object")
	}
	if err := l.CheckSchema(parent, child); err != nil {
		return err
	}
	return in.Link(l)
}

func (in DefinitionInstall) LinkType(l platform.LinkType, version int) error {
	if version < 1 || version > 64 {
		return fmt.Errorf("link type needs a retained version")
	}
	if err := in.ValidateLinkType(l); err != nil {
		return err
	}
	d := in.definition(platform.AssetLinkType, l.Name, "link", version, l.Contract())
	d.LinkType, d.LinkVersions = &l, map[string]platform.LinkType{}
	return retain(in, d, "link type is owned by code", "link version bytes cannot change", l, func(old platform.Definition) (map[string]platform.LinkType, *platform.LinkType) {
		return old.LinkVersions, old.LinkType
	}, func(d *platform.Definition, versions map[string]platform.LinkType) error {
		if err := in.Constrain(l); err != nil {
			return err
		}
		d.LinkVersions = versions
		return nil
	})
}

func (in DefinitionInstall) ValidateQuery(q platform.NamedQuery) error {
	if q.Interface != "" {
		_, err := in.Bind(q)
		return err
	}
	info, ok := in.Entity(q.Object)
	if !ok || info.App != in.Manifest().ID {
		return fmt.Errorf("query needs its owner's published object")
	}
	return in.Query(q, info)
}

func (in DefinitionInstall) NamedQuery(q platform.NamedQuery, version int) error {
	if version < 1 || version > 64 {
		return fmt.Errorf("query needs a retained published version")
	}
	if q.Interface != "" {
		bound, err := in.Bind(q)
		if err != nil {
			return err
		}
		q = bound
	}
	if err := in.ValidateQuery(q); err != nil {
		return err
	}
	d := in.definition(platform.AssetQuery, q.Name, "query", version, 1)
	d.Query = &q
	return retain(in, d, fmt.Sprintf("query %s is owned by code", d.Ref), "query version bytes cannot change", q, func(old platform.Definition) (map[string]platform.NamedQuery, *platform.NamedQuery) {
		return old.QueryVersions, old.Query
	}, func(d *platform.Definition, versions map[string]platform.NamedQuery) error {
		d.QueryVersions = versions
		return nil
	})
}

func (in DefinitionInstall) storeOwned(def platform.Definition, owned string) error {
	def, err := in.Describe(def)
	if err != nil {
		return err
	}
	definitions := in.Definitions()
	index := slices.IndexFunc(definitions, func(d platform.Definition) bool { return d.Ref == def.Ref })
	if index >= 0 && definitions[index].Source != "tenant" {
		return errors.New(owned)
	}
	in.Store(index, def)
	return nil
}

func (in DefinitionInstall) ValidateInterface(i platform.Interface) error {
	if err := i.Check(in.Manifest().ID); err != nil {
		return err
	}
	for _, info := range in.Types() {
		if slices.Contains(info.Implements, i.Name) {
			if err := i.Implements(info); err != nil {
				return err
			}
		}
	}
	return nil
}

func (in DefinitionInstall) Interface(i platform.Interface, version int) error {
	if err := in.ValidateInterface(i); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("interface needs a retained version")
	}
	d := in.definition(platform.AssetInterface, i.Name, "interface", version, 1)
	d.Interface = &i
	return in.storeOwned(d, fmt.Sprintf("interface %s is owned by code", d.Ref))
}

func (in DefinitionInstall) ValidateObjectTypeGroup(g platform.ObjectTypeGroup) error {
	if err := g.Check(); err != nil {
		return err
	}
	for _, member := range g.Members {
		info, ok := in.Entity(member.Name)
		if !ok || info.App != member.App {
			return fmt.Errorf("Object type group member %s needs a published object type", member)
		}
	}
	return nil
}

func (in DefinitionInstall) ObjectTypeGroup(g platform.ObjectTypeGroup, version int) error {
	if err := in.ValidateObjectTypeGroup(g); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("Object type group needs a retained published version")
	}
	d := in.definition(platform.AssetObjectTypeGroup, g.Name, "objecttypegroup", version, 1)
	d.ObjectTypeGroup = &g
	return in.storeOwned(d, "Object type group is owned by code")
}

func CheckObjectBindings(info platform.EntityInfo, property func(platform.AssetBinding) (platform.PropertyType, bool), value func(platform.AssetBinding) (platform.ValueType, bool)) error {
	for _, f := range info.Fields {
		if f.Property == nil {
			continue
		}
		p, ok := property(*f.Property)
		if !ok {
			return fmt.Errorf("field %s has an unavailable property version", f.Name)
		}
		if err := p.CheckField(f); err != nil {
			return err
		}
	}
	for _, f := range info.Fields {
		if f.ValueType == nil {
			continue
		}
		v, ok := value(*f.ValueType)
		if !ok {
			return fmt.Errorf("field %s has an unavailable value type version", f.Name)
		}
		if err := v.CheckField(f); err != nil {
			return err
		}
	}
	return nil
}
