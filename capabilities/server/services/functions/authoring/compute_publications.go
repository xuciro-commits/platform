package authoring

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func (o *Owner) ComputePublication() publication.Hooks {
	return publication.Hooks{
		Kinds: []platform.AssetKind{platform.AssetCompute},
		Schemas: []publication.SchemaHooks{
			{Name: SchemaCodePublish, Validate: o.validateCodePublication, Install: o.installCodePublication},
			{Name: SchemaTablePublish, Decode: o.decodeTableInstallation, Validate: o.validateTablePublication, Install: o.installTablePublication},
		},
		FreezeBatch: o.prepareComputePublications,
		VersionedDraft: func(id string) (closure publication.DraftClosure, err error) {
			if _, ok := platform.Get[Table](o.env.Host().Automation(platform.Caller{}, o.env.App()), id); ok {
				closure.Before, closure.After, closure.Prior, closure.Next, closure.HasPrior, err = o.tableDraftAssets(id)
			} else {
				closure.Before, closure.After, closure.Prior, closure.Next, closure.HasPrior, err = o.codeDraftAssets(id)
			}
			return closure, err
		},
		ReadFrozen: func(image json.RawMessage) (json.RawMessage, error) {
			var record struct{ Published string }
			if err := json.Unmarshal(image, &record); err != nil {
				return nil, err
			}
			return json.RawMessage(record.Published), nil
		},
	}
}

func (o *Owner) prepareComputePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	code, err := o.prepareCodeReleasePublications(assets)
	if err != nil {
		return nil, err
	}
	tables, err := o.prepareTablePublications(assets)
	if err != nil {
		return nil, err
	}
	return append(code, tables...), nil
}

func (o *Owner) validateCodePublication(image json.RawMessage) error {
	code, err := decodeCodePublication(image)
	if err != nil {
		return err
	}
	return o.env.Host().ValidateInstallOperation(platform.Caller{Replaying: true}, code.Definition())
}

func (o *Owner) installCodePublication(image json.RawMessage) error {
	code, err := decodeCodePublication(image)
	if err != nil {
		return err
	}
	return o.installCode(platform.Caller{Replaying: true}, code)
}

func (o *Owner) decodeTableInstallation(image json.RawMessage) (platform.AssetRef, error) {
	table, err := decodeTablePublication(image)
	return platform.AssetRef{App: o.env.App(), Kind: platform.AssetCompute, Name: table.Name}, err
}

func (o *Owner) validateTablePublication(image json.RawMessage) error {
	table, err := decodeTablePublication(image)
	if err != nil {
		return err
	}
	return o.env.Host().ValidateInstallOperation(platform.Caller{Replaying: true}, table.Definition())
}

func (o *Owner) installTablePublication(image json.RawMessage) error {
	table, err := decodeTablePublication(image)
	if err != nil {
		return err
	}
	var record Table
	if err := json.Unmarshal(image, &record); err != nil {
		return err
	}
	table.Versions = record.Versions
	return o.installTable(platform.Caller{Replaying: true}, table)
}

func (o *Owner) RestoreCodes() error {
	codes, err := o.codeInventory()
	if err != nil {
		return err
	}
	for _, record := range codes {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		code, err := decodeCodePublication(raw)
		if err != nil {
			return err
		}
		if err := o.installCode(platform.Caller{Replaying: true}, code); err != nil {
			return err
		}
	}
	return nil
}

func (o *Owner) OperationDraft(asset platform.ReleaseAsset) (string, any, error) {
	var operation platform.Operation
	if json.Unmarshal(asset.Body, &operation) == nil && operation.Binding.Kind == "native" {
		table, err := o.tableFromAsset(asset)
		table.State, table.Version = "draft", 0
		return TableType, table, err
	}
	rows, err := o.codeInventory()
	if err != nil {
		return "", nil, err
	}
	for _, record := range rows {
		if record.Name == asset.Ref.Name && !record.Archived {
			return CodeType, record, nil
		}
	}
	return "", nil, fmt.Errorf("compute %s needs its original compiled target owner; a module descriptor does not carry source or compilation evidence", asset.Ref)
}

func decodeCodePublication(image []byte) (Code, error) {
	var code Code
	if json.Unmarshal(image, &code) != nil || !slices.Contains([]string{"draft", "published"}, code.State) || code.Version < 1 || code.Version > 64 || len(code.Versions) != code.Version || !slices.Contains(code.Versions, code.Published) {
		return code, fmt.Errorf("invalid compute publication family")
	}
	var selected Code
	for i, raw := range code.Versions {
		prior, ok := publication.Read[Code](raw)
		if !ok || prior.ID != code.ID || prior.Name != code.Name || prior.Version != i+1 || prior.Definition().Check() != nil || prior.SourceHash != prior.SourceDigest() || prior.Published != "" || len(prior.Versions) > 0 {
			return code, fmt.Errorf("invalid retained compute version")
		}
		if raw == code.Published {
			selected = prior
		}
	}
	selected.Versions = code.Versions
	return selected, nil
}
func decodeTablePublication(image []byte) (Table, error) {
	var record Table
	if err := json.Unmarshal(image, &record); err != nil {
		return record, err
	}
	frozen, ok := publication.Read[Table](record.Published)
	if !ok || record.State != "published" || frozen.ID != record.ID || frozen.Name != record.Name || frozen.Version < 1 || frozen.Version > record.Version || record.Version > 64 {
		return frozen, fmt.Errorf("accepted table has no matching immutable publication")
	}
	return frozen, nil
}
