package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/internal/construction"
	"platformserver/platform"
)

// SavedDraftChanges compares saved declarations, not lifecycle badges or a
// newly allocated publication version. It performs no publication or execution.
func (b *Build) SavedDraftChanges(c platform.Caller, application, typ, id string) ([]construction.DraftChange, error) {
	out := []construction.DraftChange{}
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
			held[platform.AssetRef{App: construction.ID, Kind: platform.AssetPage, Name: name}] = true
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
			if entity.Type == construction.TestPlanType && len(meta.Evaluation) > 0 {
				ref.Kind = platform.AssetEvaluation
			}
			if application != "" && !(entity.Type == construction.AppType && meta.ID == application) && !held[ref] {
				// A renamed draft still belongs to the application that references
				// its original published identity until membership is edited.
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
			change := construction.DraftChange{Ref: ref, Type: entity.Type, ID: meta.ID, Title: meta.Title, Variant: meta.Kind, Published: meta.Published != "", Changed: meta.Changed, Fields: []construction.DraftChangeField{}}
			before, after, portable, err := b.draftComparison(record, meta.Published, info)
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
					field := construction.DraftChangeField{Name: name}
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
	slices.SortFunc(out, func(a, b construction.DraftChange) int { return strings.Compare(a.Type+"/"+a.ID, b.Type+"/"+b.ID) })
	return out, nil
}

// Ordinary publications retain the typed editor record. Portable integration,
// scenario and migration publications retain the original owner's release body.
func (b *Build) draftComparison(record any, published string, info platform.EntityInfo) (before, after map[string]json.RawMessage, portable bool, err error) {
	current := platform.Raw(record)
	previous := json.RawMessage(`{}`)
	if published != "" {
		var frozen platform.ReleaseAsset
		if json.Unmarshal([]byte(published), &frozen) == nil && frozen.Ref.Kind != "" && len(frozen.Body) > 0 {
			portable = true
			previous = frozen.Body
			var asset platform.ReleaseAsset
			switch r := record.(type) {
			case construction.Alert:
				asset, err = alertReleaseAsset(r, max(1, r.DefinitionVersion))
			case SampleSetRecord:
				asset, err = sampleSetAsset(r, max(1, r.DefinitionVersion))
			case construction.TestPlan:
				asset, err = b.testPlanAsset(r, max(1, r.DefinitionVersion))
			default:
				_, ordinal, _, _ := integrationMetadata(record)
				asset, err = b.integrationAsset(frozen.Ref.Kind, record, max(1, ordinal))
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
