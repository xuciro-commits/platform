package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

func (b *Build) SavedDraftChanges(c platform.Caller, application, typ, id string) ([]DraftChange, error) {
	out := []DraftChange{}
	held := map[platform.AssetRef]bool{}
	if application != "" {
		app, ok := platform.Get[Application](c, application)
		if !ok || app.Archived {
			return nil, fmt.Errorf("application draft is unavailable")
		}
		for _, ref := range app.Resources {
			held[ref] = true
		}
		for _, name := range app.Pages {
			held[platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: name}] = true
		}
	}
	for _, entity := range b.Manifest().Entities {
		if typ != "" && entity.Type != typ {
			continue
		}
		if _, err := draftAssetRef(entity.Type, ""); err != nil {
			continue
		}
		info, ok := b.host.Entity(entity.Type)
		if !ok {
			return nil, fmt.Errorf("draft declaration %s is unavailable", entity.Type)
		}
		rows, err := readDefinitionInventoryOf(c, reflect.TypeOf(entity.Model))
		if err != nil {
			return nil, fmt.Errorf("read %s drafts: %w", entity.Type, err)
		}
		for _, record := range rows {
			var meta struct {
				platform.Record
				Name, Title, Kind, Published string
				Evaluation                   []json.RawMessage
			}
			if err := json.Unmarshal(platform.Raw(record), &meta); err != nil {
				return nil, err
			}
			if meta.Archived || id != "" && meta.ID != id {
				continue
			}
			ref, _ := draftAssetRef(entity.Type, meta.Name)
			if entity.Type == TestPlanType && len(meta.Evaluation) > 0 {
				ref.Kind = platform.AssetEvaluation
			}
			if application != "" && !(entity.Type == AppType && meta.ID == application) && !held[ref] {
				var original struct {
					Name string
					Ref  platform.AssetRef
				}
				_ = json.Unmarshal([]byte(meta.Published), &original)
				prior := original.Ref
				if prior.Kind == "" {
					if original.Name == "" {
						continue
					}
					prior, _ = draftAssetRef(entity.Type, original.Name)
				}
				if !held[prior] {
					continue
				}
			}
			change := DraftChange{Ref: ref, Type: entity.Type, ID: meta.ID, Title: meta.Title, Variant: meta.Kind, Published: meta.Published != "", Changed: meta.Changed, Owner: draftOwner(meta.Record), Fields: []DraftChangeField{}}
			if change.Published {
				var original struct {
					Name string
					Ref  platform.AssetRef
				}
				if json.Unmarshal([]byte(meta.Published), &original) == nil {
					change.Prior = original.Ref
					if change.Prior.Kind == "" && original.Name != "" {
						change.Prior, _ = draftAssetRef(entity.Type, original.Name)
					}
				}
			}
			before, after, portable, err := b.draftComparison(record, meta.Published, info, ref)
			if err != nil {
				change.Diagnostic = err.Error()
			} else {
				var names []string
				if portable {
					for name := range before {
						names = append(names, name)
					}
					for name := range after {
						if !slices.Contains(names, name) {
							names = append(names, name)
						}
					}
					slices.Sort(names)
				} else {
					for _, field := range info.Fields {
						if !field.ReadOnly {
							names = append(names, field.Name)
						}
					}
				}
				for _, name := range names {
					if bytes.Equal(before[name], after[name]) {
						continue
					}
					field := DraftChangeField{Name: name}
					field.Removed, field.Retyped = breakingMembers(before[name], after[name])
					if typ != "" && id != "" {
						field.Before, field.After = before[name], after[name]
					}
					change.Fields = append(change.Fields, field)
				}
			}
			if !change.Published || len(change.Fields) > 0 || change.Diagnostic != "" {
				out = append(out, change)
			}
		}
	}
	slices.SortFunc(out, func(a, b DraftChange) int { return strings.Compare(a.Type+"/"+a.ID, b.Type+"/"+b.ID) })
	return out, nil
}

func (b *Build) WorkingDrafts(owner string) ([]DraftChange, error) {
	changes, err := b.SavedDraftChanges(b.host.Automation(platform.Caller{}, ID), "", "", "")
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(changes, func(change DraftChange) bool { return change.Owner != owner }), nil
}

