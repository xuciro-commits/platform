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

func (o *Owner) AlertAssets() ([]platform.ReleaseAsset, error) {
	rows, err := o.alertInventory()
	if err != nil {
		return nil, err
	}
	var assets []platform.ReleaseAsset
	for _, a := range rows {
		if a.Archived {
			continue
		}
		if err := o.freezeLegacyAlert(&a); err != nil {
			return nil, err
		}
		if a.Published == "" {
			continue
		}
		_, asset, err := o.alertImage(platform.Raw(a))
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (o *Owner) AlertPinned(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	rows, err := o.alertInventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		if err := o.freezeLegacyAlert(&record); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range append(slices.Clone(record.Versions), record.Published) {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == version {
				_, _, err := o.alertFromAsset(asset)
				return asset, err
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("alert %s@%s is not retained", ref, version)
}

func (o *Owner) AlertDraftAssets(before []platform.ReleaseAsset, id string) (after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	a, ok := platform.Get[Alert](o.env.Host().Automation(platform.Caller{}, o.env.AppID()), id)
	if !ok || a.Archived {
		return nil, prior, next, false, fmt.Errorf("alert draft %s was not found", id)
	}
	if err = o.freezeLegacyAlert(&a); err != nil {
		return
	}
	if a.Published != "" {
		_, asset, problem := o.alertImage(platform.Raw(a))
		if problem != nil {
			err = problem
			return
		}
		prior, hadPrior = asset.Ref, true
	}
	if err = o.checkAlert(o.env.Host().Automation(platform.Caller{}, o.env.AppID()), a); err != nil {
		return
	}
	next = platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetAlert, Name: a.Name}
	asset, problem := o.alertReleaseAsset(a, a.Version+1)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(candidate platform.ReleaseAsset) bool {
		return candidate.Ref == next || hadPrior && candidate.Ref == prior
	})
	after = append(after, asset)
	return
}

func (o *Owner) AlertAsset(alert Alert, version int) (platform.ReleaseAsset, error) {
	return o.alertReleaseAsset(alert, version)
}

func (o *Owner) PrepareReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	processes, err := o.PrepareProcessPublications(assets)
	if err != nil {
		return nil, err
	}
	alerts, err := o.prepareAlertPublications(assets)
	if err != nil {
		return nil, err
	}
	return append(processes, alerts...), nil
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
				a, _ := upgradedProcessBody(priorAsset.Body)
				z, _ := upgradedProcessBody(asset.Body)
				if reflect.DeepEqual(a, z) {
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

func (o *Owner) prepareAlertPublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := o.alertInventory()
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != o.env.AppID() || asset.Ref.Kind != platform.AssetAlert {
			continue
		}
		frozen, version, err := o.alertFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(a Alert) bool { return !a.Archived && a.Name == frozen.Name })
		if i < 0 {
			return nil, fmt.Errorf("alert %s has no target draft", asset.Ref)
		}
		record := rows[i]
		if err := o.freezeLegacyAlert(&record); err != nil {
			return nil, err
		}
		if version <= record.Version {
			retained, err := o.AlertPinned(asset.Ref, asset.SourceVersion)
			if err != nil || !sameRuleAsset(retained, asset) {
				return nil, fmt.Errorf("retained alert bytes differ")
			}
		} else if record.Version != 0 && version != record.Version+1 {
			return nil, fmt.Errorf("alert needs its next version or retained bytes")
		}
		if err := o.retainAlertAsset(&record, asset); err != nil {
			return nil, err
		}
		out = append(out, platform.ReleasePublication{Schema: SchemaAlertPublish, Image: platform.Raw(record)})
	}
	return out, nil
}

func (o *Owner) alertInventory() ([]Alert, error) {
	c := o.env.Host().Automation(platform.Caller{}, o.env.AppID())
	return readInventory[Alert](c, "alert")
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
	got, err := upgradedProcessBody(asset.Body)
	var want any
	if err != nil || json.Unmarshal(expected.Body, &want) != nil || expected.Ref != asset.Ref || !reflect.DeepEqual(got, want) {
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

func upgradedProcessBody(raw json.RawMessage) (any, error) {
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	var walk func(any) error
	walk = func(value any) error {
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				if condition, ok := child.(map[string]any); ok && key == "condition" && (condition["left"] != nil || condition["terms"] != nil) {
					legacy, _ := json.Marshal(condition)
					upgraded, err := platform.UpgradeCondition(legacy)
					if err != nil {
						return err
					}
					current, _ := json.Marshal(upgraded)
					var decoded any
					_ = json.Unmarshal(current, &decoded)
					node[key] = decoded
					continue
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return body, walk(body)
}

func (o *Owner) Draft(asset platform.ReleaseAsset) (string, any, bool, error) {
	switch asset.Ref.Kind {
	case platform.AssetFlow:
		p, err := o.ProcessFromAsset(asset)
		if err != nil {
			return "", nil, true, err
		}
		return ProcessType, p, true, nil
	case platform.AssetAlert:
		alert, _, err := o.alertFromAsset(asset)
		alert.State = "draft"
		return AlertType, alert, true, err
	}
	return "", nil, false, nil
}

func (o *Owner) Definition(asset platform.ReleaseAsset) (*platform.AlertRule, bool, error) {
	if asset.Ref.Kind != platform.AssetAlert {
		return nil, false, nil
	}
	var alert platform.AlertRule
	if json.Unmarshal(asset.Body, &alert) != nil || alert.Check() != nil {
		return nil, true, fmt.Errorf("invalid accepted alert rule")
	}
	return &alert, true, nil
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
	case SchemaAlertPublish:
		_, asset, err := o.alertImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		var record struct {
			platform.Record
			Name string `json:"name"`
		}
		if json.Unmarshal(image, &record) != nil || record.ID == "" {
			return platform.AssetInstallation{}, fmt.Errorf("invalid release publication row")
		}
		return platform.AssetInstallation{Ref: asset.Ref, Schema: schema, Record: AlertType + "/" + record.ID, Image: slices.Clone(image)}, nil
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
	case SchemaProcess, schemaAlertMigrate:
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if o.env.Host().BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return o.validate(p)
	case SchemaAlertPublish:
		_, asset, err := o.alertImage(image)
		if err != nil {
			return err
		}
		alert, _, err := o.alertFromAsset(asset)
		if err != nil {
			return err
		}
		return o.checkAlert(o.env.Host().Automation(platform.Caller{}, o.env.AppID()), alert)
	}
	return fmt.Errorf("not an automation publication schema")
}

func (o *Owner) ApplyImage(schema string, image []byte) error {
	switch schema {
	case SchemaProcess, schemaAlertMigrate:
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if o.env.Host().BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return o.Install(platform.Caller{Replaying: true}, p)
	case SchemaAlertPublish:
		_, _, err := o.alertImage(image)
		return err
	}
	return fmt.Errorf("not an automation publication schema")
}

func (o *Owner) HandlesPublication(schema string) bool {
	return schema == SchemaProcess || schema == SchemaAlertPublish || schema == schemaAlertMigrate || schema == schemaAlertDraftMigrate
}
