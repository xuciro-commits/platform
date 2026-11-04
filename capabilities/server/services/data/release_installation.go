package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"platformserver/platform"
)

func (o *Owner) EnvironmentRequirements(asset platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	if !ValueIntegrationKind(asset.Ref.Kind) {
		return nil, nil
	}
	var requirement platform.AssetEnvironmentRequirement
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var definition ConnectionDefinition
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		if definition.Name != asset.Ref.Name || !slices.Contains(ConnectionKinds, definition.Kind) {
			return nil, fmt.Errorf("invalid connection binding descriptor")
		}
		requirement = platform.AssetEnvironmentRequirement{Ref: asset.Ref, Profile: definition.Kind, Credential: definition.Credential}
	case platform.AssetSource:
		var definition SourceDefinition
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		kind := "http"
		if definition.Profile == "odata" {
			kind = "odata"
		}
		if definition.Profile == "table" {
			kind = "postgres"
		}
		if definition.Connection == "" {
			return nil, fmt.Errorf("source has no environment connection")
		}
		requirement = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: App, Kind: platform.AssetConnection, Name: definition.Connection}, Profile: kind}
	case platform.AssetObservation:
		var definition platform.ObservationDefinition
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		if definition.Connection.Kind != platform.AssetConnection || definition.Connection.Name == "" {
			return nil, fmt.Errorf("observation source has no environment connection")
		}
		requirement = platform.AssetEnvironmentRequirement{Ref: definition.Connection, Profile: "http"}
	case platform.AssetWriteback:
		var definition WritebackDefinition
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		if definition.Connection == "" {
			return nil, fmt.Errorf("writeback has no environment connection")
		}
		requirement = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: App, Kind: platform.AssetConnection, Name: definition.Connection}}
	default:
		return nil, nil
	}
	return []platform.AssetEnvironmentRequirement{requirement}, nil
}

func (o *Owner) integrationTargets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) (func(), error) {
	saved := o.releaseDraftIDs
	o.releaseDraftIDs = maps.Clone(saved)
	if o.releaseDraftIDs == nil {
		o.releaseDraftIDs = map[platform.AssetRef]string{}
	}
	restore := func() { o.releaseDraftIDs = saved }
	for _, asset := range assets {
		if asset.Ref.App != App || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		rows, err := o.integrationInventory(asset.Ref.Kind)
		if err != nil {
			restore()
			return nil, err
		}
		id, found := asset.Ref.Name, false
		for _, row := range rows {
			name, _, _, _ := integrationMetadata(row)
			if name != asset.Ref.Name {
				continue
			}
			if found {
				restore()
				return nil, fmt.Errorf("integration name %s is ambiguous", name)
			}
			found, id = true, integrationRecord(row).ID
			if connection, ok := row.(Connection); ok {
				var definition ConnectionDefinition
				if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
					restore()
					return nil, err
				}
				if definition.Kind != connection.Kind {
					restore()
					return nil, fmt.Errorf("connection %s has an incompatible local kind", asset.Ref)
				}
			}
		}
		o.releaseDraftIDs[asset.Ref] = id
	}
	for ref, id := range ctx.DraftIDs {
		o.releaseDraftIDs[ref] = id
	}
	return restore, nil
}

func (o *Owner) BeginTargets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) (func(), error) {
	return o.integrationTargets(assets, ctx)
}

func (o *Owner) IntegrationDrafts(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	restore, err := o.integrationTargets(assets, ctx)
	if err != nil {
		return nil, err
	}
	defer restore()
	var out []platform.AssetDraft
	for _, asset := range assets {
		if asset.Ref.App != App || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		var record any
		if asset.Ref.Kind == platform.AssetConnection {
			connection, err := ConnectionDraft(asset)
			if err != nil {
				return nil, err
			}
			record = connection
		} else {
			configured, err := o.integrationConfiguration(asset)
			if err != nil {
				return nil, err
			}
			record = configured
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		typ := integrationType(asset.Ref.Kind)
		name := asset.Ref.Name
		if id := ctx.DraftIDs[asset.Ref]; id != "" {
			name = id
		}
		out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: typ, Name: name, Schema: typ + ".create", Image: raw})
	}
	return out, nil
}