func (b *Build) draftEntities(kind platform.AssetKind) []platform.Entity {
	var out []platform.Entity
	for _, entity := range b.Manifest().Entities {
		ref, err := draftAssetRef(entity.Type, "")
		if err == nil && (ref.Kind == kind || entity.Type == TestPlanType && kind == platform.AssetEvaluation) {
			out = append(out, entity)
		}
	}
	return out
}

func (b *Build) DraftChangeOf(member platform.Member, kind platform.AssetKind, id string) (DraftChange, bool, error) {
	c := b.host.Caller(platform.Caller{}, member, ID)
	for _, entity := range b.draftEntities(kind) {
		changes, err := b.SavedDraftChanges(c, "", entity.Type, id)
		if err != nil {
			return DraftChange{}, false, err
		}
		if len(changes) > 0 {
			return changes[0], true, nil
		}
	}
	return DraftChange{}, false, nil
}

func (b *Build) DraftPublished(kind platform.AssetKind, id string) (string, string, bool, error) {
	c := b.host.Automation(platform.Caller{}, ID)
	domain, _ := json.Marshal([][]any{{"id", "=", id}})
	for _, entity := range b.draftEntities(kind) {
		rows, _, err := c.FindOf(reflect.TypeOf(entity.Model), platform.Query{Domain: domain, Limit: 1})
		if err != nil {
			return "", "", false, err
		}
		for _, row := range rows {
			var meta struct {
				platform.Record
				Published string
			}
			if json.Unmarshal(platform.Raw(row), &meta) == nil && meta.ID == id && !meta.Archived {
				return entity.Type, meta.Published, true, nil
			}
		}
	}
	return "", "", false, nil
}

func draftOwner(r platform.Record) string {
	if r.Changed.By == "" || strings.HasPrefix(r.Changed.By, "app:") {
		return r.Created.By
	}
	return r.Changed.By
}

func (b *Build) draftComparison(record any, published string, info platform.EntityInfo, ref platform.AssetRef) (before, after map[string]json.RawMessage, portable bool, err error) {
	current := platform.Raw(record)
	previous := json.RawMessage(`{}`)
	if published != "" {
		var frozen platform.ReleaseAsset
		if json.Unmarshal([]byte(published), &frozen) == nil && frozen.Ref.Kind != "" && len(frozen.Body) > 0 {
			portable = true
			previous = frozen.Body
			asset, compiled := b.jointDeclarations[ref]
			if !compiled {
				switch r := record.(type) {
				case Alert:
					asset, err = alertReleaseAsset(r, max(1, r.DefinitionVersion))
				case SampleSetRecord:
					asset, err = sampleSetAsset(r, max(1, r.DefinitionVersion))
				case TestPlan:
					asset, err = b.testPlanAsset(r, max(1, r.DefinitionVersion))
				default:
					_, ordinal, _, _ := integrationMetadata(record)
					asset, err = b.integrationAsset(frozen.Ref.Kind, record, max(1, ordinal))
				}
			}
			current = asset.Body
		} else {
			original := reflect.New(info.Go).Interface()
			err = json.Unmarshal([]byte(published), original)
			previous = platform.Raw(original)
		}
	}
	if err != nil {
		return
	}
	if _, object := record.(Object); object {
		authoring := func(raw json.RawMessage) (json.RawMessage, error) {
			var object Object
			if err := json.Unmarshal(raw, &object); err != nil {
				return nil, err
			}
			object.ActionTypes = slices.Clone(object.ActionTypes)
			if len(object.ActionTypes) == 0 {
				for _, action := range object.Actions {
					object.ActionTypes = append(object.ActionTypes, platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: TypeOf(object.Name) + "." + action.Name}})
				}
			}
			for i := range object.ActionTypes {
				object.ActionTypes[i].SourceVersion = ""
			}
			object.Actions = nil
			return json.Marshal(object)
		}
		current, err = authoring(current)
		if err != nil {
			return
		}
		previous, err = authoring(previous)
		if err != nil {
			return
		}
	}
	canonicalFields := func(raw json.RawMessage) (map[string]json.RawMessage, error) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		for name, value := range fields {
			var parsed any
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.UseNumber()
			if err := decoder.Decode(&parsed); err != nil {
				return nil, err
			}
			fields[name] = platform.Raw(parsed)
		}
		return fields, nil
	}
	before, err = canonicalFields(previous)
	if err == nil {
		after, err = canonicalFields(current)
	}
	return
}
