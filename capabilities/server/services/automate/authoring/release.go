package authoring

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func (o *Owner) PrepareReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return o.PrepareProcessPublications(assets)
}

func (o *Owner) PrepareProcessPublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	records, err := o.Inventory()
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != o.env.AppID() || asset.Ref.Kind != platform.AssetFlow {
			continue
		}
		i := slices.IndexFunc(records, func(process Process) bool {
			return !process.Archived && o.QualifiedName(process.Name) == asset.Ref.Name
		})
		if i < 0 {
			return nil, fmt.Errorf("saved flow %s has no matching owner", asset.Ref)
		}
		frozen, err := o.ProcessFromAsset(asset)
		if err != nil {
			return nil, err
		}
		if prior, ok := readPublished[Process](records[i].Published); ok {
			priorAsset, err := o.processReleaseAsset(prior, asset.SourceVersion)
			if err == nil {
				var a, z any
				if json.Unmarshal(priorAsset.Body, &a) == nil && json.Unmarshal(asset.Body, &z) == nil && reflect.DeepEqual(a, z) {
					continue
				}
			}
		}
		if records[i].Version >= 64 {
			return nil, fmt.Errorf("flow version family is full")
		}
		frozen.Record, frozen.Version, frozen.State = records[i].Record, records[i].Version+1, "published"
		frozen.Layout = records[i].Layout
		records[i].Version, records[i].State = frozen.Version, "published"
		records[i].Published = processImage(frozen)
		records[i].Versions = append(slices.Clone(records[i].Versions), records[i].Published)
		raw, err := json.Marshal(records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ReleasePublication{Schema: SchemaProcess, Image: raw})
	}
	return out, nil
}

