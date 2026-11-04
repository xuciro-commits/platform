package data

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformkernel/kernel"
	"platformserver/platform"
)

func (o *Owner) integrationName(kind platform.AssetKind, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	list, err := o.integrationInventory(kind)
	if err != nil {
		return "", err
	}
	for _, r := range list {
		if integrationRecord(r).ID == id {
			name, _, _, _ := integrationMetadata(r)
			return name, nil
		}
	}
	for ref, planned := range o.releaseDraftIDs {
		if ref.App == App && ref.Kind == kind && planned == id {
			return ref.Name, nil
		}
	}
	return "", fmt.Errorf("integration %s/%s is unavailable", kind, id)
}

func (o *Owner) integrationID(kind platform.AssetKind, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	list, err := o.integrationInventory(kind)
	if err != nil {
		return "", err
	}
	var id string
	for _, r := range list {
		n, _, _, _ := integrationMetadata(r)
		if n == name {
			if id != "" {
				return "", fmt.Errorf("integration name %s is ambiguous", name)
			}
			id = integrationRecord(r).ID
		}
	}
	if id == "" {
		if planned := o.releaseDraftIDs[platform.AssetRef{App: App, Kind: kind, Name: name}]; planned != "" {
			return planned, nil
		}
		return "", fmt.Errorf("integration %s/%s has no local owner", kind, name)
	}
	return id, nil
}

func (o *Owner) IntegrationID(kind platform.AssetKind, name string) (string, error) {
	return o.integrationID(kind, name)
}

func (o *Owner) IntegrationName(kind platform.AssetKind, id string) (string, error) {
	return o.integrationName(kind, id)
}

func (o *Owner) Asset(kind platform.AssetKind, record any, ordinal int) (platform.ReleaseAsset, error) {
	return o.integrationAsset(kind, record, ordinal)
}

func (o *Owner) Prospective(kind platform.AssetKind, id string, declarations map[platform.AssetRef]platform.ReleaseAsset) (any, bool, error) {
	name, err := o.integrationName(kind, id)
	if err != nil {
		return nil, false, nil
	}
	asset, found := declarations[platform.AssetRef{App: App, Kind: kind, Name: name}]
	if !found {
		return nil, false, nil
	}
	configuration, err := o.integrationConfiguration(asset)
	return configuration, true, err
}

