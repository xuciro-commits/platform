package authoring

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func (o *Owner) codeInventory() ([]Code, error) {
	return publication.Inventory[Code](o.env.Host().Automation(platform.Caller{}, o.env.App()))
}
func (o *Owner) installCode(c platform.Caller, code Code) error {
	if code.Published != "" {
		published, ok := publication.Read[Code](code.Published)
		if !ok {
			return fmt.Errorf("compute has no valid publication")
		}
		code = published
	}
	if err := o.env.Host().InstallOperation(c, code.Definition(), code.Version); err != nil {
		return err
	}
	o.codes[code.Name] = code
	return nil
}
func (o *Owner) OperationDefinition(name string, version int) (platform.Operation, int, bool) {
	if current, ok := o.codes[name]; ok && (version == 0 || version == current.Version) {
		return current.Definition(), current.Version, true
	}
	if tb, ok := o.tables[name]; ok && (version == 0 || version == tb.Version) {
		return tb.Definition(), tb.Version, true
	}
	for _, tb := range o.tables {
		for _, raw := range tb.Versions {
			prior, ok := publication.Read[Table](raw)
			if ok && prior.Name == name && prior.Version == version {
				return prior.Definition(), version, true
			}
		}
	}
	list, err := o.codeInventory()
	if err != nil {
		return platform.Operation{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		prior, ok := publication.Read[Code](record.Versions[version-1])
		if ok && prior.Name == name && prior.Version == version {
			return prior.Definition(), version, true
		}
	}
	return platform.Operation{}, 0, false
}
func (o *Owner) OperationDeclarations() []platform.Operation {
	out := make([]platform.Operation, 0, len(o.codes)+len(o.tables))
	for _, name := range slices.Sorted(maps.Keys(o.codes)) {
		out = append(out, o.codes[name].Definition())
	}
	for _, name := range slices.Sorted(maps.Keys(o.tables)) {
		out = append(out, o.tables[name].Definition())
	}
	return out
}

func (o *Owner) codeReleaseAsset(code Code, manifest string) (platform.ReleaseAsset, error) {
	raw, err := json.Marshal(code.Definition())
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return o.resolver.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: code.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(code.Version), Body: raw})
}
func (o *Owner) CodeReleaseAssets() ([]platform.ReleaseAsset, error) {
	out := []platform.ReleaseAsset{}
	for _, op := range o.OperationDeclarations() {
		if op.Binding.Kind == "native" {
			asset, err := o.tableReleaseAsset(o.tables[op.Name], o.env.Version())
			if err != nil {
				return nil, err
			}
			out = append(out, asset)
			continue
		}
		asset, err := o.codeReleaseAsset(o.codes[op.Name], o.env.Version())
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}
func (o *Owner) OperationReleaseAsset(name, version string) (platform.ReleaseAsset, error) {
	text, ok := strings.CutPrefix(version, o.env.Version()+".compute-")
	ordinal, err := strconv.Atoi(text)
	op, actual, found := o.OperationDefinition(name, ordinal)
	if !ok || err != nil || strconv.Itoa(ordinal) != text || !found || actual != ordinal || ordinal < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("compute version is not retained")
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return o.resolver.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: name}, ContractVersion: 1, SourceVersion: version, Body: raw})
}
func (o *Owner) tableReleaseAsset(table Table, manifest string) (platform.ReleaseAsset, error) {
	return o.resolver.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: table.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(table.Version), Body: platform.Raw(table.Definition())})
}

func (o *Owner) installTable(c platform.Caller, tb Table) error {
	if err := o.env.Host().InstallOperation(c, tb.Definition(), tb.Version); err != nil {
		return err
	}
	o.tables[tb.Name] = tb
	return nil
}

func (o *Owner) Compute(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	tb, ok := o.tables[name]
	if !ok {
		return nil, fmt.Errorf("the builder has no native operation %s", name)
	}
	var values map[string]any
	if err := json.Unmarshal(input, &values); err != nil {
		return nil, fmt.Errorf("the input is not an object")
	}
	plan, err := tb.RulePlan()
	if err != nil {
		return nil, err
	}
	legacy := len(tb.Rows) > 0 && len(tb.Rows[0].Expressions) == 0
	return plan.Decide(TableInput(plan.Reads, legacy, values), func(string) (platform.EntityInfo, bool) { return platform.EntityInfo{}, false })
}
