package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
)

// ReleasePublication changes only publication metadata beside the mutable draft.
// The host commits these images with the release pointer in one release-result.
type ReleasePublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

type ReleaseDeclaration struct {
	Entity      platform.Entity
	Actions     []platform.Action
	Pages       []platform.Page
	Application *platform.Application
}

func (b *Build) ReleaseDeclaration(p ReleasePublication) (ReleaseDeclaration, error) {
	e, actions, pages, app, err := b.publicationImage(p.Schema, p.Image)
	return ReleaseDeclaration{e, actions, pages, app}, err
}

// PrepareReleasePublications compiles frozen owner descriptors. It neither
// installs definitions nor edits drafts. Renaming an owning draft is not yet
// supported: the candidate has logical identities rather than editor row IDs.
func (b *Build) PrepareReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, []platform.ReleaseAsset, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, nil, err
	}
	var publications []ReleasePublication
	add := func(schema string, record any) error {
		raw, err := json.Marshal(record)
		if err == nil {
			publications = append(publications, ReleasePublication{schema, raw})
		}
		return err
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			if slices.ContainsFunc([]platform.Entity{b.objectEntity(), b.pageEntity(), b.applicationEntity(), b.testPlanEntity(), b.processEntity(), b.functionEntity(), b.functionCallEntity(), b.evaluationEntity()}, func(e platform.Entity) bool { return e.Type == asset.Ref.Name }) {
				continue // immutable code-owned metadata, not a tenant-authored object
			}
			var frozen Object
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || TypeOf(frozen.Name) != asset.Ref.Name {
				return nil, nil, fmt.Errorf("invalid saved object %s", asset.Ref)
			}
			i := slices.IndexFunc(objects, func(o Object) bool { return !o.Archived && o.Name == frozen.Name })
			if i < 0 {
				return nil, nil, fmt.Errorf("saved object %s has no matching draft; renamed or archived definitions need an upgrade plan", asset.Ref)
			}
			frozen.Record = objects[i].Record
			objects[i].State, objects[i].Installed = "published", TypeOf(frozen.Name)
			objects[i].Published = published(frozen)
			if err := add(SchemaPublish, objects[i]); err != nil {
				return nil, nil, err
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return !p.Archived && p.Name == asset.Ref.Name })
			if i < 0 { // the object's generated page has no second owner record
				continue
			}
			var pageDescriptor platform.Page
			if err := json.Unmarshal(asset.Body, &pageDescriptor); err != nil || pageDescriptor.Name != asset.Ref.Name {
				return nil, nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			if previous, ok := wasPublished[Page](pages[i].Published); ok && !maps.Equal(pageFunctionBindings(pageDescriptor), pageFunctionBindings(descriptor(previous))) {
				return nil, nil, fmt.Errorf("page %s changes AI function bindings; publish the binding through its owner before release activation", asset.Ref)
			}
			frozen := Page{Record: pages[i].Record, Name: pageDescriptor.Name, Title: pageDescriptor.Title, Description: pageDescriptor.Description,
				Object: pageDescriptor.Object.Name, List: pageDescriptor.ListFields, Detail: pageDescriptor.DetailFields}
			for _, action := range pageDescriptor.Actions {
				frozen.Actions = append(frozen.Actions, action.Name)
			}
			for _, s := range pageDescriptor.Sections {
				section := Section{Widget: s.Widget, Title: s.Title, Width: s.Width, Object: s.Object.Name, Relation: s.Relation,
					Fields: s.Fields, Group: s.Group, Measure: s.Measure, Text: s.Text}
				if s.Query.Name != "" {
					section.Query = s.Query.App + "." + s.Query.Name
				}
				for _, action := range s.Actions {
					section.Actions = append(section.Actions, action.Name)
				}
				if s.Function != nil {
					var version int
					if _, err := fmt.Sscanf(s.Function.SourceVersion, definitionVersion+".function-%d", &version); err != nil || version < 1 {
						return nil, nil, fmt.Errorf("invalid saved function binding in page %s", asset.Ref)
					}
					section.Function = &platform.FunctionRef{Name: s.Function.Ref.Name, Version: version}
				}
				frozen.Sections = append(frozen.Sections, section)
			}
			pages[i].State, pages[i].Published = "published", published(frozen)
			if err := add(SchemaRelease, pages[i]); err != nil {
				return nil, nil, err
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return !a.Archived && a.Name == asset.Ref.Name })
			if i < 0 {
				return nil, nil, fmt.Errorf("saved application %s has no matching draft", asset.Ref)
			}
			var frozen Application
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
				return nil, nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			frozen.Record = apps[i].Record
			apps[i].State, apps[i].Published = "published", published(frozen)
			if err := add(SchemaHandOver, apps[i]); err != nil {
				return nil, nil, err
			}
		}
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, nil, err
	}
	available, err := releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	if err != nil {
		return nil, nil, err
	}
	functions, err := b.functionAssets()
	return publications, append(available, functions...), err
}

func pageFunctionBindings(p platform.Page) map[string]string {
	bindings := map[string]string{}
	for _, section := range p.Sections {
		if section.Function != nil {
			bindings[section.Function.Ref.String()] = section.Function.SourceVersion
		}
	}
	return bindings
}

// PublicationRecord identifies the metadata row, without executing a decision.
func PublicationRecord(p ReleasePublication) (string, string, error) {
	typ, _, ok := strings.Cut(p.Schema, ".publish")
	var record platform.Record
	if !ok || !slices.Contains([]string{ObjectType, PageType, AppType}, typ) || json.Unmarshal(p.Image, &record) != nil || record.ID == "" {
		return "", "", fmt.Errorf("invalid release publication row")
	}
	return typ, record.ID, nil
}