func (o *Owner) integrationAsset(kind platform.AssetKind, record any, ordinal int) (platform.ReleaseAsset, error) {
	asset := platform.ReleaseAsset{Ref: platform.AssetRef{App: App, Kind: kind}, ContractVersion: 1, SourceVersion: ValueDefinitionVersion + "." + string(kind) + "-" + strconv.Itoa(ordinal)}
	name, _, _, _ := integrationMetadata(record)
	asset.Ref.Name = name
	var body any
	objectBindings := func(typ string) map[string]platform.AssetRef {
		if typ == "" {
			return nil
		}
		owner, _, _ := strings.Cut(typ, ".")
		if info, ok := o.lookupEntity(typ); ok {
			owner = info.App
		}
		return map[string]platform.AssetRef{typ: {App: owner, Kind: platform.AssetObject, Name: typ}}
	}
	switch r := record.(type) {
	case Connection:
		body = ConnectionDefinition{Name: r.Name, Title: r.Title, Kind: r.Kind, Marking: r.Marking, Compartments: r.Compartments, Credential: r.Secret != ""}
	case Listener:
		body = ListenerDefinition{Name: r.Name, Title: r.Title, Connector: r.Connector, Path: r.Path, Secret: r.Secret}
	case Source:
		if r.Connection == "" || r.Header != "" || strings.Contains(r.URL, "://") || r.AllowPrivate {
			return asset, fmt.Errorf("portable source %s needs a connection and relative address without inline headers", r.Name)
		}
		conn, err := o.integrationName(platform.AssetConnection, r.Connection)
		if err != nil {
			return asset, err
		}
		dataset, err := o.integrationName(platform.AssetDataset, r.Dataset)
		if err != nil {
			return asset, err
		}
		body = SourceDefinition{ObjectBindings: objectBindings(r.Object), Name: r.Name, Title: r.Title, Connection: conn, Profile: r.Profile, URL: r.URL, Entity: r.Entity, Filter: r.Filter, Since: r.Since, Path: r.Path, Dataset: dataset, Object: r.Object, Key: r.Key, Mapping: r.Mapping, Stream: r.Stream, Every: r.Every}
	case platform.MediaSet:
		definition, err := o.mediaSetDefinition(r)
		if err != nil {
			return asset, err
		}
		body = definition
	case platform.Schedule:
		pipeline, err := o.integrationName(platform.AssetPipeline, r.Pipeline)
		if err != nil {
			return asset, err
		}
		body = ScheduleDefinition{Name: r.Name, Title: r.Title, Pipeline: pipeline, Every: r.Every, Enabled: r.Enabled}
	case Dataset:
		body = DatasetDefinition{Key: r.Key, Stream: r.Stream, Name: r.Name, Title: r.Title, Keep: r.Keep, Marking: r.Marking, Compartments: r.Compartments}
	case Pipeline:
		input, err := o.integrationName(platform.AssetDataset, r.Input)
		if err != nil {
			return asset, err
		}
		output, err := o.integrationName(platform.AssetDataset, r.OutputDataset)
		if err != nil {
			return asset, err
		}
		steps := slices.Clone(r.Steps)
		for i := range steps {
			if steps[i].Dataset != "" {
				steps[i].Dataset, err = o.integrationName(platform.AssetDataset, steps[i].Dataset)
				if err != nil {
					return asset, err
				}
			}
		}
		body = PipelineDefinition{ObjectBindings: objectBindings(r.OutputObject), Name: r.Name, Title: r.Title, Input: input, Steps: steps, Expectations: r.Expectations, OutputDataset: output, OutputObject: r.OutputObject, OutputEnterprise: r.OutputEnterprise, Key: r.Key, Every: r.Every}
		if r.OutputEnterprise != nil {
			return asset, fmt.Errorf("portable enterprise landing requires a separate owner migration and is unsupported")
		}
	case Writeback:
		conn, err := o.integrationName(platform.AssetConnection, r.Connection)
		if err != nil {
			return asset, err
		}
		body = WritebackDefinition{ObjectBindings: objectBindings(r.Object), Name: r.Name, Title: r.Title, Connection: conn, Object: r.Object, On: r.On, Method: r.Method, Transport: r.Transport, Path: r.Path, Mapping: r.Mapping, Result: r.Result}
	case Observation:
		d, err := o.observationDefinition(r)
		if err != nil {
			return asset, err
		}
		c := o.host.Automation(platform.Caller{}, App)
		if err := platform.CheckObservationDefinition(d, o.lookupEntity); err != nil {
			return asset, err
		}
		if d.Decision != nil {
			owner, action, ok := o.observationAction(c, d)
			if !ok || owner != d.Decision.Action.App || action.Target != d.Object.Name {
				return asset, fmt.Errorf("The observation decision must use an action on its declared object")
			}
			if ok {
				if err := platform.CheckObservationAction(d, action); err != nil {
					return asset, err
				}
			}
		}
		body = d
	case Match:
		producers := map[string]platform.AssetRef{}
		for _, preference := range r.Prefer {
			ref, err := o.matchingProducer(preference.Producer)
			if err != nil {
				return asset, err
			}
			producers[preference.Producer] = ref
		}
		body = MatchingDefinition{Name: r.Name, Title: r.Title, Object: r.Object, Keys: r.Keys, Prefer: r.Prefer, Producers: producers, ObjectBindings: objectBindings(r.Object)}
	default:
		return asset, fmt.Errorf("unknown integration descriptor")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return asset, err
	}
	asset.Body = raw
	return platform.WithDependencies(asset)
}

