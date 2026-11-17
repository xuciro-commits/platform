package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	functionauthoring "platformserver/services/functions/authoring"
	"platformserver/services/resources/publication"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (b *Build) Answer(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error {
	if strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint) {
		return b.answered(c, effect, out, now)
	}
	return b.functions.AnswerCompilation(c, b, effect, out, now)
}
func (b *Build) codeInventory() ([]functionauthoring.Code, error) {
	return readDefinitionInventory[functionauthoring.Code](b.host.Automation(platform.Caller{}, ID))
}
func (b *Build) checkCode(code functionauthoring.Code) error {
	if code.Module == "" || code.SourceHash != code.SourceDigest() || code.BuildHash != code.BuildDigest() || code.Language != "go" && code.Language != "tinygo" || code.Toolchain == "" {
		return fmt.Errorf("code %s needs an unchanged isolated build", code.Name)
	}
	if err := b.host.ValidateInstallOperation(platform.Caller{}, code.Definition()); err != nil {
		return err
	}
	list, err := b.codeInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		previous, _ := publication.Read[functionauthoring.Code](other.Published)
		if other.ID != code.ID && (other.Name == code.Name || previous.Name == code.Name) {
			return fmt.Errorf("compute %s is already declared", code.Name)
		}
	}
	if old, ok := publication.Read[functionauthoring.Code](code.Published); ok && old.Name != code.Name {
		return fmt.Errorf("a published compute keeps its name")
	}
	return nil
}
func (b *Build) installCode(c platform.Caller, code functionauthoring.Code) error {
	if code.Published != "" {
		published, ok := publication.Read[functionauthoring.Code](code.Published)
		if !ok {
			return fmt.Errorf("compute has no valid publication")
		}
		code = published
	}
	if err := b.host.InstallOperation(c, code.Definition(), code.Version); err != nil {
		return err
	}
	b.codes[code.Name] = code
	return nil
}
func (b *Build) OperationDefinition(name string, version int) (platform.Operation, int, bool) {
	if current, ok := b.codes[name]; ok && (version == 0 || version == current.Version) {
		return current.Definition(), current.Version, true
	}
	if tb, ok := b.tables[name]; ok && (version == 0 || version == tb.Version) {
		return tb.Definition(), tb.Version, true
	}
	for _, tb := range b.tables {
		for _, raw := range tb.Versions {
			prior, ok := publication.Read[functionauthoring.Table](raw)
			if ok && prior.Name == name && prior.Version == version {
				return prior.Definition(), version, true
			}
		}
	}
	list, err := b.codeInventory()
	if err != nil {
		return platform.Operation{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		prior, ok := publication.Read[functionauthoring.Code](record.Versions[version-1])
		if ok && prior.Name == name && prior.Version == version {
			return prior.Definition(), version, true
		}
	}
	return platform.Operation{}, 0, false
}
func (b *Build) operationDeclarations() []platform.Operation {
	return append(definitionDeclarations(b.codes), definitionDeclarations(b.tables)...)
}
func (b *Build) codeReleaseAsset(code functionauthoring.Code, manifest string) (platform.ReleaseAsset, error) {
	raw, err := json.Marshal(code.Definition())
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return b.resolver.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: code.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(code.Version), Body: raw})
}
func (b *Build) CodeReleaseAssets() ([]platform.ReleaseAsset, error) {
	out := []platform.ReleaseAsset{}
	for _, op := range b.operationDeclarations() {
		if op.Binding.Kind == "native" {
			asset, err := b.tableReleaseAsset(b.tables[op.Name], b.Manifest().Version)
			if err != nil {
				return nil, err
			}
			out = append(out, asset)
			continue
		}
		asset, err := b.codeReleaseAsset(b.codes[op.Name], b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}
func (b *Build) OperationReleaseAsset(name, version string) (platform.ReleaseAsset, error) {
	text, ok := strings.CutPrefix(version, b.Manifest().Version+".compute-")
	ordinal, err := strconv.Atoi(text)
	op, actual, found := b.OperationDefinition(name, ordinal)
	if !ok || err != nil || strconv.Itoa(ordinal) != text || !found || actual != ordinal || ordinal < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("compute version is not retained")
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return b.resolver.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: name}, ContractVersion: 1, SourceVersion: version, Body: raw})
}
func (b *Build) InstallOperationAsset(asset platform.ReleaseAsset) error {
	var op platform.Operation
	text, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".compute-")
	ordinal, err := strconv.Atoi(text)
	if !ok || err != nil || ordinal < 1 || ordinal > 64 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Check() != nil || b.resolver.CheckAssetDependencies(asset) != nil {
		return fmt.Errorf("invalid compute candidate asset")
	}
	if op.Binding.Kind == "native" {
		table, err := b.tableFromAsset(asset)
		if err != nil {
			return err
		}
		return b.installTable(platform.Caller{Replaying: true}, table)
	}
	code := functionauthoring.Code{Name: op.Name, Title: op.Title, Description: op.Description, Input: op.Input, Output: op.Output, Roles: op.Roles, Limits: op.Limits, Module: op.Binding.Module, State: "published", Version: ordinal}
	return b.installCode(platform.Caller{Replaying: true}, code)
}
func (b *Build) CodeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	list, problem := b.codeInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(code functionauthoring.Code) bool { return code.ID == id && !code.Archived })
	if i < 0 {
		err = fmt.Errorf("compute draft was not found")
		return
	}
	code := list[i]
	if old, ok := publication.Read[functionauthoring.Code](code.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: code.Name}
	if err = b.checkCode(code); err != nil {
		return
	}
	if code.Version >= 64 {
		err = fmt.Errorf("compute version family is full")
		return
	}
	code.Version++
	asset, problem := b.codeReleaseAsset(code, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.Clone(before)
	after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) PrepareCodeReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	list, err := b.codeInventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) == nil && operation.Binding.Kind == "native" {
			continue
		}
		i := slices.IndexFunc(list, func(code functionauthoring.Code) bool { return code.Name == asset.Ref.Name && !code.Archived })
		if i < 0 {
			return nil, fmt.Errorf("compute has no source owner")
		}
		record := list[i]
		var op platform.Operation
		if json.Unmarshal(asset.Body, &op) != nil || op.Check() != nil {
			return nil, fmt.Errorf("compute descriptor is invalid")
		}
		text, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".compute-")
		ordinal, err := strconv.Atoi(text)
		if !ok || err != nil || ordinal < 1 || ordinal > 64 {
			return nil, fmt.Errorf("compute version is invalid")
		}
		if ordinal <= record.Version {
			retained, err := b.OperationReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			if err != nil || !b.data.EqualAsset(retained, asset) {
				return nil, fmt.Errorf("retained compute bytes differ")
			}
			if len(record.Versions) != record.Version {
				return nil, fmt.Errorf("retained compute history is unavailable")
			}
			selected := record.Versions[ordinal-1]
			if selected == record.Published {
				continue
			}
			record.Published = selected
			if ordinal < record.Version {
				record.State = "draft"
			}
			out = append(out, platform.ReleasePublication{Schema: functionauthoring.SchemaCodePublish, Image: platform.Raw(record)})
			continue
		}
		built := functionauthoring.Code{ABI: record.BuiltABI, Language: record.BuiltLanguage, Source: record.BuiltSource, Input: record.BuiltInput, Output: record.BuiltOutput}
		if ordinal != record.Version+1 || record.Module != op.Binding.Module || record.SourceHash != built.SourceDigest() || record.BuildHash != built.BuildDigest() {
			return nil, fmt.Errorf("compute source changed after candidate construction")
		}
		frozen := record
		frozen.ABI = record.BuiltABI
		frozen.Source, frozen.Language = record.BuiltSource, record.BuiltLanguage
		frozen.Input, frozen.Output, frozen.Roles, frozen.Limits = op.Input, op.Output, op.Roles, op.Limits
		frozen.Title, frozen.Description, frozen.Version, frozen.State = op.Title, op.Description, ordinal, "published"
		frozen.Published = published(frozen)
		record.Version, record.State, record.Published = ordinal, "published", frozen.Published
		record.Versions = append(record.Versions, record.Published)
		raw, _ := json.Marshal(record)
		out = append(out, platform.ReleasePublication{Schema: functionauthoring.SchemaCodePublish, Image: raw})
	}
	return out, nil
}

