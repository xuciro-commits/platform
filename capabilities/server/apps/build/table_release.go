package build

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func tableReleaseAsset(table construction.Table, manifest string) (platform.ReleaseAsset, error) {
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetCompute, Name: table.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(table.Version), Body: platform.Raw(table.Definition())})
}

// A table's operation carries the complete rule plan. Reconstruct the editor
// columns from its ordered schema and its rows from that same plan.
func tableFromAsset(asset platform.ReleaseAsset) (construction.Table, error) {
	var op platform.Operation
	text, found := strings.CutPrefix(asset.SourceVersion, construction.ValueDefinitionVersion+".compute-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Binding.Kind != "native" || op.Rules == nil || op.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return construction.Table{}, fmt.Errorf("invalid decision table asset %s", asset.Ref)
	}
	tb := construction.Table{Name: op.Name, Title: op.Title, Description: op.Description, Reads: &op.Rules.Reads, State: "published", Version: version}
	columns := func(schema platform.ValueSchema) ([]construction.TableColumn, error) {
		var columns []construction.TableColumn
		for _, name := range schema.Required {
			field := schema.Properties[name]
			kind := map[string]string{"string": "text", "number": "number", "boolean": "boolean"}[field.Type]
			if kind == "" {
				return nil, fmt.Errorf("table column %s has an unsupported schema", name)
			}
			columns = append(columns, construction.TableColumn{Name: name, Title: field.Description, Type: kind})
		}
		if len(columns) == 0 || len(columns) != len(schema.Properties) {
			return nil, fmt.Errorf("table columns need ordered, required schema fields")
		}
		return columns, nil
	}
	if tb.Inputs, err = columns(op.Input); err != nil {
		return construction.Table{}, err
	}
	if tb.Outputs, err = columns(op.Output); err != nil {
		return construction.Table{}, err
	}
	results := func(raw json.RawMessage) ([]string, error) {
		var values map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&values); err != nil {
			return nil, err
		}
		out := make([]string, 0, len(tb.Outputs))
		for _, col := range tb.Outputs {
			value, exists := values[col.Name]
			if !exists {
				return nil, fmt.Errorf("table result is missing %s", col.Name)
			}
			out = append(out, fmt.Sprint(value))
		}
		return out, nil
	}
	for _, row := range op.Rules.Rows {
		expressions := []platform.Expr{row.Condition}
		if len(tb.Inputs) > 1 {
			if row.Condition.Op != "and" || len(row.Condition.Args) != len(tb.Inputs) {
				return construction.Table{}, fmt.Errorf("table row does not match its columns")
			}
			expressions = row.Condition.Args
		}
		then, err := results(row.Result)
		if err != nil {
			return construction.Table{}, err
		}
		tb.Rows = append(tb.Rows, construction.TableRow{Expressions: expressions, Then: then})
	}
	if len(op.Rules.Default) > 0 {
		if tb.Default, err = results(op.Rules.Default); err != nil {
			return construction.Table{}, err
		}
	}
	if !bytes.Equal(platform.Raw(tb.Definition()), platform.Raw(op)) {
		return construction.Table{}, fmt.Errorf("table operation cannot be represented by its original editor")
	}
	return tb, nil
}

func (b *Build) tableDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	tb, ok := platform.Get[construction.Table](b.host.Automation(platform.Caller{}, construction.ID), id)
	if !ok || tb.Archived {
		err = fmt.Errorf("decision table draft %s was not found", id)
		return
	}
	if old, ok := wasPublished[construction.Table](tb.Published); ok {
		prior, hadPrior = platform.AssetRef{App: construction.ID, Kind: platform.AssetCompute, Name: old.Name}, true
	}
	if err = tb.NormalizeRules(); err == nil {
		err = b.checkTable(tb)
	}
	if err != nil {
		return
	}
	if tb.Version >= 64 {
		err = fmt.Errorf("table version family is full")
		return
	}
	tb.Version++
	next = platform.AssetRef{App: construction.ID, Kind: platform.AssetCompute, Name: tb.Name}
	asset, problem := tableReleaseAsset(tb, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(asset platform.ReleaseAsset) bool { return asset.Ref == next || hadPrior && asset.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareTablePublications(assets []platform.ReleaseAsset) ([]construction.ReleasePublication, error) {
	rows, err := readDefinitionInventory[construction.Table](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return nil, err
	}
	var out []construction.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) != nil || operation.Binding.Kind != "native" {
			continue
		}
		frozen, err := tableFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(tb construction.Table) bool { return !tb.Archived && tb.Name == frozen.Name })
		if i < 0 {
			return nil, fmt.Errorf("table %s has no target draft", asset.Ref)
		}
		record := rows[i]
		if frozen.Version <= record.Version {
			retained, err := b.OperationReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			if err != nil || !sameRuleAsset(retained, asset) {
				return nil, fmt.Errorf("retained table bytes differ")
			}
		} else if record.Version != 0 && frozen.Version != record.Version+1 {
			return nil, fmt.Errorf("table needs its next version or retained bytes")
		}
		frozen.Record = record.Record
		raw := published(frozen)
		for _, retained := range record.Versions {
			if prior, ok := wasPublished[construction.Table](retained); ok && prior.Version == frozen.Version {
				raw = retained
				break
			}
		}
		if !slices.Contains(record.Versions, raw) {
			record.Versions = append(slices.Clone(record.Versions), raw)
		}
		record.Version, record.State, record.Published = max(record.Version, frozen.Version), "published", raw
		out = append(out, construction.ReleasePublication{Schema: SchemaTablePublish, Image: platform.Raw(record)})
	}
	return out, nil
}
