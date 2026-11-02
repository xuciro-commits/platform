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

func integrationType(kind platform.AssetKind) string {
	switch kind {
	case platform.AssetConnection:
		return ConnectionType
	case platform.AssetListener:
		return ListenerType
	case platform.AssetSource:
		return SourceType
	case platform.AssetMediaSet:
		return platform.MediaSetType
	case platform.AssetSchedule:
		return platform.ScheduleType
	case platform.AssetDataset:
		return DatasetType
	case platform.AssetPipeline:
		return PipelineType
	case platform.AssetWriteback:
		return WritebackType
	case platform.AssetObservation:
		return ObservationType
	case platform.AssetMatching:
		return MatchType
	}
	return ""
}

func integrationKindOf(typ string) platform.AssetKind {
	for _, kind := range []platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetListener, platform.AssetDataset, platform.AssetMediaSet, platform.AssetSchedule, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetObservation} {
		if integrationType(kind) == typ {
			return kind
		}
	}
	return ""
}

func integrationVersion(version int, kind platform.AssetKind) string {
	return ValueDefinitionVersion + "." + string(kind) + "-" + strconv.Itoa(version)
}

func integrationMetadata(record any) (name string, version int, published string, versions []string) {
	switch r := record.(type) {
	case Connection:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Listener:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Source:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case platform.MediaSet:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case platform.Schedule:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Dataset:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Pipeline:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Writeback:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Observation:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Match:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	}
	return
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
	switch r := record.(type) {
	case Connection:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Listener:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Source:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case platform.MediaSet:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case platform.Schedule:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Dataset:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Pipeline:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Writeback:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Observation:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	case Match:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	}
	return nil, fmt.Errorf("unknown integration owner")
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
	switch kind {
	case platform.AssetConnection:
		return reflect.TypeFor[Connection]()
	case platform.AssetListener:
		return reflect.TypeFor[Listener]()
	case platform.AssetSource:
		return reflect.TypeFor[Source]()
	case platform.AssetMediaSet:
		return reflect.TypeFor[platform.MediaSet]()
	case platform.AssetSchedule:
		return reflect.TypeFor[platform.Schedule]()
	case platform.AssetDataset:
		return reflect.TypeFor[Dataset]()
	case platform.AssetPipeline:
		return reflect.TypeFor[Pipeline]()
	case platform.AssetWriteback:
		return reflect.TypeFor[Writeback]()
	case platform.AssetObservation:
		return reflect.TypeFor[Observation]()
	case platform.AssetMatching:
		return reflect.TypeFor[Match]()
	}
	return nil
}

func integrationRecord(record any) platform.Record {
	switch r := record.(type) {
	case Connection:
		return r.Record
	case Listener:
		return r.Record
	case Source:
		return r.Record
	case platform.MediaSet:
		return r.Record
	case platform.Schedule:
		return r.Record
	case Dataset:
		return r.Record
	case Pipeline:
		return r.Record
	case Writeback:
		return r.Record
	case Observation:
		return r.Record
	case Match:
		return r.Record
	}
	return platform.Record{}
}
