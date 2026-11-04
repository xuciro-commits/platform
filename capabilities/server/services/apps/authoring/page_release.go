package authoring

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func Descriptor(p Page, app, version string) platform.Page {
	if p.Format == 3 || p.Format == 4 {
		owner, _, _ := strings.Cut(p.Object, ".")
		value := platform.Page{Format: p.Format, Name: p.Name, Title: p.Title, Description: p.Description, Object: platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: p.Object}, DataBindings: p.DataBindings, ActionTriggers: p.ActionTriggers, InputSchema: p.InputSchema, ScopeVisibility: p.ScopeVisibility, WidgetSpec: p.WidgetSpec}
		raw, _ := json.Marshal(value)
		_ = json.Unmarshal(raw, &value)
		return value
	}
	appName, _, _ := strings.Cut(p.Object, ".")
	actions := descriptorActions(p.Actions)
	out := platform.Page{Name: p.Name, Title: p.Title, Description: p.Description, Layout: "list-detail",
		Object:     platform.AssetRef{App: appName, Kind: platform.AssetObject, Name: p.Object},
		ListFields: slices.Clone(p.List), DetailFields: slices.Clone(p.Detail), Actions: actions}
	out.Selections = slices.Clone(p.Selections)
	out.Document = p.Document
	if len(p.Sections) == 0 {
		return out
	}
	out.Layout, out.ListFields, out.DetailFields, out.Sections = "composed", nil, nil, []platform.Section{}
	for _, s := range p.Sections {
		values := map[string]json.RawMessage{}
		raw, _ := json.Marshal(s)
		_ = json.Unmarshal(raw, &values)
		for _, key := range []string{"object", "query", "actions", "function", "inlineEdit"} {
			delete(values, key)
		}
		raw, _ = json.Marshal(values)
		var section platform.Section
		_ = json.Unmarshal(raw, &section)

		if s.InlineEdit != nil {
			section.InlineEdit = &platform.PageInlineEdit{Action: platform.AssetRef{App: strings.SplitN(s.InlineEdit.Action, ".", 2)[0], Kind: platform.AssetAction, Name: s.InlineEdit.Action}, Fields: slices.Clone(s.InlineEdit.Fields)}
		}
		if s.Function != nil {
			section.Function = &platform.AssetBinding{Ref: platform.AssetRef{App: app, Kind: platform.AssetFunction, Name: s.Function.Name},
				SourceVersion: version + ".function-" + fmt.Sprint(s.Function.Version)}
		}
		if s.Object != "" {
			owner, _, _ := strings.Cut(s.Object, ".")
			section.Object = platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: s.Object}
		}
		if s.Query != "" {
			owner, name, _ := strings.Cut(s.Query, ".")
			section.Query = platform.AssetRef{App: owner, Kind: platform.AssetQuery, Name: name}
		}
		if s.Actions != nil {
			section.Actions = descriptorActions(s.Actions)
		}
		out.Sections = append(out.Sections, section)
	}
	return out
}

func (o *Owner) PageAsset(p Page, sourceVersion string) (platform.ReleaseAsset, error) {
	return platform.PageReleaseAsset(o.env.AppID(), sourceVersion, Descriptor(p, o.env.AppID(), o.env.Version()))
}

func (o *Owner) PageAssetFromView(p platform.Page, sourceVersion string) (platform.ReleaseAsset, error) {
	return platform.PageReleaseAsset(o.env.AppID(), sourceVersion, p)
}

func (o *Owner) ObjectPageAsset(object ontology.Object, sourceVersion string) (platform.ReleaseAsset, error) {
	return platform.PageReleaseAsset(o.env.AppID(), sourceVersion, ontology.CompileObjectPage(object, o.env.AppID()))
}

func (o *Owner) PublishedPage(p Page) string {
	p.Published = ""
	p.Versions = nil
	return string(platform.Raw(p))
}

