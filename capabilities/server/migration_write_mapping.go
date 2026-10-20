package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"time"

	"platformserver/internal/host"
	"platformserver/platform"
)

// Retained decisions use the same reviewed forward conversions as migration.
// Conversion is part of this new decision; recovery only applies its images.
func (r *flowExecutionRuntime) mapRetainedWrite(me, typ string, before, after map[string]json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, []retainedExtractionWrite, error) {
	before, after = maps.Clone(before), maps.Clone(after)
	release := r.release
	declared := r.shapes
	seen := map[string]bool{}
	writes := []retainedExtractionWrite{}
	convertedSources := map[string]bool{}
	for {
		if seen[release] {
			return nil, nil, nil, fmt.Errorf("retained write migration path contains a cycle")
		}
		seen[release] = true
		var selected *platform.MigrationRun
		if runs := r.records.types[platform.MigrationRunType]; runs != nil {
			for _, value := range runs.rows.All() {
				run := value.Value.Interface().(platform.MigrationRun)
				if run.State != "complete" || len(run.Failures) != 0 || run.Plan.From != release || run.SourceShapes[typ] == "" {
					continue
				}
				if selected != nil {
					return nil, nil, nil, fmt.Errorf("retained write has ambiguous completed migrations")
				}
				copy := run
				selected = &copy
			}
		}
		if selected == nil {
			current := r.records.types[typ]
			for _, target := range current.info.Fields {
				// Split/merge source values are historical fields in this release.
				// Their declared destinations receive the write; the caller's
				// original values remain in its retained source projection.
				if target.ReadOnly && target.AliasOf == "" && convertedSources[target.Name] {
					if value, exists := before[target.Name]; exists {
						after[target.Name] = value
					} else {
						delete(after, target.Name)
					}
				}
				if string(before[target.Name]) == string(after[target.Name]) {
					continue
				}
				if target.AliasOf != "" {
					continue
				}
				field, found := declared[typ].Field(target.Name)
				if !found || field.Type != target.Type || field.Ref != target.Ref {
					return nil, nil, nil, fmt.Errorf("retained write %s lacks its declared field conversion", target.Name)
				}
			}
			return before, after, writes, nil
		}
		sources, err := r.tenant.releaseShapes(selected.Plan.From)
		if err != nil {
			return nil, nil, nil, err
		}
		targets, err := r.tenant.releaseShapes(selected.Plan.To)
		if err != nil {
			return nil, nil, nil, err
		}
		for name, want := range selected.SourceShapes {
			shape, err := acceptance.Digest(sources[name].Fields)
			if err != nil || shape != want {
				return nil, nil, nil, fmt.Errorf("retained write source shape differs from its accepted migration")
			}
		}
		for name, want := range selected.TargetShapes {
			shape, err := acceptance.Digest(targets[name].Fields)
			if err != nil || shape != want {
				return nil, nil, nil, fmt.Errorf("retained write target shape differs from its accepted migration")
			}
		}
		program, err := platform.CompileMigrationPlan(selected.Plan, sources, targets)
		if err != nil {
			return nil, nil, nil, err
		}
		context := platform.ExprContext{Me: me, Now: r.at, Today: r.at.UTC().Format(time.DateOnly)}
		for index, step := range selected.Plan.Steps {
			if !slices.Contains(step.Types, typ) || step.Kind == "retire-action" {
				continue
			}
			var previous platform.MigrationRecordResult
			if before != nil {
				var err error
				previous, err = program.Transform(index, typ, jsonOf(before), context)
				if err != nil {
					return nil, nil, nil, err
				}
				before = previous.Values
			}
			next, err := program.Transform(index, typ, jsonOf(after), context)
			if err != nil {
				return nil, nil, nil, err
			}
			after = next.Values
			if step.Kind == "extract" && next.Selected {
				if previous.Selected && string(previous.Extracted["id"]) != string(next.Extracted["id"]) {
					return nil, nil, nil, fmt.Errorf("retained extraction cannot change its declared record identity")
				}
				child := *r
				child.release, child.shapes = selected.Plan.To, targets
				a, b, nested, err := child.mapRetainedWrite(me, step.Target, previous.Extracted, next.Extracted)
				if err != nil {
					return nil, nil, nil, err
				}
				writes = append(writes, nested...)
				var id string
				_ = json.Unmarshal(after["id"], &id)
				writes = append(writes, retainedExtractionWrite{typ: step.Target, before: a, after: b, origin: records.Extraction{Migration: selected.ID, Source: typ + "/" + id, Step: index}})
				for _, field := range step.Fields {
					convertedSources[field.From] = true
				}
			}
			if step.Kind == "split" || step.Kind == "merge" {
				for _, field := range next.Retired {
					convertedSources[field] = true
				}
			}
		}
		release = selected.Plan.To
		declared = targets
	}
}

func (t *Tenant) releaseShapes(id string) (map[string]platform.EntityInfo, error) {
	sealed, err := t.releases.candidate(id)
	if err != nil {
		return nil, err
	}
	return t.releaseAssetShapes(sealed.Assets)
}

func (t *Tenant) releaseAssetShapes(available []platform.ReleaseAsset) (map[string]platform.EntityInfo, error) {
	shapes := map[string]platform.EntityInfo{}
	for _, owner := range t.apps {
		compiler, ok := owner.(host.ReleaseExecutionOwner)
		if !ok {
			continue
		}
		assets := []platform.ReleaseAsset{}
		for _, asset := range available {
			if asset.Ref.App == owner.Manifest().ID {
				assets = append(assets, asset)
			}
		}
		if len(assets) == 0 {
			continue
		}
		bound, err := compiler.ForkReleaseExecution(assets)
		if err != nil {
			return nil, err
		}
		for _, entity := range bound.Manifest().Entities {
			if !slices.ContainsFunc(assets, func(a platform.ReleaseAsset) bool {
				return a.Ref.Kind == platform.AssetObject && a.Ref.Name == entity.Type
			}) {
				continue
			}
			info, err := platform.Describe(owner.Manifest().ID, entity, func(reflect.Type) string { return "" })
			if err != nil {
				return nil, err
			}
			shapes[info.Type] = info
		}
	}
	return shapes, nil
}