func (b *Build) tableReleaseAsset(table functionauthoring.Table, manifest string) (platform.ReleaseAsset, error) {
	return b.resolver.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: table.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(table.Version), Body: platform.Raw(table.Definition())})
}

func (b *Build) tableFromAsset(asset platform.ReleaseAsset) (functionauthoring.Table, error) {
	var op platform.Operation
	text, found := strings.CutPrefix(asset.SourceVersion, ValueDefinitionVersion+".compute-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Binding.Kind != "native" || op.Rules == nil || op.Check() != nil || b.resolver.CheckAssetDependencies(asset) != nil {
		return functionauthoring.Table{}, fmt.Errorf("invalid decision table asset %s", asset.Ref)
	}
	tb := functionauthoring.Table{Name: op.Name, Title: op.Title, Description: op.Description, Reads: &op.Rules.Reads, State: "published", Version: version}
	columns := func(schema platform.ValueSchema) ([]functionauthoring.TableColumn, error) {
		var columns []functionauthoring.TableColumn
		for _, name := range schema.Required {
			field := schema.Properties[name]
			kind := map[string]string{"string": "text", "number": "number", "boolean": "boolean"}[field.Type]
			if kind == "" {
				return nil, fmt.Errorf("table column %s has an unsupported schema", name)
			}
			columns = append(columns, functionauthoring.TableColumn{Name: name, Title: field.Description, Type: kind})
		}
		if len(columns) == 0 || len(columns) != len(schema.Properties) {
			return nil, fmt.Errorf("table columns need ordered, required schema fields")
		}
		return columns, nil
	}
	if tb.Inputs, err = columns(op.Input); err != nil {
		return functionauthoring.Table{}, err
	}
	if tb.Outputs, err = columns(op.Output); err != nil {
		return functionauthoring.Table{}, err
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
				return functionauthoring.Table{}, fmt.Errorf("table row does not match its columns")
			}
			expressions = row.Condition.Args
		}
		then, err := results(row.Result)
		if err != nil {
			return functionauthoring.Table{}, err
		}
		tb.Rows = append(tb.Rows, functionauthoring.TableRow{Expressions: expressions, Then: then})
	}
	if len(op.Rules.Default) > 0 {
		if tb.Default, err = results(op.Rules.Default); err != nil {
			return functionauthoring.Table{}, err
		}
	}
	if !bytes.Equal(platform.Raw(tb.Definition()), platform.Raw(op)) {
		return functionauthoring.Table{}, fmt.Errorf("table operation cannot be represented by its original editor")
	}
	return tb, nil
}