func (o *Owner) Prepare(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
	restore, err := o.integrationTargets(assets, ctx)
	if err != nil {
		return nil, err
	}
	defer restore()
	out := []platform.AssetInstallation{}
	for _, asset := range assets {
		if asset.Ref.App != App || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		rows, err := o.integrationInventory(asset.Ref.Kind)
		if err != nil {
			return nil, err
		}
		var current any
		for _, row := range rows {
			name, _, _, _ := integrationMetadata(row)
			if name == asset.Ref.Name {
				current = row
				break
			}
		}
		if current == nil {
			return nil, fmt.Errorf("asset %s has no target draft", asset.Ref)
		}
		record, err := retainIntegration(current, asset)
		if err != nil {
			return nil, err
		}
		if entry := integrationEntryOfRecord(record); entry != nil && entry.publish != nil {
			record, err = entry.publish(o, record, asset, ctx)
			if err != nil {
				return nil, err
			}
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if before, _ := json.Marshal(current); bytes.Equal(before, raw) {
			continue
		}
		descriptor := integrationDescriptor(asset.Ref.Kind)
		out = append(out, platform.AssetInstallation{Ref: asset.Ref, Schema: descriptor.publication(), Record: descriptor.record(integrationRecord(record).ID), Image: raw})
	}
	if !ctx.DraftsOnly {
		for _, plan := range out {
			if entry := integrationEntryOfKind(plan.Ref.Kind); entry != nil && entry.requiresToken && (!ctx.Member.Active() || !ctx.Member.InScope(plan.Schema)) {
				return nil, fmt.Errorf("release publication token does not permit %s", plan.Schema)
			}
		}
	}
	return out, nil
}

func (o *Owner) DecodeInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	resource, known := integrationResourceOfSchema(schema)
	if !known {
		return platform.AssetInstallation{}, fmt.Errorf("invalid publication schema")
	}
	record, err := integrationInstallationRecord(resource.kind, image)
	if err != nil {
		return platform.AssetInstallation{}, err
	}
	name, version, raw, _ := integrationMetadata(record)
	var asset platform.ReleaseAsset
	if integrationRecord(record).ID == "" || version < 1 || ValueDecodeIntegration([]byte(raw), &asset) != nil || asset.Ref != (platform.AssetRef{App: App, Kind: resource.kind, Name: name}) {
		return platform.AssetInstallation{}, fmt.Errorf("invalid integration installation")
	}
	if err := validateIntegrationFamily(record, asset); err != nil {
		return platform.AssetInstallation{}, err
	}
	if err := ValueValidateIntegrationDescriptor(asset); err != nil {
		return platform.AssetInstallation{}, err
	}
	return platform.AssetInstallation{Ref: asset.Ref, Schema: schema, Record: resource.record(integrationRecord(record).ID), Image: slices.Clone(image)}, nil
}

func integrationResourceOfSchema(schema string) (integrationResource, bool) {
	for _, resource := range integrationResources {
		if resource.publication() == schema {
			return resource, true
		}
	}
	return integrationResource{}, false
}

func (o *Owner) ValidateInstallation(plan platform.AssetInstallation) error {
	decoded, err := o.DecodeInstallation(plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("installation identity differs from its saved image")
	}
	record, err := integrationInstallationRecord(plan.Ref.Kind, plan.Image)
	if err != nil {
		return err
	}
	c := o.host.Automation(platform.Caller{}, App)
	switch r := record.(type) {
	case Connection:
		_, err = o.ConnectionConfiguration(r)
	case platform.MediaSet:
		_, err = o.acceptedIntegration(r, platform.AssetMediaSet)
	case platform.Schedule:
		fixed, e := o.ExecutionSchedule(r)
		if e != nil {
			return e
		}
		if e := o.scheduleEntity().Validate(c, &fixed); e != nil {
			return e
		}
	case Dataset:
		_, err = o.ExecutionDataset(r)
	case Listener:
		_, err = o.ExecutionListener(r)
	case Source:
		fixed, e := o.ExecutionSource(r)
		if e != nil {
			return e
		}
		if e := o.checkSource(c, fixed); e != nil {
			return e
		}
	case Pipeline:
		fixed, e := o.ExecutionPipeline(r)
		if e != nil {
			return e
		}
		if e := o.checkPipeline(c, fixed); e != nil {
			return e
		}
	case Writeback:
		fixed, e := o.ExecutionWriteback(r)
		if e != nil {
			return e
		}
		if e := o.checkWriteback(c, fixed); e != nil {
			return e
		}
	case Observation:
		if _, e := o.ExecutionObservation(r); e != nil {
			return e
		}
		configuration, e := o.acceptedIntegration(r, platform.AssetObservation)
		if e != nil {
			return e
		}
		fixed := configuration.(Observation)
		fixed.Record, fixed.State, fixed.Publisher = r.Record, r.State, r.Publisher
		fixed.PublisherScopes = r.PublisherScopes
		fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
		err = o.checkObservation(c, fixed)
	case Match:
		fixed, e := o.ExecutionMatching(r)
		if e != nil {
			return e
		}
		if e := o.checkMatch(c, fixed); e != nil {
			return e
		}
	}
	return err
}

