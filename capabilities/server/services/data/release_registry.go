package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
)

type integrationResource struct {
	typ  string
	kind platform.AssetKind
}

func (r integrationResource) publication() string { return r.typ + ".publish" }
func (r integrationResource) record(id string) string {
	return r.typ + "/" + id
}

var integrationResources = []integrationResource{
	{ConnectionType, platform.AssetConnection},
	{ListenerType, platform.AssetListener},
	{SourceType, platform.AssetSource},
	{platform.MediaSetType, platform.AssetMediaSet},
	{DatasetType, platform.AssetDataset},
	{PipelineType, platform.AssetPipeline},
	{platform.ScheduleType, platform.AssetSchedule},
	{WritebackType, platform.AssetWriteback},
	{ObservationType, platform.AssetObservation},
	{MatchType, platform.AssetMatching},
}

func integrationResourceOfType(typ string) (integrationResource, bool) {
	for _, resource := range integrationResources {
		if resource.typ == typ {
			return resource, true
		}
	}
	return integrationResource{}, false
}

func integrationResourceOfKind(kind platform.AssetKind) (integrationResource, bool) {
	for _, resource := range integrationResources {
		if resource.kind == kind {
			return resource, true
		}
	}
	return integrationResource{}, false
}

func integrationDescriptor(kind platform.AssetKind) integrationResource {
	resource, ok := integrationResourceOfKind(kind)
	if !ok {
		panic("unknown integration kind " + string(kind))
	}
	return resource
}

type integrationEntry struct {
	kind          platform.AssetKind
	typ           string
	recordType    reflect.Type
	requiresToken bool
	publish       func(*Owner, any, platform.ReleaseAsset, platform.AssetReleaseContext) (any, error)
}

func newIntegrationEntry[T any](typ string, requiresToken bool, publish func(*Owner, T, platform.ReleaseAsset, platform.AssetReleaseContext) (T, error)) integrationEntry {
	resource, ok := integrationResourceOfType(typ)
	if !ok {
		panic("integration resource " + typ + " has no metadata")
	}
	entry := integrationEntry{kind: resource.kind, typ: typ, recordType: reflect.TypeFor[T](), requiresToken: requiresToken}
	if publish != nil {
		entry.publish = func(o *Owner, record any, asset platform.ReleaseAsset, ctx platform.AssetReleaseContext) (any, error) {
			return publish(o, record.(T), asset, ctx)
		}
	}
	return entry
}

var integrationEntries []integrationEntry

func init() {
	integrationEntries = []integrationEntry{
		newIntegrationEntry[Connection](ConnectionType, false, nil),
		newIntegrationEntry[Listener](ListenerType, true, func(_ *Owner, r Listener, _ platform.ReleaseAsset, _ platform.AssetReleaseContext) (Listener, error) {
			r.State, r.Enabled = "published", true
			return r, nil
		}),
		newIntegrationEntry[Source](SourceType, true, func(_ *Owner, r Source, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Source, error) {
			r.State, r.Publisher, r.PublisherScopes = "published", ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			if r.Stream && r.Dataset != "" && r.Puller == "" {
				r.Puller, r.PullerScopes = ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			}
			return r, nil
		}),
		newIntegrationEntry[platform.MediaSet](platform.MediaSetType, false, func(o *Owner, r platform.MediaSet, asset platform.ReleaseAsset, _ platform.AssetReleaseContext) (platform.MediaSet, error) {
			configuration, err := o.integrationConfiguration(asset)
			if err != nil {
				return r, err
			}
			fixed := configuration.(platform.MediaSet)
			fixed.Record, fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.Record, r.DefinitionVersion, r.Published, r.Versions
			return fixed, nil
		}),
		newIntegrationEntry[platform.Schedule](platform.ScheduleType, false, func(o *Owner, r platform.Schedule, asset platform.ReleaseAsset, ctx platform.AssetReleaseContext) (platform.Schedule, error) {
			configuration, err := o.integrationConfiguration(asset)
			if err != nil {
				return r, err
			}
			fixed := configuration.(platform.Schedule)
			fixed.Record, fixed.LastRun, fixed.Runner, fixed.RunnerScopes = r.Record, r.LastRun, ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			return fixed, nil
		}),
		newIntegrationEntry[Dataset](DatasetType, false, nil),
		newIntegrationEntry[Pipeline](PipelineType, true, func(_ *Owner, r Pipeline, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Pipeline, error) {
			if !ctx.DraftsOnly && !ctx.Member.CanDeclassify(r.PublishedDeclassifications()) {
				return r, fmt.Errorf("declassification publication requires clearance and permission for every selected marking")
			}
			r.State = "published"
			return r, nil
		}),
		newIntegrationEntry[Writeback](WritebackType, true, func(_ *Owner, r Writeback, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Writeback, error) {
			r.State, r.Publisher = "published", ctx.Member.ID
			return r, nil
		}),
		newIntegrationEntry[Observation](ObservationType, true, func(o *Owner, r Observation, asset platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Observation, error) {
			configuration, err := o.integrationConfiguration(asset)
			if err != nil {
				return r, err
			}
			fixed := configuration.(Observation)
			fixed.Record, fixed.State, fixed.Publisher = r.Record, "published", ctx.Member.ID
			fixed.PublisherScopes = string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			return fixed, nil
		}),
		newIntegrationEntry[Match](MatchType, true, func(_ *Owner, r Match, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Match, error) {
			r.State, r.Publisher = "published", ctx.Member.ID
			return r, nil
		}),
	}
	for _, entry := range integrationEntries {
		for _, name := range []string{"Name", "DefinitionVersion", "Published", "Versions", "Record"} {
			if _, ok := entry.recordType.FieldByName(name); !ok {
				panic("integration record " + entry.recordType.Name() + " lacks " + name)
			}
		}
	}
}

