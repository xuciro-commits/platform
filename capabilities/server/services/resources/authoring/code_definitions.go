package build

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
)

type CodeDefinitions struct {
	Apps          []platform.App
	Entities      []platform.EntityInfo
	Narrowable    func(info platform.EntityInfo) []string
	CheckSections func(p platform.Page, page platform.EntityInfo) error
	BindInterface func(q platform.NamedQuery) (platform.NamedQuery, error)
	InstallLink   func(l platform.LinkType) error
	NativeFlow    func(owner platform.App, name string, version int) (platform.ReleaseAsset, error)
	CheckObject   func(info platform.EntityInfo) error
	Describe      func(def platform.Definition) (platform.Definition, error)
	Store         func(def platform.Definition)
	Current       func() []platform.Definition
	UniqueRefs    func(refs []platform.AssetRef) []platform.AssetRef
}

func (h CodeDefinitions) Register() error {
	classes := map[string]bool{}
	versions := map[string]string{}
	declarations := CodeDeclarations{DataClasses: classes, NarrowedFields: map[string][]string{}, NativeExecutors: map[string]bool{}}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		versions[manifest.ID] = manifest.Version
		declarations.Manifests = append(declarations.Manifests, manifest)
		_, declarations.NativeExecutors[manifest.ID] = app.(platform.OperationExecutor)
		for _, declaration := range app.Declarations() {
			classes[declaration.GetDataClass()] = true
		}
	}
	objects := map[string]platform.AssetRef{}
	objectInfo := map[string]platform.EntityInfo{}
	for _, info := range h.Entities {
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
		objectInfo[info.Type] = info
		declarations.Entities = append(declarations.Entities, info)
		declarations.NarrowedFields[info.Type] = h.Narrowable(info)
	}
	if err := declarations.Check(); err != nil {
		return err
	}
	seen := map[platform.AssetRef]bool{}
	add := func(def platform.Definition) error {
		if err := def.Ref.Check(); err != nil {
			return err
		}
		if seen[def.Ref] {
			return fmt.Errorf("asset %s is declared twice", def.Ref)
		}
		if def.Flow == nil {
			var err error
			def, err = h.Describe(def)
			if err != nil {
				return err
			}
		}
		seen[def.Ref] = true
		h.Store(def)
		return nil
	}
	for _, info := range h.Entities {
		if err := add(platform.Definition{Ref: objects[info.Type], Source: "code", Version: versions[info.App], ContractVersion: 1,
			Entity: &info}); err != nil {
			return err
		}
	}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		for _, shape := range manifest.Interfaces {
			shape := shape
			if err := shape.Check(manifest.ID); err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetInterface, Name: shape.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Interface: &shape}); err != nil {
				return err
			}
		}
	}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		for _, action := range manifest.Actions.All() {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: action.Schema}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Action: &action}); err != nil {
				return err
			}
		}
	}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		for _, page := range manifest.Pages {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetPage, Name: page.Name}
			if len(page.DataBindings) != 0 {
				if err := h.CheckSections(page, objectInfo[page.Object.Name]); err != nil {
					return err
				}
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Page: &page}); err != nil {
				return err
			}
		}
	}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		for _, p := range manifest.PropertyTypes {
			p := p
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetPropertyType, Name: p.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, PropertyType: &p}); err != nil {
				return err
			}
		}
		for _, v := range manifest.ValueTypes {
			v := v
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetValueType, Name: v.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, ValueType: &v}); err != nil {
				return err
			}
		}
		for _, l := range manifest.LinkTypes {
			if err := h.InstallLink(l); err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetLinkType, Name: l.Name}, Source: "code", Version: manifest.Version, ContractVersion: l.Contract(), LinkType: &l}); err != nil {
				return err
			}
		}
		for _, query := range manifest.Queries {
			q := query
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetQuery, Name: q.Name}
			if q.Interface != "" {
				bound, err := h.BindInterface(q)
				if err != nil {
					return fmt.Errorf("asset %s: %w", ref, err)
				}
				q = bound
				if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1, Query: &q}); err != nil {
					return err
				}
				continue
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Query: &q}); err != nil {
				return err
			}
		}
		for _, function := range manifest.Functions {
			f := function
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetFunction, Name: f.Name}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Function: &f}); err != nil {
				return err
			}
		}
		for _, agent := range manifest.Agents {
			descriptor, err := AgentDescriptor(agent)
			if err != nil {
				return err
			}
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAgent, Name: agent.Name}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1, Agent: &descriptor}); err != nil {
				return err
			}
		}
		for _, operation := range manifest.Operations {
			o := operation
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetCompute, Name: o.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Operation: &o}); err != nil {
				return err
			}
		}
	}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		for _, observation := range manifest.Observations {
			observation := observation
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetObservation, Name: observation.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Observation: &observation}); err != nil {
				return err
			}
		}
		for _, provision := range manifest.Provides {
			protocol, err := ProtocolDescriptor(manifest, provision)
			if err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetProtocol, Name: protocol.Name}, Source: "code", Version: manifest.Version, ContractVersion: protocol.Version, Protocol: &protocol}); err != nil {
				return err
			}
		}
		latest := map[string]platform.Flow{}
		for _, flow := range manifest.Flows {
			if old, ok := latest[flow.Name]; !ok || old.Version < flow.Version {
				latest[flow.Name] = flow
			}
		}
		for _, flow := range latest {
			asset, err := h.NativeFlow(app, flow.Name, flow.Version)
			if err != nil {
				return err
			}
			var descriptor platform.FlowReleaseDescriptor
			if json.Unmarshal(asset.Body, &descriptor) != nil {
				return fmt.Errorf("native flow descriptor is invalid")
			}
			var definition platform.FlowDefinition
			if json.Unmarshal(descriptor.Definition, &definition) != nil {
				return fmt.Errorf("native flow definition is invalid")
			}
			if err := add(platform.Definition{Ref: asset.Ref, Source: "code", Version: asset.SourceVersion, ContractVersion: 1, Requires: asset.Requires, Provides: asset.Provides, Flow: &definition}); err != nil {
				return err
			}
		}
	}
	for _, app := range h.Apps {
		manifest := app.Manifest()
		pages, resources := []string{}, []platform.AssetRef{}
		for _, definition := range h.Current() {
			if definition.Ref.App != manifest.ID || definition.Source != "code" {
				continue
			}
			if definition.Ref.Kind == platform.AssetPage {
				pages = append(pages, definition.Ref.Name)
			} else if definition.Ref.Kind != platform.AssetApp {
				resources = append(resources, definition.Ref)
			}
		}
		declaration := platform.Application{Name: manifest.ID, Title: manifest.Title, Pages: pages, Resources: h.UniqueRefs(resources)}
		if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetApp, Name: manifest.ID}, Source: "code", Version: manifest.Version, ContractVersion: 1, Application: &declaration}); err != nil {
			return err
		}
	}
	for _, info := range h.Entities {
		if err := h.CheckObject(info); err != nil {
			return err
		}
	}
	return nil
}
