package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
)

type integrationEntry struct {
	kind          platform.AssetKind
	typ           string
	recordType    reflect.Type
	requiresToken bool
	publish       func(*Build, any, platform.ReleaseAsset, platform.AssetReleaseContext) (any, error)
}

func newIntegrationEntry[T any](kind platform.AssetKind, typ string, requiresToken bool, publish func(*Build, T, platform.ReleaseAsset, platform.AssetReleaseContext) (T, error)) integrationEntry {
	entry := integrationEntry{kind: kind, typ: typ, recordType: reflect.TypeFor[T](), requiresToken: requiresToken}
	if publish != nil {
		entry.publish = func(b *Build, record any, asset platform.ReleaseAsset, ctx platform.AssetReleaseContext) (any, error) {
			return publish(b, record.(T), asset, ctx)
		}
	}
	return entry
}

var integrationEntries []integrationEntry

func init() {
	integrationEntries = []integrationEntry{
		newIntegrationEntry[Connection](platform.AssetConnection, ConnectionType, false, nil),
		newIntegrationEntry[Listener](platform.AssetListener, ListenerType, true, func(_ *Build, r Listener, _ platform.ReleaseAsset, _ platform.AssetReleaseContext) (Listener, error) {
			r.State, r.Enabled = "published", true
			return r, nil
		}),
		newIntegrationEntry[Source](platform.AssetSource, SourceType, true, func(_ *Build, r Source, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Source, error) {
			r.State, r.Publisher, r.PublisherScopes = "published", ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			if r.Stream && r.Dataset != "" && r.Puller == "" {
				r.Puller, r.PullerScopes = ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			}
			return r, nil
		}),
		newIntegrationEntry[platform.MediaSet](platform.AssetMediaSet, platform.MediaSetType, false, func(b *Build, r platform.MediaSet, asset platform.ReleaseAsset, _ platform.AssetReleaseContext) (platform.MediaSet, error) {
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return r, err
			}
			fixed := configuration.(platform.MediaSet)
			fixed.Record, fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.Record, r.DefinitionVersion, r.Published, r.Versions
			return fixed, nil
		}),
		newIntegrationEntry[platform.Schedule](platform.AssetSchedule, platform.ScheduleType, false, func(b *Build, r platform.Schedule, asset platform.ReleaseAsset, ctx platform.AssetReleaseContext) (platform.Schedule, error) {
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return r, err
			}
			fixed := configuration.(platform.Schedule)
			fixed.Record, fixed.LastRun, fixed.Runner, fixed.RunnerScopes = r.Record, r.LastRun, ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			return fixed, nil
		}),
		newIntegrationEntry[Dataset](platform.AssetDataset, DatasetType, false, nil),
		newIntegrationEntry[Pipeline](platform.AssetPipeline, PipelineType, true, func(_ *Build, r Pipeline, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Pipeline, error) {
			if !ctx.DraftsOnly && !ctx.Member.CanDeclassify(r.PublishedDeclassifications()) {
				return r, fmt.Errorf("declassification publication requires clearance and permission for every selected marking")
			}
			r.State = "published"
			return r, nil
		}),
		newIntegrationEntry[Writeback](platform.AssetWriteback, WritebackType, true, func(_ *Build, r Writeback, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Writeback, error) {
			r.State, r.Publisher = "published", ctx.Member.ID
			return r, nil
		}),
		newIntegrationEntry[Observation](platform.AssetObservation, ObservationType, true, func(b *Build, r Observation, asset platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Observation, error) {
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return r, err
			}
			fixed := configuration.(Observation)
			fixed.Record, fixed.State, fixed.Publisher = r.Record, "published", ctx.Member.ID
			fixed.PublisherScopes = string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			return fixed, nil
		}),
		newIntegrationEntry[Match](platform.AssetMatching, MatchType, true, func(_ *Build, r Match, _ platform.ReleaseAsset, ctx platform.AssetReleaseContext) (Match, error) {
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

func integrationKindOf(typ string) platform.AssetKind {
	for i := range integrationEntries {
		if integrationEntries[i].typ == typ {
			return integrationEntries[i].kind
		}
	}
	return ""
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

func (b *Build) integrationInventory(kind platform.AssetKind) ([]any, error) {
	typ := integrationRecordType(kind)
	if typ == nil {
		return nil, nil
	}
	c := b.host.Automation(platform.Caller{}, ID)
	rows, err := readDefinitionInventoryOf(c, typ)
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