func integrationCandidateAsset(asset platform.ReleaseAsset) (platform.ReleaseAsset, error) {
	if asset.Provides != nil {
		return asset, nil
	}
	if asset.Ref.App != App || !ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
		return asset, fmt.Errorf("unsupported retained integration %s", asset.Ref)
	}
	if _, err := ValueIntegrationOrdinal(asset); err != nil {
		return asset, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(asset.Body, &fields); err != nil {
		return asset, err
	}
	var definition struct {
		Object       string       `json:"object"`
		OutputObject string       `json:"outputObject"`
		On           string       `json:"on"`
		Prefer       []Preference `json:"prefer"`
	}
	if err := json.Unmarshal(asset.Body, &definition); err != nil {
		return asset, err
	}
	for _, ref := range asset.Requires {
		if err := ref.Check(); err != nil {
			return asset, err
		}
	}
	resolve := func(name string, kinds ...platform.AssetKind) (platform.AssetRef, error) {
		var refs []platform.AssetRef
		for _, ref := range asset.Requires {
			if ref.Name == name && slices.Contains(kinds, ref.Kind) {
				refs = append(refs, ref)
			}
		}
		if len(refs) != 1 {
			return platform.AssetRef{}, fmt.Errorf("retained integration %s needs one frozen binding for %s", asset.Ref, name)
		}
		return refs[0], nil
	}
	bindings := map[string]platform.AssetRef{}
	for _, name := range []string{definition.Object, definition.OutputObject} {
		if name == "" {
			continue
		}
		ref, err := resolve(name, platform.AssetObject)
		if err != nil {
			return asset, err
		}
		bindings[name] = ref
	}
	producers := map[string]platform.AssetRef{}
	if asset.Ref.Kind == platform.AssetMatching {
		for _, preference := range definition.Prefer {
			ref, err := resolve(preference.Producer, platform.AssetSource, platform.AssetPipeline)
			if err != nil {
				return asset, err
			}
			if ref.App != asset.Ref.App {
				return asset, fmt.Errorf("retained matching %s has a different producer owner", asset.Ref)
			}
			producers[preference.Producer] = ref
		}
	}
	if asset.Ref.Kind == platform.AssetWriteback && definition.On == "confirmation" && slices.Contains(asset.Requires, platform.AssetRef{App: App, Kind: platform.AssetAction, Name: definition.Object + ".confirmation"}) {
		return asset, fmt.Errorf("writeback %s retains a retired confirmation action; publish a new writeback version before including it in a candidate", asset.Ref)
	}
	for key, refs := range map[string]map[string]platform.AssetRef{"objectBindings": bindings, "producers": producers} {
		if existing := fields[key]; len(existing) != 0 {
			var original map[string]platform.AssetRef
			if json.Unmarshal(existing, &original) != nil || !reflect.DeepEqual(original, refs) {
				return asset, fmt.Errorf("retained integration %s binding differs from its frozen references", asset.Ref)
			}
		}
		if len(refs) == 0 {
			continue
		}
		raw, err := json.Marshal(refs)
		if err != nil {
			return asset, err
		}
		fields[key] = raw
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return asset, err
	}
	projected := asset
	projected.Body = body
	projected, err = platform.WithDependencies(projected)
	if err != nil {
		return asset, err
	}
	requires := slices.Clone(asset.Requires)
	slices.SortFunc(requires, func(a, z platform.AssetRef) int { return strings.Compare(a.String(), z.String()) })
	if !slices.Equal(requires, projected.Requires) {
		return asset, fmt.Errorf("retained integration %s references differ from its original closure", asset.Ref)
	}
	if err := ValueValidateIntegrationDescriptor(projected); err != nil {
		return asset, err
	}
	return projected, nil
}

func (o *Owner) Assets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, entry := range integrationEntries {
		kind := entry.kind
		rows, err := o.integrationInventory(kind)
		if err != nil {
			return nil, err
		}
		for _, record := range rows {
			_, version, published, _ := integrationMetadata(record)
			if published != "" {
				var asset platform.ReleaseAsset
				if err := ValueDecodeIntegration([]byte(published), &asset); err != nil {
					return nil, err
				}
				asset, err = integrationCandidateAsset(asset)
				if err != nil {
					return nil, err
				}
				out = append(out, asset)
				continue
			}
			if version != 0 {
				return nil, fmt.Errorf("integration publication is missing")
			}
			installed := false
			switch r := record.(type) {
			case Connection:
				installed = r.State == "ready"
			case Listener:
				installed = r.State == "published"
			case Source:
				installed = r.State == "published"
			case platform.MediaSet, platform.Schedule:
				installed = true
			case Dataset:
				installed = true
			case Pipeline:
				installed = r.State == "published"
			case Writeback:
				installed = r.State == "published"
			case Match:
				installed = r.State == "published"
			}
			if !installed {
				continue
			}
			asset, err := o.integrationAsset(kind, record, 1)
			if err != nil {
				continue
			}
			out = append(out, asset)
		}
	}
	return out, nil
}