func (o *Owner) RetainPagePublication(p *Page) error {
	snapshots := append(slices.Clone(p.Versions), p.Published, o.PublishedPage(*p))
	kept := []string{}
	seen := map[string]bool{}
	for _, raw := range snapshots {
		if raw == "" {
			continue
		}
		saved, ok := publication.Read[Page](raw)
		if !ok || saved.ID != p.ID || saved.Name == "" || saved.Published != "" || len(saved.Versions) != 0 {
			return fmt.Errorf("invalid original page publication history")
		}
		view := Descriptor(saved, o.env.AppID(), o.env.Version())
		if _, err := platform.PageReleaseAsset(o.env.AppID(), "", view); err != nil {
			return err
		}
		version, err := platform.PageContentVersion(view)
		if err != nil {
			return err
		}
		if !seen[version] {
			seen[version] = true
			kept = append(kept, raw)
		}
	}
	if len(kept) > 64 {
		return fmt.Errorf("a page may retain at most 64 different published contents")
	}
	p.Versions = kept
	p.Published = o.PublishedPage(*p)
	return nil
}

func (o *Owner) PageContent(name, version string) (platform.Page, bool) {
	if platform.CheckPageContentVersion(version) != nil || o.env.Host() == nil {
		return platform.Page{}, false
	}
	rows, err := o.Pages()
	if err != nil {
		return platform.Page{}, false
	}
	for _, p := range rows {
		if p.Archived || p.Name != name || p.Published == "" {
			continue
		}
		for _, raw := range append(slices.Clone(p.Versions), p.Published) {
			saved, ok := publication.Read[Page](raw)
			if !ok || saved.ID != p.ID || saved.Name == "" || saved.Published != "" || len(saved.Versions) != 0 {
				return platform.Page{}, false
			}
			if saved.Name != name {
				continue
			}
			view := Descriptor(saved, o.env.AppID(), o.env.Version())
			digest, err := platform.PageContentVersion(view)
			if err == nil && digest == version {
				return view, true
			}
		}
	}
	return platform.Page{}, false
}

