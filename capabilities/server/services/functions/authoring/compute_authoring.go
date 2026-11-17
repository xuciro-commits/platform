package authoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

const SchemaTablePublish = TableType + ".publish"

func (o *Owner) checkCode(code Code) error {
	if code.Module == "" || code.SourceHash != code.SourceDigest() || code.BuildHash != code.BuildDigest() || code.Language != "go" && code.Language != "tinygo" || code.Toolchain == "" {
		return fmt.Errorf("code %s needs an unchanged isolated build", code.Name)
	}
	if err := o.env.Host().ValidateInstallOperation(platform.Caller{}, code.Definition()); err != nil {
		return err
	}
	list, err := o.codeInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		previous, _ := publication.Read[Code](other.Published)
		if other.ID != code.ID && (other.Name == code.Name || previous.Name == code.Name) {
			return fmt.Errorf("compute %s is already declared", code.Name)
		}
	}
	if old, ok := publication.Read[Code](code.Published); ok && old.Name != code.Name {
		return fmt.Errorf("a published compute keeps its name")
	}
	return nil
}

func (o *Owner) InstallOperationAsset(asset platform.ReleaseAsset) error {
	var op platform.Operation
	text, ok := strings.CutPrefix(asset.SourceVersion, o.env.Version()+".compute-")
	ordinal, err := strconv.Atoi(text)
	if !ok || err != nil || ordinal < 1 || ordinal > 64 || asset.Ref.App != o.env.App() || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Check() != nil || o.resolver.CheckAssetDependencies(asset) != nil {
		return fmt.Errorf("invalid compute candidate asset")
	}
	if op.Binding.Kind == "native" {
		table, err := o.tableFromAsset(asset)
		if err != nil {
			return err
		}
		return o.installTable(platform.Caller{Replaying: true}, table)
	}
	code := Code{Name: op.Name, Title: op.Title, Description: op.Description, Input: op.Input, Output: op.Output, Roles: op.Roles, Limits: op.Limits, Module: op.Binding.Module, State: "published", Version: ordinal}
	return o.installCode(platform.Caller{Replaying: true}, code)
}
func (o *Owner) codeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = o.env.AllReleaseAssets()
	if err != nil {
		return
	}
	list, problem := o.codeInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(code Code) bool { return code.ID == id && !code.Archived })
	if i < 0 {
		err = fmt.Errorf("compute draft was not found")
		return
	}
	code := list[i]
	if old, ok := publication.Read[Code](code.Published); ok {
		prior = platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: code.Name}
	if err = o.checkCode(code); err != nil {
		return
	}
	if code.Version >= 64 {
		err = fmt.Errorf("compute version family is full")
		return
	}
	code.Version++
	asset, problem := o.codeReleaseAsset(code, o.env.Version())
	if problem != nil {
		err = problem
		return
	}
	after = slices.Clone(before)
	after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (o *Owner) prepareCodeReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	list, err := o.codeInventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != o.env.App() || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) == nil && operation.Binding.Kind == "native" {
			continue
		}
		i := slices.IndexFunc(list, func(code Code) bool { return code.Name == asset.Ref.Name && !code.Archived })
		if i < 0 {
			return nil, fmt.Errorf("compute has no source owner")
		}
		record := list[i]
		var op platform.Operation
		if json.Unmarshal(asset.Body, &op) != nil || op.Check() != nil {
			return nil, fmt.Errorf("compute descriptor is invalid")
		}
		text, ok := strings.CutPrefix(asset.SourceVersion, o.env.Version()+".compute-")
		ordinal, err := strconv.Atoi(text)
		if !ok || err != nil || ordinal < 1 || ordinal > 64 {
			return nil, fmt.Errorf("compute version is invalid")
		}
		if ordinal <= record.Version {
			retained, err := o.OperationReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			if err != nil || !publication.EqualAsset(retained, asset) {
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
			out = append(out, platform.ReleasePublication{Schema: SchemaCodePublish, Image: platform.Raw(record)})
			continue
		}
		built := Code{ABI: record.BuiltABI, Language: record.BuiltLanguage, Source: record.BuiltSource, Input: record.BuiltInput, Output: record.BuiltOutput}
		if ordinal != record.Version+1 || record.Module != op.Binding.Module || record.SourceHash != built.SourceDigest() || record.BuildHash != built.BuildDigest() {
			return nil, fmt.Errorf("compute source changed after candidate construction")
		}
		frozen := record
		frozen.ABI = record.BuiltABI
		frozen.Source, frozen.Language = record.BuiltSource, record.BuiltLanguage
		frozen.Input, frozen.Output, frozen.Roles, frozen.Limits = op.Input, op.Output, op.Roles, op.Limits
		frozen.Title, frozen.Description, frozen.Version, frozen.State = op.Title, op.Description, ordinal, "published"
		image := frozen
		image.Published, image.Versions = "", nil
		frozen.Published = string(platform.Raw(image))
		record.Version, record.State, record.Published = ordinal, "published", frozen.Published
		record.Versions = append(record.Versions, record.Published)
		raw, _ := json.Marshal(record)
		out = append(out, platform.ReleasePublication{Schema: SchemaCodePublish, Image: raw})
	}
	return out, nil
}

