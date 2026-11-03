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
	kind       platform.AssetKind
	typ        string
	recordType reflect.Type
}

var integrationEntries = []integrationEntry{
	{platform.AssetConnection, ConnectionType, reflect.TypeFor[Connection]()},
	{platform.AssetListener, ListenerType, reflect.TypeFor[Listener]()},
	{platform.AssetSource, SourceType, reflect.TypeFor[Source]()},
	{platform.AssetMediaSet, platform.MediaSetType, reflect.TypeFor[platform.MediaSet]()},
	{platform.AssetSchedule, platform.ScheduleType, reflect.TypeFor[platform.Schedule]()},
	{platform.AssetDataset, DatasetType, reflect.TypeFor[Dataset]()},
	{platform.AssetPipeline, PipelineType, reflect.TypeFor[Pipeline]()},
	{platform.AssetWriteback, WritebackType, reflect.TypeFor[Writeback]()},
	{platform.AssetObservation, ObservationType, reflect.TypeFor[Observation]()},
	{platform.AssetMatching, MatchType, reflect.TypeFor[Match]()},
}

func init() {
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