func integrationEntryOfKind(kind platform.AssetKind) *integrationEntry {
	for i := range integrationEntries {
		if integrationEntries[i].kind == kind {
			return &integrationEntries[i]
		}
	}
	return nil
}

func integrationEntryOfRecord(record any) *integrationEntry {
	typ := reflect.TypeOf(record)
	for i := range integrationEntries {
		if integrationEntries[i].recordType == typ {
			return &integrationEntries[i]
		}
	}
	return nil
}

func integrationValue(record any) (reflect.Value, bool) {
	value := reflect.ValueOf(record)
	if !value.IsValid() || value.Kind() != reflect.Struct || integrationEntryOfRecord(record) == nil {
		return reflect.Value{}, false
	}
	return value, true
}

func integrationType(kind platform.AssetKind) string {
	if entry := integrationEntryOfKind(kind); entry != nil {
		return entry.typ
	}
	return ""
}

func IsIntegration(kind platform.AssetKind) bool { return ValueIntegrationKind(kind) }

func DecodeDescriptor(raw []byte, value any) error { return ValueDecodeIntegration(raw, value) }

func IntegrationMetadata(record any) (name string, version int, published string, versions []string) {
	return integrationMetadata(record)
}

func integrationVersion(version int, kind platform.AssetKind) string {
	return ValueDefinitionVersion + "." + string(kind) + "-" + strconv.Itoa(version)
}

func integrationMetadata(record any) (name string, version int, published string, versions []string) {
	value, ok := integrationValue(record)
	if !ok {
		return
	}
	return value.FieldByName("Name").String(), int(value.FieldByName("DefinitionVersion").Int()),
		value.FieldByName("Published").String(), value.FieldByName("Versions").Interface().([]string)
}

func retainIntegration(record any, asset platform.ReleaseAsset) (any, error) {
	raw, err := json.Marshal(asset)
	if err != nil {
		return nil, err
	}
	name, version, prior, versions := integrationMetadata(record)
	ordinal, err := ValueIntegrationOrdinal(asset)
	if err != nil {
		return nil, err
	}
	if name != asset.Ref.Name {
		return nil, fmt.Errorf("integration rename requires a migration")
	}
	if prior == string(raw) {
		return record, nil
	}
	for _, old := range versions {
		var accepted platform.ReleaseAsset
		if ValueDecodeIntegration([]byte(old), &accepted) != nil {
			return nil, fmt.Errorf("invalid retained integration version")
		}
		if accepted.SourceVersion == asset.SourceVersion {
			if !equalIntegrationAsset(accepted, asset) {
				return nil, fmt.Errorf("integration version already denotes another descriptor")
			}
			raw = []byte(old)
		}
		if bytes.Equal([]byte(old), raw) {
			versions = slices.Clone(versions)
			goto saved
		}
	}
	if len(versions) >= 64 {
		return nil, fmt.Errorf("integration version family is full")
	}
	versions = append(slices.Clone(versions), string(raw))
saved:
	version = max(version, ordinal)
	value, ok := integrationValue(record)
	if !ok {
		return nil, fmt.Errorf("unknown integration owner")
	}
	retained := reflect.New(value.Type()).Elem()
	retained.Set(value)
	retained.FieldByName("DefinitionVersion").SetInt(int64(version))
	retained.FieldByName("Published").SetString(string(raw))
	retained.FieldByName("Versions").Set(reflect.ValueOf(versions))
	return retained.Interface(), nil
}

func (o *Owner) integrationInventory(kind platform.AssetKind) ([]any, error) {
	typ := integrationRecordType(kind)
	if typ == nil {
		return nil, nil
	}
	c := o.host.Automation(platform.Caller{}, App)
	rows, err := readInventoryOf(c, typ)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, record := range rows {
		if !integrationRecord(record).Archived {
			out = append(out, record)
		}
	}
	return out, nil
}

func integrationRecordType(kind platform.AssetKind) reflect.Type {
	if entry := integrationEntryOfKind(kind); entry != nil {
		return entry.recordType
	}
	return nil
}

func integrationRecord(record any) platform.Record {
	value, ok := integrationValue(record)
	if !ok {
		return platform.Record{}
	}
	return value.FieldByName("Record").Interface().(platform.Record)
}