func (o *Owner) ApplyInstallation(plan platform.AssetInstallation, ctx platform.AssetApplyContext) error {
	if ctx.Validate && ctx.Upgrade {
		if err := o.validateIntegrationUpgrade(plan); err != nil {
			return err
		}
	}
	if plan.Ref.Kind == platform.AssetMediaSet {
		var set platform.MediaSet
		var asset platform.ReleaseAsset
		var definition MediaSetDefinition
		if err := json.Unmarshal(plan.Image, &set); err != nil {
			return err
		}
		if err := ValueDecodeIntegration([]byte(set.Published), &asset); err != nil {
			return err
		}
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return err
		}
		files, ok := o.files.(platform.MediaFiles)
		if !ok {
			return fmt.Errorf("Media content storage is unavailable")
		}
		for hash, raw := range definition.Blobs {
			if err := files.StoreMedia(hash, "application/octet-stream", raw); err != nil {
				return err
			}
		}
	}
	_, err := o.DecodeInstallation(plan.Schema, plan.Image)
	return err
}

func (o *Owner) validateIntegrationUpgrade(plan platform.AssetInstallation) error {
	if !ValueIntegrationKind(plan.Ref.Kind) {
		return nil
	}
	proposed, err := integrationInstallationRecord(plan.Ref.Kind, plan.Image)
	if err != nil {
		return err
	}
	rows, err := o.integrationInventory(plan.Ref.Kind)
	if err != nil {
		return err
	}
	var current any
	for _, row := range rows {
		if integrationRecord(row).ID == integrationRecord(proposed).ID {
			current = row
			break
		}
	}
	if current == nil {
		return nil
	}
	switch old := current.(type) {
	case Source:
		next, e := o.ExecutionSource(proposed.(Source))
		if e != nil {
			return e
		}
		prior, e := o.ExecutionSource(old)
		if e != nil {
			return e
		}
		if (old.Cursor != "" || old.Last != nil) && (prior.Connection != next.Connection || prior.Profile != next.Profile || prior.URL != next.URL || prior.Filter != next.Filter || prior.Path != next.Path || prior.Stream != next.Stream || prior.Entity != next.Entity || prior.Since != next.Since || prior.Dataset != next.Dataset || prior.Object != next.Object || prior.Key != next.Key || !reflect.DeepEqual(prior.Mapping, next.Mapping)) {
			return fmt.Errorf("source with accepted data or cursor needs an explicit migration before changing its target or identity mapping")
		}
	case Pipeline:
		next, e := o.ExecutionPipeline(proposed.(Pipeline))
		if e != nil {
			return e
		}
		prior, e := o.ExecutionPipeline(old)
		if e != nil {
			return e
		}
		if old.Last != nil && (prior.Input != next.Input || prior.OutputDataset != next.OutputDataset || prior.OutputObject != next.OutputObject || prior.Key != next.Key) {
			return fmt.Errorf("pipeline with accepted data needs an explicit migration before changing its input or output identity")
		}
	}
	return nil
}

func integrationInstallationRecord(kind platform.AssetKind, raw []byte) (any, error) {
	typ := integrationRecordType(kind)
	if typ == nil {
		return nil, fmt.Errorf("unknown integration kind")
	}
	ptr := reflect.New(typ)
	if err := ValueDecodeIntegration(raw, ptr.Interface()); err != nil {
		return nil, err
	}
	return ptr.Elem().Interface(), nil
}

func ConnectionDraft(asset platform.ReleaseAsset) (Connection, error) {
	var definition ConnectionDefinition
	if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
		return Connection{}, err
	}
	return Connection{Name: definition.Name, Title: definition.Title, Kind: definition.Kind, Marking: definition.Marking, State: "draft"}, nil
}
