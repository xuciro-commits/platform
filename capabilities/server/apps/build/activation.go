package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"reflect"
	"slices"
)

// prepareReleasePublications compiles frozen owner descriptors. It neither
// installs definitions nor edits drafts. Renaming an owning draft is not yet
// supported: the candidate has logical identities rather than editor row IDs.
func (b *Build) prepareReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	var publications []ReleasePublication
	add := func(schema string, record any) error {
		raw, err := json.Marshal(record)
		if err == nil {
			publications = append(publications, ReleasePublication{Schema: schema, Image: raw})
		}
		return err
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			code, err := b.retainedCodeObject(asset)
			if err != nil {
				return nil, err
			}
			if code {
				continue
			}
			var frozen Object
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || TypeOf(frozen.Name) != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved object %s", asset.Ref)
			}
			if err := mapDispatchIntegrations(&frozen, b.integrationID); err != nil {
				return nil, err
			}
			i := slices.IndexFunc(objects, func(o Object) bool { return !o.Archived && o.Name == frozen.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved object %s has no matching draft; renamed or archived definitions need an upgrade plan", asset.Ref)
			}
			frozen.Record = objects[i].Record
			objects[i].State, objects[i].Installed = "published", TypeOf(frozen.Name)
			objects[i].Published = published(frozen)
			objects[i].ExtensionSource = frozen.ExtensionSource
			objects[i].SchemaVersions = frozen.SchemaVersions
			if err := add(SchemaPublish, objects[i]); err != nil {
				return nil, err
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return !p.Archived && p.Name == asset.Ref.Name })
			if i < 0 { // the object's generated page has no second owner record
				continue
			}
			var pageDescriptor platform.Page
			if err := json.Unmarshal(asset.Body, &pageDescriptor); err != nil || pageDescriptor.Name != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			if previous, ok := wasPublished[Page](pages[i].Published); ok && !maps.Equal(pageFunctionBindings(pageDescriptor), pageFunctionBindings(descriptor(previous))) {
				return nil, fmt.Errorf("page %s changes AI function bindings; publish the binding through its owner before release activation", asset.Ref)
			}
			frozen, err := pageDraft(pageDescriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			frozen.Record = pages[i].Record
			frozen.Versions = slices.Clone(pages[i].Versions)
			frozen.Published = pages[i].Published
			if err := retainPagePublication(&frozen); err != nil {
				return nil, err
			}
			pages[i].State, pages[i].Published, pages[i].Versions = "published", frozen.Published, frozen.Versions
			if err := add(SchemaRelease, pages[i]); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return !a.Archived && a.Name == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved application %s has no matching draft", asset.Ref)
			}
			var frozen Application
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			frozen.Record = apps[i].Record
			apps[i].State, apps[i].Published = "published", published(frozen)
			if err := add(SchemaHandOver, apps[i]); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			i := slices.IndexFunc(processes, func(p Process) bool { return !p.Archived && TypeOf(p.Name) == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved flow %s has no matching owner", asset.Ref)
			}
			frozen, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if prior, ok := wasPublished[Process](processes[i].Published); ok {
				priorAsset, err := processReleaseAsset(prior, asset.SourceVersion)
				if err == nil {
					var a, z any
					_ = json.Unmarshal(priorAsset.Body, &a)
					_ = json.Unmarshal(asset.Body, &z)
					if reflect.DeepEqual(a, z) {
						continue
					}
				}
			}
			if processes[i].Version >= 64 {
				return nil, fmt.Errorf("flow version family is full")
			}
			frozen.Record, frozen.Version, frozen.State = processes[i].Record, processes[i].Version+1, "published"
			frozen.Layout = processes[i].Layout
			processes[i].Version, processes[i].State = frozen.Version, "published"
			processes[i].Published = published(frozen)
			processes[i].Versions = append(slices.Clone(processes[i].Versions), processes[i].Published)
			if err := add(SchemaProcess, processes[i]); err != nil {
				return nil, err
			}
		}
	}

	for _, prepare := range []func([]platform.ReleaseAsset) ([]ReleasePublication, error){b.prepareQueryReleasePublications, b.preparePropertyTypeReleasePublications, b.prepareLinkTypeReleasePublications, b.prepareFunctionReleasePublications, b.prepareAgentReleasePublications, b.prepareTestPlanPublications, b.PrepareCodeReleasePublications, b.prepareTablePublications, b.prepareAlertPublications} {
		images, err := prepare(assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	return publications, nil
}

func pageFunctionBindings(p platform.Page) map[string]string {
	bindings := map[string]string{}
	for _, section := range p.Sections {
		if section.Function != nil {
			bindings[section.Function.Ref.String()] = section.Function.SourceVersion
		}
		if section.Operation != nil {
			bindings[section.Operation.Ref.String()] = section.Operation.SourceVersion
		}
	}
	return bindings
}

// pageDraft is the builder's page record a saved page descriptor denotes: what
// activation writes onto the owning draft, and what an environment that never
// authored the page creates before it can activate it.
func pageDraft(pageDescriptor platform.Page) (Page, error) {
	frozen := Page{Name: pageDescriptor.Name, Title: pageDescriptor.Title, Description: pageDescriptor.Description,
		Object: pageDescriptor.Object.Name, List: pageDescriptor.ListFields, Detail: pageDescriptor.DetailFields, Selections: slices.Clone(pageDescriptor.Selections), Document: pageDescriptor.Document}
	for _, action := range pageDescriptor.Actions {
		frozen.Actions = append(frozen.Actions, action.Name)
	}
	for _, s := range pageDescriptor.Sections {
		section := Section{PageWidgetConfig: s.PageWidgetConfig, Facets: slices.Clone(s.Facets), FilterSearchVariable: s.FilterSearchVariable, ID: s.ID, ConfigVersion: s.ConfigVersion, Widget: s.Widget, Title: s.Title, Width: s.Width, Object: s.Object.Name, Relation: s.Relation, Selection: s.Selection, RecordVariable: s.RecordVariable, SelectionVariable: s.SelectionVariable, FilterVariable: s.FilterVariable, CollectionVariable: s.CollectionVariable, ParentSelection: s.ParentSelection,
			Fields: s.Fields, Group: s.Group, Mark: s.Mark, ChartVariant: s.ChartVariant, ColumnGroup: s.ColumnGroup, TimeStart: s.TimeStart, TimeEnd: s.TimeEnd, TimeLabel: s.TimeLabel, TimeGroup: s.TimeGroup, CardLabel: s.CardLabel, Measure: s.Measure, Text: s.Text, Operation: s.Operation, Inputs: s.Inputs}
		section.RecordLinks, section.Buttons = slices.Clone(s.RecordLinks), slices.Clone(s.Buttons)
		section.TableColumns = slices.Clone(s.TableColumns)
		section.ActionDefaults = slices.Clone(s.ActionDefaults)
		if s.InlineEdit != nil {
			section.InlineEdit = &InlineEdit{Action: s.InlineEdit.Action.Name, Fields: slices.Clone(s.InlineEdit.Fields)}
		}
		if s.Query.Name != "" {
			section.Query = s.Query.App + "." + s.Query.Name
		}
		for _, action := range s.Actions {
			section.Actions = append(section.Actions, action.Name)
		}
		if s.Function != nil {
			var version int
			if _, err := fmt.Sscanf(s.Function.SourceVersion, definitionVersion+".function-%d", &version); err != nil || version < 1 {
				return Page{}, fmt.Errorf("invalid saved function binding")
			}
			section.Function = &platform.FunctionRef{Name: s.Function.Ref.Name, Version: version}
		}
		frozen.Sections = append(frozen.Sections, section)
	}
	return frozen, nil
}

// Draft is one builder draft a candidate's assets rest on: the record type,
// the asset name and the draft image.
type Draft struct {
	Type  string
	Name  string
	Image json.RawMessage
}

// releaseDraftsOf lists the builder drafts a candidate's assets are published onto:
// objects, authored pages (an object's generated page has no draft), applications
// and flows. Promotion into an environment that never authored them creates each
// missing one through the builder's own create action (ADR-0047 §11).
func (b *Build) releaseDraftsOf(candidate platform.ReleaseCandidate) ([]Draft, error) {
	generated := map[string]bool{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetObject {
			var o Object
			if json.Unmarshal(asset.Body, &o) == nil {
				generated[page(o).Name] = true
			}
		}
	}
	var out []Draft
	add := func(typ, name string, record any) error {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		out = append(out, Draft{Type: typ, Name: name, Image: raw})
		return nil
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			var o Object
			if err := json.Unmarshal(asset.Body, &o); err != nil || o.Name == "" {
				continue // code-owned metadata is not a draft
			}
			if err := mapDispatchIntegrations(&o, b.integrationID); err != nil {
				return nil, err
			}
			if err := add(ObjectType, o.Name, o); err != nil {
				return nil, err
			}
		case platform.AssetPage:
			if generated[asset.Ref.Name] {
				continue
			}
			var descriptor platform.Page
			if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
				return nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			p, err := pageDraft(descriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			if err := add(PageType, p.Name, p); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			var a Application
			if err := json.Unmarshal(asset.Body, &a); err != nil {
				return nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			if err := add(AppType, a.Name, a); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			p, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if err := add(ProcessType, p.Name, p); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// ReleasePublication changes only publication metadata beside the mutable draft.
// The host commits these images with the release pointer in one release-result.
type ReleasePublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}