func (o *Owner) tableFromAsset(asset platform.ReleaseAsset) (Table, error) {
	var op platform.Operation
	text, found := strings.CutPrefix(asset.SourceVersion, o.env.Version()+".compute-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != o.env.App() || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Binding.Kind != "native" || op.Rules == nil || op.Check() != nil || o.resolver.CheckAssetDependencies(asset) != nil {
		return Table{}, fmt.Errorf("invalid decision table asset %s", asset.Ref)
	}
	tb := Table{Name: op.Name, Title: op.Title, Description: op.Description, Reads: &op.Rules.Reads, State: "published", Version: version}
	columns := func(schema platform.ValueSchema) ([]TableColumn, error) {
		var columns []TableColumn
		for _, name := range schema.Required {
			field := schema.Properties[name]
			kind := map[string]string{"string": "text", "number": "number", "boolean": "boolean"}[field.Type]
			if kind == "" {
				return nil, fmt.Errorf("table column %s has an unsupported schema", name)
			}
			columns = append(columns, TableColumn{Name: name, Title: field.Description, Type: kind})
		}
		if len(columns) == 0 || len(columns) != len(schema.Properties) {
			return nil, fmt.Errorf("table columns need ordered, required schema fields")
		}
		return columns, nil
	}
	if tb.Inputs, err = columns(op.Input); err != nil {
		return Table{}, err
	}
	if tb.Outputs, err = columns(op.Output); err != nil {
		return Table{}, err
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
				return Table{}, fmt.Errorf("table row does not match its columns")
			}
			expressions = row.Condition.Args
		}
		then, err := results(row.Result)
		if err != nil {
			return Table{}, err
		}
		tb.Rows = append(tb.Rows, TableRow{Expressions: expressions, Then: then})
	}
	if len(op.Rules.Default) > 0 {
		if tb.Default, err = results(op.Rules.Default); err != nil {
			return Table{}, err
		}
	}
	if !bytes.Equal(platform.Raw(tb.Definition()), platform.Raw(op)) {
		return Table{}, fmt.Errorf("table operation cannot be represented by its original editor")
	}
	return tb, nil
}

func (o *Owner) tableDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = o.env.AllReleaseAssets()
	if err != nil {
		return
	}
	tb, ok := platform.Get[Table](o.env.Host().Automation(platform.Caller{}, o.env.App()), id)
	if !ok || tb.Archived {
		err = fmt.Errorf("decision table draft %s was not found", id)
		return
	}
	if old, ok := publication.Read[Table](tb.Published); ok {
		prior, hadPrior = platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: old.Name}, true
	}
	if err = tb.NormalizeRules(); err == nil {
		err = o.checkTable(tb)
	}
	if err != nil {
		return
	}
	if tb.Version >= 64 {
		err = fmt.Errorf("table version family is full")
		return
	}
	tb.Version++
	next = platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: tb.Name}
	asset, problem := o.tableReleaseAsset(tb, o.env.Version())
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(asset platform.ReleaseAsset) bool { return asset.Ref == next || hadPrior && asset.Ref == prior })
	after = append(after, asset)
	return
}