func (o *Owner) DraftAssets(before []platform.ReleaseAsset, kind platform.AssetKind, id string) (after []platform.ReleaseAsset, prior, next platform.AssetRef, had bool, err error) {
	rows, e := o.integrationInventory(kind)
	if e != nil {
		err = e
		return
	}
	var record any
	for _, r := range rows {
		if integrationRecord(r).ID == id {
			record = r
			break
		}
	}
	if record == nil {
		err = fmt.Errorf("integration draft %s/%s is missing", kind, id)
		return
	}
	c := o.host.Automation(platform.Caller{}, App)
	var problem *kernel.Error
	switch r := record.(type) {
	case Source:
		problem = o.checkSource(c, r)
	case Pipeline:
		problem = o.checkPipeline(c, r)
	case Writeback:
		problem = o.checkWriteback(c, r)
	case Match:
		problem = o.checkMatch(c, r)
	}
	if problem != nil {
		err = fmt.Errorf("integration draft %s/%s: %s", kind, id, problem.Message)
		return
	}
	_, version, published, _ := integrationMetadata(record)
	if published != "" {
		var old platform.ReleaseAsset
		if err = ValueDecodeIntegration([]byte(published), &old); err != nil {
			return
		}
		prior, had = old.Ref, true
	}
	if version >= 64 {
		err = fmt.Errorf("integration version family is full")
		return
	}
	asset, e := o.integrationAsset(kind, record, version+1)
	if e != nil {
		err = e
		return
	}
	next = asset.Ref
	if !had {
		for _, candidate := range before {
			if candidate.Ref == next {
				prior, had = next, true
				break
			}
		}
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || had && a.Ref == prior })
	after = append(after, asset)
	return
}

