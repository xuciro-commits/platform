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
	owner, _, _ := strings.Cut(p.Object, ".")
	value := platform.Page{Format: p.Format, Name: p.Name, Title: p.Title, Description: p.Description, Object: platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: p.Object}, DataBindings: p.DataBindings, ActionTriggers: p.ActionTriggers, InputSchema: p.InputSchema, ScopeVisibility: p.ScopeVisibility, WidgetSpec: p.WidgetSpec}
	raw, _ := json.Marshal(value)
	_ = json.Unmarshal(raw, &value)
	return value
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