func (b *Build) tableDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	tb, ok := platform.Get[functionauthoring.Table](b.host.Automation(platform.Caller{}, ID), id)
	if !ok || tb.Archived {
		err = fmt.Errorf("decision table draft %s was not found", id)
		return
	}
	if old, ok := publication.Read[functionauthoring.Table](tb.Published); ok {
		prior, hadPrior = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: old.Name}, true
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
	next = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: tb.Name}
	asset, problem := b.tableReleaseAsset(tb, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(asset platform.ReleaseAsset) bool { return asset.Ref == next || hadPrior && asset.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareTablePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := readDefinitionInventory[functionauthoring.Table](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) != nil || operation.Binding.Kind != "native" {
			continue
		}
		frozen, err := b.tableFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(tb functionauthoring.Table) bool { return !tb.Archived && tb.Name == frozen.Name })
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
			if prior, ok := publication.Read[functionauthoring.Table](retained); ok && prior.Version == frozen.Version {
				raw = retained
				break
			}
		}
		if !slices.Contains(record.Versions, raw) {
			record.Versions = append(slices.Clone(record.Versions), raw)
		}
		record.Version, record.State, record.Published = max(record.Version, frozen.Version), "published", raw
		out = append(out, platform.ReleasePublication{Schema: SchemaTablePublish, Image: platform.Raw(record)})
	}
	return out, nil
}