func (o *Owner) HasPublishedPage(name string) (bool, error) {
	pages, err := o.Pages()
	if err != nil {
		return false, err
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		saved, ok := publication.Read[Page](record.Published)
		if !ok {
			return false, fmt.Errorf("invalid published page %s", record.ID)
		}
		if saved.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (o *Owner) PageFromDescriptor(pageDescriptor platform.Page) (Page, error) {
	raw, err := json.Marshal(pageDescriptor)
	if err != nil {
		return Page{}, err
	}
	var original map[string]json.RawMessage
	if err = json.Unmarshal(raw, &original); err != nil {
		return Page{}, err
	}
	original["object"] = platform.Raw(pageDescriptor.Object.Name)
	if pageDescriptor.Format != 3 && pageDescriptor.Format != 4 {
		original["list"], original["detail"] = original["listFields"], original["detailFields"]
		delete(original, "listFields")
		delete(original, "detailFields")
		if pageDescriptor.Actions != nil {
			actions := make([]string, 0, len(pageDescriptor.Actions))
			for _, action := range pageDescriptor.Actions {
				actions = append(actions, action.Name)
			}
			original["actions"] = platform.Raw(actions)
		}
		sections := make([]map[string]json.RawMessage, 0, len(pageDescriptor.Sections))
		for _, section := range pageDescriptor.Sections {
			var value map[string]json.RawMessage
			if err = json.Unmarshal(platform.Raw(section), &value); err != nil {
				return Page{}, err
			}
			if section.Object.Name != "" {
				value["object"] = platform.Raw(section.Object.Name)
			} else {
				delete(value, "object")
			}
			if section.Query.Name != "" {
				value["query"] = platform.Raw(section.Query.App + "." + section.Query.Name)
			} else {
				delete(value, "query")
			}
			if section.Actions != nil {
				actions := make([]string, 0, len(section.Actions))
				for _, action := range section.Actions {
					actions = append(actions, action.Name)
				}
				value["actions"] = platform.Raw(actions)
			}
			if section.InlineEdit != nil {
				value["inlineEdit"] = platform.Raw(InlineEdit{Action: section.InlineEdit.Action.Name, Fields: section.InlineEdit.Fields})
			}
			if section.Function != nil {
				var version int
				if _, err = fmt.Sscanf(section.Function.SourceVersion, o.env.Version()+".function-%d", &version); err != nil || version < 1 {
					return Page{}, fmt.Errorf("invalid saved function binding")
				}
				value["function"] = platform.Raw(platform.FunctionRef{Name: section.Function.Ref.Name, Version: version})
			}
			sections = append(sections, value)
		}
		if pageDescriptor.Sections != nil {
			original["sections"] = platform.Raw(sections)
		}
	}
	var frozen Page
	if err = json.Unmarshal(platform.Raw(original), &frozen); err != nil {
		return Page{}, err
	}
	return frozen, nil
}

func (o *Owner) PreparePagePublication(asset platform.ReleaseAsset, all []platform.ReleaseAsset, current Page) (Page, bool, error) {
	if current.ID == "" {
		generated, err := o.generatedReleasePage(asset, all)
		if err != nil {
			return Page{}, false, err
		}
		if generated {
			return Page{}, true, nil
		}
	}
	var pageDescriptor platform.Page
	if err := json.Unmarshal(asset.Body, &pageDescriptor); err != nil || pageDescriptor.Name != asset.Ref.Name {
		return Page{}, false, fmt.Errorf("invalid saved page %s", asset.Ref)
	}
	if previous, ok := publication.Read[Page](current.Published); ok && !maps.Equal(pageFunctionBindings(pageDescriptor), pageFunctionBindings(Descriptor(previous, o.env.AppID(), o.env.Version()))) {
		return Page{}, false, fmt.Errorf("page %s changes AI function bindings; publish the binding through its owner before release activation", asset.Ref)
	}
	frozen, err := o.PageFromDescriptor(pageDescriptor)
	if err != nil {
		return Page{}, false, fmt.Errorf("%w in page %s", err, asset.Ref)
	}
	frozen.Record = current.Record
	frozen.Versions = slices.Clone(current.Versions)
	frozen.Published = current.Published
	if err := o.RetainPagePublication(&frozen); err != nil {
		return Page{}, false, err
	}
	current.State, current.Published, current.Versions = "published", frozen.Published, frozen.Versions
	return current, false, nil
}

func (o *Owner) CheckPageDraft(record Page, validate bool) (Page, platform.AssetRef, platform.AssetRef, bool, error) {
	var prior platform.AssetRef
	hadPrior := false
	if was, ok := publication.Read[Page](record.Published); ok {
		prior = platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetPage, Name: was.Name}
		hadPrior = true
	}
	next := platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetPage, Name: record.Name}
	if err := o.checkPage(record); err != nil {
		return Page{}, prior, next, hadPrior, err
	}
	if validate {
		if err := o.env.Host().ValidateInstallPage(Descriptor(record, o.env.AppID(), o.env.Version())); err != nil {
			return Page{}, prior, next, hadPrior, err
		}
	}
	if err := o.RetainPagePublication(&record); err != nil {
		return Page{}, prior, next, hadPrior, err
	}
	return record, prior, next, hadPrior, nil
}

func (o *Owner) generatedReleasePage(page platform.ReleaseAsset, assets []platform.ReleaseAsset) (bool, error) {
	for _, asset := range assets {
		if asset.Ref.App != o.env.AppID() || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object ontology.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return false, err
		}
		if o.env.AppID()+"."+object.Name != page.Ref.Name {
			continue
		}
		generated, err := platform.PageReleaseAsset(o.env.AppID(), o.env.Version(), ontology.CompileObjectPage(object, o.env.AppID()))
		if err != nil {
			return false, err
		}
		var expected, actual any
		if err := json.Unmarshal(generated.Body, &expected); err != nil {
			return false, err
		}
		if err := json.Unmarshal(page.Body, &actual); err != nil {
			return false, err
		}
		return reflect.DeepEqual(expected, actual), nil
	}
	return false, nil
}

func (o *Owner) IsGeneratedReleasePage(page platform.ReleaseAsset, assets []platform.ReleaseAsset) (bool, error) {
	return o.generatedReleasePage(page, assets)
}

func pageFunctionBindings(p platform.Page) map[string]string {
	bindings := map[string]string{}
	safe, err := p.Safety()
	if err != nil {
		return bindings
	}
	for _, trigger := range safe.ActionTriggers {
		if trigger.Function != nil {
			bindings[trigger.Function.Ref.String()] = trigger.Function.SourceVersion
		}
		if trigger.Operation != nil {
			bindings[trigger.Operation.Ref.String()] = trigger.Operation.SourceVersion
		}
	}
	return bindings
}