func (o *Owner) prepareTablePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := publication.Inventory[Table](o.env.Host().Automation(platform.Caller{}, o.env.App()))
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != o.env.App() || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) != nil || operation.Binding.Kind != "native" {
			continue
		}
		frozen, err := o.tableFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(tb Table) bool { return !tb.Archived && tb.Name == frozen.Name })
		if i < 0 {
			return nil, fmt.Errorf("table %s has no target draft", asset.Ref)
		}
		record := rows[i]
		if frozen.Version <= record.Version {
			retained, err := o.OperationReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			var prior, selected any
			if err != nil || json.Unmarshal(platform.Raw(retained), &prior) != nil || json.Unmarshal(platform.Raw(asset), &selected) != nil || !reflect.DeepEqual(prior, selected) {
				return nil, fmt.Errorf("retained table bytes differ")
			}
		} else if record.Version != 0 && frozen.Version != record.Version+1 {
			return nil, fmt.Errorf("table needs its next version or retained bytes")
		}
		frozen.Record = record.Record
		raw := string(platform.Raw(frozen))
		for _, retained := range record.Versions {
			if prior, ok := publication.Read[Table](retained); ok && prior.Version == frozen.Version {
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

func (o *Owner) TableEntity() platform.Entity {
	return platform.Entity{Type: TableType, Title: "Decision table", Plural: "Decision tables", Model: Table{}, Display: "title", Description: "Condition columns to result columns; the first matching row decides. Published as a native operation.",
		Validate: func(c platform.Caller, record any) *platform.Error {
			if c.Replaying {
				return nil
			}
			tb := record.(*Table)
			if err := tb.NormalizeRules(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if err := CheckTableShape(*tb, o.env.ConditionLookup, o.env.CheckRuleCollections, c.Rules()); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder"}, Capability: "compute"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Description: "Check the table and install it as an operation at once.",
				Roles:       []string{"builder"},
				Capability:  "compute",
				Parameters:  []platform.Field{},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules:       []platform.ActionRule{{Function: o.publishTable}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (o *Owner) publishTable(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	tb, ok := record.(*Table)
	if !ok {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if err := tb.NormalizeRules(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if err := o.checkTable(*tb, c.Rules()); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if tb.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A table may retain at most 64 published versions")
	}
	tb.Version++
	frozen := *tb
	frozen.Published = ""
	frozen.Versions = nil
	if c.Staging() {
		if err := o.env.Host().ValidateInstallOperation(c, frozen.Definition()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := o.installTable(c, frozen); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	raw, _ := json.Marshal(frozen)
	if tb.Published != "" && !slices.Contains(tb.Versions, tb.Published) {
		tb.Versions = append(tb.Versions, tb.Published)
	}
	tb.Published = string(raw)
	tb.Versions = append(tb.Versions, tb.Published)
	return nil
}

func (o *Owner) RestoreTables() error {
	list, err := publication.Inventory[Table](o.env.Host().Automation(platform.Caller{}, o.env.App()))
	if err != nil {
		return err
	}
	for _, record := range list {
		if record.Published == "" || record.Archived {
			continue
		}
		tb, ok := publication.Read[Table](record.Published)
		if !ok || tb.Name != record.Name {
			return fmt.Errorf("decision table %s has a malformed publication", record.Name)
		}
		tb.Versions = record.Versions
		if err := o.installTable(platform.Caller{Replaying: true}, tb); err != nil {
			return err
		}
	}
	return nil
}

func (o *Owner) checkTable(tb Table, engines ...platform.RuleEngine) error {
	if err := o.env.CheckName(tb.Name, tb.ID); err != nil {
		return err
	}
	return CheckTableShape(tb, o.env.ConditionLookup, o.env.CheckRuleCollections, engines...)
}