func (o *Owner) ProcessAssets(processes []Process, sourceVersion string) ([]platform.ReleaseAsset, error) {
	var assets []platform.ReleaseAsset
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		saved, err := ReadProcessImage(image)
		if err != nil {
			return nil, err
		}
		asset, err := o.processReleaseAsset(saved, sourceVersion)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (o *Owner) DraftAsset(record Process, sourceVersion string) (platform.ReleaseAsset, error) {
	if record.Version >= 64 {
		return platform.ReleaseAsset{}, fmt.Errorf("flow version family is full")
	}
	record.Version++
	record.State = "published"
	record.Published = processImage(record)
	record.Versions = append(slices.Clone(record.Versions), record.Published)
	return o.processReleaseAsset(record, sourceVersion)
}

func (o *Owner) FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error) {
	processes, err := o.Inventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		if _, err := ReadProcessImage(image); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range record.Versions {
			p, ok := readPublished[Process](raw)
			if ok && p.Name == name && p.Version == version {
				return o.processReleaseAsset(p, o.env.Version())
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("process %s version %d is not retained", name, version)
}

func (o *Owner) ProcessFromAsset(asset platform.ReleaseAsset) (Process, error) {
	var envelope platform.FlowReleaseDescriptor
	var p Process
	if asset.Ref.App != o.env.AppID() || asset.Ref.Kind != platform.AssetFlow || asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil {
		return p, fmt.Errorf("unsupported process release asset %s", asset.Ref)
	}
	if err := json.Unmarshal(asset.Body, &envelope); err != nil {
		return p, err
	}
	if err := json.Unmarshal(envelope.Definition, &p); err != nil {
		return p, err
	}
	expected, err := o.processReleaseAsset(p, asset.SourceVersion)
	if err != nil {
		return p, err
	}
	var got, want any
	if json.Unmarshal(asset.Body, &got) != nil || json.Unmarshal(expected.Body, &want) != nil || expected.Ref != asset.Ref || !reflect.DeepEqual(got, want) {
		return p, fmt.Errorf("process %s definition and compiled bindings differ", asset.Ref)
	}
	p.ID, p.Version, p.State = asset.Ref.Name, 1, "published"
	p.Published = processImage(p)
	p.Versions = []string{p.Published}
	return p, nil
}

func (o *Owner) processReleaseAsset(saved Process, sourceVersion string) (platform.ReleaseAsset, error) {
	ref := platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetFlow, Name: o.QualifiedName(saved.Name)}
	var subject platform.AssetRef
	if saved.Object != "" {
		owner, _, _ := strings.Cut(saved.Object, ".")
		subject = platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: saved.Object}
	}
	var actions []platform.AssetRef
	var functions []platform.AssetBinding
	var operations []platform.AssetBinding
	var queries []platform.AssetBinding
	var dependencies []platform.AssetRef
	for _, step := range saved.Steps {
		if step.Act != "" {
			name := step.Act
			if !strings.Contains(name, ".") {
				name = saved.Object + "." + name
			}
			owner := step.App
			if owner == "" {
				owner, _, _ = strings.Cut(name, ".")
			}
			actions = append(actions, platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: name})
			if step.Protocol != "" {
				protocol, _, _ := strings.Cut(step.Protocol, "/")
				dependencies = append(dependencies, platform.AssetRef{App: owner, Kind: platform.AssetProtocol, Name: protocol})
			}
		}
		if step.Function != nil {
			owner := step.Function.App
			if owner == "" {
				owner = o.env.AppID()
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: step.Function.Name}
			if owner != o.env.AppID() || step.Function.Version == 0 {
				dependencies = append(dependencies, ref)
				continue
			}
			binding := platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".function-" + strconv.Itoa(step.Function.Version)}
			if !slices.Contains(functions, binding) {
				functions = append(functions, binding)
			}
		}
		if step.Operation != nil {
			owner := step.Operation.App
			if owner == "" {
				owner = o.env.AppID()
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetCompute, Name: step.Operation.Name}
			if owner == o.env.AppID() && step.Operation.Version > 0 {
				operations = append(operations, platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".compute-" + strconv.Itoa(step.Operation.Version)})
			} else {
				dependencies = append(dependencies, ref)
			}
		}
		if step.Query != "" {
			ref := platform.AssetRef{App: step.App, Kind: platform.AssetQuery, Name: step.Query}
			if step.App == o.env.AppID() && step.QueryVersion > 0 {
				queries = append(queries, platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".query-" + strconv.Itoa(step.QueryVersion)})
			} else {
				dependencies = append(dependencies, ref)
			}
		}
		if step.Flow != "" {
			name := step.Flow
			if !strings.HasPrefix(name, o.env.AppID()+".") {
				name = o.QualifiedName(name)
			}
			dependencies = append(dependencies, platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetFlow, Name: name})
		}
	}
	slices.SortFunc(operations, func(a, b platform.AssetBinding) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	operations = slices.Compact(operations)
	slices.SortFunc(queries, func(a, b platform.AssetBinding) int {
		return strings.Compare(a.Ref.String()+"@"+a.SourceVersion, b.Ref.String()+"@"+b.SourceVersion)
	})
	queries = slices.Compact(queries)
	slices.SortFunc(dependencies, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	dependencies = slices.Compact(dependencies)
	slices.SortFunc(functions, func(a, b platform.AssetBinding) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	slices.SortFunc(actions, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	actions = slices.Compact(actions)
	definition := json.RawMessage(processImage(saved))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(definition, &fields); err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "version", "layout", "paused"} {
		delete(fields, key)
	}
	definition, err := json.Marshal(fields)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(platform.FlowReleaseDescriptor{Name: ref.Name, Subject: subject, Actions: actions, Definition: definition, Functions: functions, Operations: operations, Queries: queries, Dependencies: dependencies})
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

func (o *Owner) Draft(asset platform.ReleaseAsset) (string, any, bool, error) {
	switch asset.Ref.Kind {
	case platform.AssetFlow:
		p, err := o.ProcessFromAsset(asset)
		if err != nil {
			return "", nil, true, err
		}
		return ProcessType, p, true, nil
	}
	return "", nil, false, nil
}

func (o *Owner) DecodeInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	switch schema {
	case SchemaProcess:
		var record struct {
			platform.Record
			Name string `json:"name"`
		}
		if json.Unmarshal(image, &record) != nil || record.ID == "" || record.Name == "" {
			return platform.AssetInstallation{}, fmt.Errorf("invalid release publication row")
		}
		return platform.AssetInstallation{
			Ref:    platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetFlow, Name: o.QualifiedName(record.Name)},
			Schema: schema, Record: ProcessType + "/" + record.ID, Image: slices.Clone(image),
		}, nil
	}
	return platform.AssetInstallation{}, fmt.Errorf("not an automation installation schema")
}

func (o *Owner) ValidateInstallation(plan platform.AssetInstallation) error {
	decoded, err := o.DecodeInstallation(plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("installation identity differs from its saved image")
	}
	return o.ValidateImage(plan.Schema, plan.Image)
}

func (o *Owner) ValidateImage(schema string, image []byte) error {
	switch schema {
	case SchemaProcess:
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if o.env.Host().BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return o.validate(p)
	}
	return fmt.Errorf("not an automation publication schema")
}

func (o *Owner) ApplyImage(schema string, image []byte) error {
	switch schema {
	case SchemaProcess:
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if o.env.Host().BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return o.Install(platform.Caller{Replaying: true}, p)
	}
	return fmt.Errorf("not an automation publication schema")
}

func (o *Owner) HandlesPublication(schema string) bool {
	return schema == SchemaProcess
}