func (o *Owner) ResolvePinned(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	rows, err := o.integrationInventory(ref.Kind)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		name, ordinal, accepted, history := integrationMetadata(record)
		if name != ref.Name {
			continue
		}
		for _, raw := range append(slices.Clone(history), accepted) {
			if raw == "" {
				continue
			}
			var asset platform.ReleaseAsset
			if err := ValueDecodeIntegration([]byte(raw), &asset); err != nil {
				return asset, err
			}
			if asset.Ref == ref && asset.SourceVersion == version {
				return asset, nil
			}
		}
		if ordinal == 0 && accepted == "" && version == integrationVersion(1, ref.Kind) {
			return o.integrationAsset(ref.Kind, record, 1)
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("integration version %s@%s is not retained", ref, version)
}

func (o *Owner) integrationConfiguration(asset platform.ReleaseAsset) (any, error) {
	if asset.Ref.App != App || !ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
		return nil, fmt.Errorf("unknown integration asset %s", asset.Ref)
	}
	if _, err := ValueIntegrationOrdinal(asset); err != nil {
		return nil, err
	}
	if err := ValueValidateIntegrationDependencies(asset); err != nil {
		return nil, err
	}
	var record any
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var d ConnectionDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || !slices.Contains(ConnectionKinds, d.Kind) {
			return nil, fmt.Errorf("invalid connection definition")
		}
		record = Connection{Name: d.Name, Title: d.Title, Kind: d.Kind, Marking: d.Marking, Compartments: d.Compartments}
	case platform.AssetListener:
		var d ListenerDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if err := ValueValidateIntegrationDescriptor(asset); err != nil {
			return nil, err
		}
		record = Listener{Name: d.Name, Title: d.Title, Connector: d.Connector, Path: d.Path, Secret: d.Secret}
	case platform.AssetSource:
		var d SourceDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Connection == "" || strings.Contains(d.URL, "://") {
			return nil, fmt.Errorf("source requires a portable connection and relative address")
		}
		conn, err := o.integrationID(platform.AssetConnection, d.Connection)
		if err != nil {
			return nil, err
		}
		dataset, err := o.integrationID(platform.AssetDataset, d.Dataset)
		if err != nil {
			return nil, err
		}
		record = Source{Name: d.Name, Title: d.Title, Connection: conn, Profile: d.Profile, URL: d.URL, Entity: d.Entity, Filter: d.Filter, Since: d.Since, Path: d.Path, Dataset: dataset, Object: d.Object, Key: d.Key, Mapping: d.Mapping, Stream: d.Stream, Every: d.Every}
	case platform.AssetMediaSet:
		var d MediaSetDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		record = platform.MediaSet{Name: d.Name, Title: d.Title, Description: d.Description, Version: d.Version, Items: d.Items}
	case platform.AssetSchedule:
		var d ScheduleDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		pipeline, err := o.integrationID(platform.AssetPipeline, d.Pipeline)
		if err != nil {
			return nil, err
		}
		record = platform.Schedule{Name: d.Name, Title: d.Title, Pipeline: pipeline, Every: d.Every, Enabled: d.Enabled}
	case platform.AssetDataset:
		var d DatasetDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Keep < 0 {
			return nil, fmt.Errorf("invalid dataset definition")
		}
		record = Dataset{Key: d.Key, Stream: d.Stream, Name: d.Name, Title: d.Title, Keep: d.Keep, Marking: d.Marking, Compartments: d.Compartments}
	case platform.AssetPipeline:
		var d PipelineDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Input == "" || d.OutputEnterprise != nil {
			return nil, fmt.Errorf("invalid or unsupported pipeline definition")
		}
		input, err := o.integrationID(platform.AssetDataset, d.Input)
		if err != nil {
			return nil, err
		}
		output, err := o.integrationID(platform.AssetDataset, d.OutputDataset)
		if err != nil {
			return nil, err
		}
		for i := range d.Steps {
			if d.Steps[i].Dataset != "" {
				d.Steps[i].Dataset, err = o.integrationID(platform.AssetDataset, d.Steps[i].Dataset)
				if err != nil {
					return nil, err
				}
			}
		}
		record = Pipeline{Name: d.Name, Title: d.Title, Input: input, Steps: d.Steps, Expectations: d.Expectations, OutputDataset: output, OutputObject: d.OutputObject, Key: d.Key, Every: d.Every}
	case platform.AssetWriteback:
		var d WritebackDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Connection == "" || d.Object == "" || d.On == "" {
			return nil, fmt.Errorf("invalid writeback definition")
		}
		conn, err := o.integrationID(platform.AssetConnection, d.Connection)
		if err != nil {
			return nil, err
		}
		record = Writeback{Name: d.Name, Title: d.Title, Connection: conn, Object: d.Object, On: d.On, Method: d.Method, Transport: d.Transport, Path: d.Path, Mapping: d.Mapping, Result: d.Result}
	case platform.AssetObservation:
		var d platform.ObservationDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Check() != nil || d.Name != asset.Ref.Name || d.Connection.App != App {
			return nil, fmt.Errorf("invalid observation source definition")
		}
		conn, err := o.integrationID(platform.AssetConnection, d.Connection.Name)
		if err != nil {
			return nil, err
		}
		record = Observation{Name: d.Name, Title: d.Title, Description: d.Description, Connection: conn, Object: d.Object.Name, DeviceField: d.DeviceField, Signal: d.Signal, Unit: d.Unit, Path: d.Path, WindowSeconds: d.WindowSeconds, ExpectedRetentionSeconds: d.ExpectedRetentionSeconds, ExpectedGranularitySeconds: d.ExpectedGranularitySeconds, Roles: slices.Clone(d.Roles), Scheduled: d.Scheduled, RequireComplete: d.RequireComplete, Decision: d.Decision, Intervals: d.Intervals}
	case platform.AssetMatching:
		var d MatchingDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Object == "" {
			return nil, fmt.Errorf("invalid matching definition")
		}
		record = Match{Name: d.Name, Title: d.Title, Object: d.Object, Keys: d.Keys, Prefer: d.Prefer}
	}
	return record, nil
}

func (o *Owner) acceptedIntegration(record any, kind platform.AssetKind) (any, error) {
	_, version, raw, _ := integrationMetadata(record)
	if raw == "" {
		if version == 0 {
			return record, nil
		}
		return nil, fmt.Errorf("accepted integration definition is missing")
	}
	var asset platform.ReleaseAsset
	if err := ValueDecodeIntegration([]byte(raw), &asset); err != nil {
		return nil, err
	}
	if asset.Ref.Kind != kind {
		return nil, fmt.Errorf("accepted integration kind differs")
	}
	return o.integrationConfiguration(asset)
}
