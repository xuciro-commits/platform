package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/transport"
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) registerDefinitions() error {
	classes := map[string]bool{}
	versions := map[string]string{}
	declarations := build.CodeDeclarations{DataClasses: classes, NarrowedFields: map[string][]string{}, NativeExecutors: map[string]bool{}}
	for _, app := range t.apps {
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
	for _, et := range t.records.SortedTypes() {
		info := et.Info()
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
		objectInfo[info.Type] = info
		declarations.Entities = append(declarations.Entities, info)
		declarations.NarrowedFields[info.Type] = t.narrowable(et)
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
			def, err = t.describeDefinition(def)
			if err != nil {
				return err
			}
		}
		seen[def.Ref] = true
		t.setDefinitions(append(t.definitions, def))
		return nil
	}
	for _, et := range t.records.SortedTypes() {
		info := et.Info()
		if err := add(platform.Definition{Ref: objects[info.Type], Source: "code", Version: versions[info.App], ContractVersion: 1,
			Entity: &info}); err != nil {
			return err
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, action := range manifest.Actions.All() {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: action.Schema}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Action: &action}); err != nil {
				return err
			}
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, page := range manifest.Pages {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetPage, Name: page.Name}
			if len(page.DataBindings) != 0 || page.Format == 0 {
				if err := t.checkSections(page, objectInfo[page.Object.Name]); err != nil {
					return err
				}
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Page: &page}); err != nil {
				return err
			}
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, p := range manifest.PropertyTypes {
			p := p
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetPropertyType, Name: p.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, PropertyType: &p}); err != nil {
				return err
			}
		}
		for _, l := range manifest.LinkTypes {
			if err := t.records.InstallLinkConstraint(l); err != nil {
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
				bound, err := t.bindInterfaceQuery(q)
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
			descriptor, err := build.AgentDescriptor(agent)
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
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, observation := range manifest.Observations {
			observation := observation
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetObservation, Name: observation.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Observation: &observation}); err != nil {
				return err
			}
		}
		for _, shape := range manifest.Interfaces {
			shape := shape
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetInterface, Name: shape.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Interface: &shape}); err != nil {
				return err
			}
		}
		for _, provision := range manifest.Provides {
			protocol, err := build.ProtocolDescriptor(manifest, provision)
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
			asset, err := t.nativeFlowAsset(app, flow.Name, flow.Version)
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
	for _, app := range t.apps {
		manifest := app.Manifest()
		pages, resources := []string{}, []platform.AssetRef{}
		for _, definition := range t.definitions {
			if definition.Ref.App != manifest.ID || definition.Source != "code" {
				continue
			}
			if definition.Ref.Kind == platform.AssetPage {
				pages = append(pages, definition.Ref.Name)
			} else if definition.Ref.Kind != platform.AssetApp {
				resources = append(resources, definition.Ref)
			}
		}
		declaration := platform.Application{Name: manifest.ID, Title: manifest.Title, Pages: pages, Resources: releaseowner.UniqueRefs(resources)}
		if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetApp, Name: manifest.ID}, Source: "code", Version: manifest.Version, ContractVersion: 1, Application: &declaration}); err != nil {
			return err
		}
	}
	for _, et := range t.records.SortedTypes() {
		if err := t.checkObjectProperties(et.Info()); err != nil {
			return err
		}
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

func (t *Tenant) Definitions(m platform.Member) []platform.Definition {
	gen, key := t.definitionsFingerprint(), transport.Rights(m)
	if out, ok := t.views.Definitions(gen, key); ok {
		return out
	}
	out := t.buildDefinitions(m)
	t.views.StoreDefinitions(gen, key, out)
	return out
}

func (t *Tenant) buildDefinitions(m platform.Member) []platform.Definition {
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return []platform.Definition{}
	}
	return definitions
}

func (t *Tenant) definitionsFor(m platform.Member) ([]platform.Definition, error) {
	registered, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return nil, err
	}
	return t.definitionsFrom(m, registered), nil
}

func (r runtime) VisibleApplications(c platform.Caller) []platform.AssetRef {
	refs := []platform.AssetRef{}
	for _, definition := range r.t.Definitions(c.Member) {
		if definition.Application != nil {
			refs = append(refs, definition.Ref)
		}
	}
	return refs
}
func (d *stagedDecision) VisibleApplications(c platform.Caller) []platform.AssetRef {
	return (runtime{d.tenant}).VisibleApplications(c)
}

func (t *Tenant) definitionsFingerprint() uint64 { return t.defGen.Load() }

func (t *Tenant) setDefinitions(definitions []platform.Definition) {
	t.definitions = definitions
	t.defGen.Add(1)
	t.i18n.InstallApplications(definitions)
}
