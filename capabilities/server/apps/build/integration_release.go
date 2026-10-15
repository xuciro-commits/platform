package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	buildapi "platformserver/internal/host/buildapi"

	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func integrationType(kind platform.AssetKind) string {
	switch kind {
	case platform.AssetConnection:
		return ConnectionType
	case platform.AssetSource:
		return SourceType
	case platform.AssetDataset:
		return DatasetType
	case platform.AssetPipeline:
		return PipelineType
	case platform.AssetWriteback:
		return WritebackType
	case platform.AssetMatching:
		return MatchType
	}
	return ""
}
func integrationKindOf(typ string) platform.AssetKind {
	for _, kind := range []platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching} {
		if integrationType(kind) == typ {
			return kind
		}
	}
	return ""
}

func integrationVersion(version int, kind platform.AssetKind) string {
	return definitionVersion + "." + string(kind) + "-" + strconv.Itoa(version)
}

func integrationMetadata(record any) (name string, version int, published string, versions []string) {
	switch r := record.(type) {
	case Connection:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Source:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Dataset:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Pipeline:
		return r.Name, r.DefinitionVersion, r.Published, r.Versions
	case Writeback:
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
	ordinal, err := buildapi.ValueIntegrationOrdinal(asset)
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
		if buildapi.ValueDecodeIntegration([]byte(old), &accepted) != nil {
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
	case Source:
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
	case Match:
		r.DefinitionVersion, r.Published, r.Versions = version, string(raw), versions
		return r, nil
	}
	return nil, fmt.Errorf("unknown integration owner")
}

func (b *Build) integrationInventory(kind platform.AssetKind) ([]any, error) {
	c := b.host.Automation(platform.Caller{}, ID)
	var out []any
	switch kind {
	case platform.AssetConnection:
		list, err := readDefinitionInventory[Connection](c)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !r.Archived {
				out = append(out, r)
			}
		}
	case platform.AssetSource:
		list, err := readDefinitionInventory[Source](c)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !r.Archived {
				out = append(out, r)
			}
		}
	case platform.AssetDataset:
		list, err := readDefinitionInventory[Dataset](c)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !r.Archived {
				out = append(out, r)
			}
		}
	case platform.AssetPipeline:
		list, err := readDefinitionInventory[Pipeline](c)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !r.Archived {
				out = append(out, r)
			}
		}
	case platform.AssetWriteback:
		list, err := readDefinitionInventory[Writeback](c)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !r.Archived {
				out = append(out, r)
			}
		}
	case platform.AssetMatching:
		list, err := readDefinitionInventory[Match](c)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !r.Archived {
				out = append(out, r)
			}
		}
	}
	return out, nil
}
func integrationRecord(record any) platform.Record {
	switch r := record.(type) {
	case Connection:
		return r.Record
	case Source:
		return r.Record
	case Dataset:
		return r.Record
	case Pipeline:
		return r.Record
	case Writeback:
		return r.Record
	case Match:
		return r.Record
	}
	return platform.Record{}
}
func (b *Build) integrationName(kind platform.AssetKind, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	list, err := b.integrationInventory(kind)
	if err != nil {
		return "", err
	}
	for _, r := range list {
		if integrationRecord(r).ID == id {
			name, _, _, _ := integrationMetadata(r)
			return name, nil
		}
	}
	return "", fmt.Errorf("integration %s/%s is unavailable", kind, id)
}
func (b *Build) integrationID(kind platform.AssetKind, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	list, err := b.integrationInventory(kind)
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
		if planned := b.releaseDraftIDs[platform.AssetRef{App: ID, Kind: kind, Name: name}]; planned != "" {
			return planned, nil
		}
		return "", fmt.Errorf("integration %s/%s has no local owner", kind, name)
	}
	return id, nil
}
func (b *Build) integrationAsset(kind platform.AssetKind, record any, ordinal int) (platform.ReleaseAsset, error) {
	asset := platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: kind}, ContractVersion: 1, SourceVersion: integrationVersion(ordinal, kind)}
	name, _, _, _ := integrationMetadata(record)
	asset.Ref.Name = name
	var body any
	add := func(kind platform.AssetKind, name string) {
		if name != "" {
			asset.Requires = append(asset.Requires, platform.AssetRef{App: ID, Kind: kind, Name: name})
		}
	}
	object := func(typ string) {
		if typ != "" {
			if info, ok := b.lookupEntity(typ); ok {
				asset.Requires = append(asset.Requires, platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: typ})
			} else {
				asset.Requires = append(asset.Requires, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: typ})
			}
		}
	}
	switch r := record.(type) {
	case Connection:
		body = ConnectionDefinition{Name: r.Name, Title: r.Title, Kind: r.Kind, Marking: r.Marking, Credential: r.Secret != ""}
	case Source:
		if r.Connection == "" || r.Header != "" || strings.Contains(r.URL, "://") || r.AllowPrivate {
			return asset, fmt.Errorf("portable source %s needs a connection and relative address without inline headers", r.Name)
		}
		conn, err := b.integrationName(platform.AssetConnection, r.Connection)
		if err != nil {
			return asset, err
		}
		dataset, err := b.integrationName(platform.AssetDataset, r.Dataset)
		if err != nil {
			return asset, err
		}
		body = SourceDefinition{Name: r.Name, Title: r.Title, Connection: conn, Profile: r.Profile, URL: r.URL, Entity: r.Entity, Filter: r.Filter, Since: r.Since, Path: r.Path, Dataset: dataset, Object: r.Object, Key: r.Key, Mapping: r.Mapping, Stream: r.Stream, Every: r.Every}
		add(platform.AssetConnection, conn)
		add(platform.AssetDataset, dataset)
		object(r.Object)
	case Dataset:
		body = DatasetDefinition{Name: r.Name, Title: r.Title, Keep: r.Keep, Marking: r.Marking}
	case Pipeline:
		input, err := b.integrationName(platform.AssetDataset, r.Input)
		if err != nil {
			return asset, err
		}
		output, err := b.integrationName(platform.AssetDataset, r.OutputDataset)
		if err != nil {
			return asset, err
		}
		steps := slices.Clone(r.Steps)
		for i := range steps {
			if steps[i].Dataset != "" {
				steps[i].Dataset, err = b.integrationName(platform.AssetDataset, steps[i].Dataset)
				if err != nil {
					return asset, err
				}
				add(platform.AssetDataset, steps[i].Dataset)
			}
		}
		body = PipelineDefinition{Name: r.Name, Title: r.Title, Input: input, Steps: steps, Expectations: r.Expectations, OutputDataset: output, OutputObject: r.OutputObject, OutputEnterprise: r.OutputEnterprise, Key: r.Key, Every: r.Every}
		add(platform.AssetDataset, input)
		add(platform.AssetDataset, output)
		object(r.OutputObject)
		if r.OutputEnterprise != nil {
			return asset, fmt.Errorf("portable enterprise landing requires a separate owner migration and is unsupported")
		}
	case Writeback:
		conn, err := b.integrationName(platform.AssetConnection, r.Connection)
		if err != nil {
			return asset, err
		}
		body = WritebackDefinition{Name: r.Name, Title: r.Title, Connection: conn, Object: r.Object, On: r.On, Method: r.Method, Path: r.Path, Mapping: r.Mapping, Result: r.Result}
		add(platform.AssetConnection, conn)
		object(r.Object)
		if r.On != "confirmation" {
			add(platform.AssetAction, r.Object+"."+r.On)
		}
	case Match:
		body = MatchingDefinition{Name: r.Name, Title: r.Title, Object: r.Object, Keys: r.Keys, Prefer: r.Prefer}
		object(r.Object)
		for _, preference := range r.Prefer {
			ref, err := b.matchingProducer(preference.Producer)
			if err != nil {
				return asset, err
			}
			asset.Requires = append(asset.Requires, ref)
		}
	default:
		return asset, fmt.Errorf("unknown integration descriptor")
	}
	slices.SortFunc(asset.Requires, func(a, z platform.AssetRef) int { return strings.Compare(a.String(), z.String()) })
	asset.Requires = slices.Compact(asset.Requires)
	raw, err := json.Marshal(body)
	asset.Body = raw
	return asset, err
}

func (b *Build) integrationAssets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, kind := range []platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching} {
		rows, err := b.integrationInventory(kind)
		if err != nil {
			return nil, err
		}
		for _, record := range rows {
			_, version, published, _ := integrationMetadata(record)
			if published != "" {
				var asset platform.ReleaseAsset
				if err := buildapi.ValueDecodeIntegration([]byte(published), &asset); err != nil {
					return nil, err
				}
				out = append(out, asset)
				continue
			}
			if version != 0 {
				return nil, fmt.Errorf("integration publication is missing")
			}
			// Explicit legacy/development configurations have no immutable definition
			// history. Only portable supported installed definitions are projected.
			installed := false
			switch r := record.(type) {
			case Connection:
				installed = r.State == "ready"
			case Source:
				installed = r.State == "published"
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
			asset, err := b.integrationAsset(kind, record, 1)
			if err != nil {
				continue
			}
			out = append(out, asset)
		}
	}
	return out, nil
}
func (b *Build) integrationDraftAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, had bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	rows, e := b.integrationInventory(kind)
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
	_, version, published, _ := integrationMetadata(record)
	if published != "" {
		var old platform.ReleaseAsset
		if err = buildapi.ValueDecodeIntegration([]byte(published), &old); err != nil {
			return
		}
		prior, had = old.Ref, true
	}
	if version >= 64 {
		err = fmt.Errorf("integration version family is full")
		return
	}
	asset, e := b.integrationAsset(kind, record, version+1)
	if e != nil {
		err = e
		return
	}
	next = asset.Ref
	if !had {
		_, had = findReleaseAsset(before, next)
		if had {
			prior = next
		}
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || had && a.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) integrationConfiguration(asset platform.ReleaseAsset) (any, error) {
	if asset.Ref.App != ID || !buildapi.ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
		return nil, fmt.Errorf("unknown integration asset %s", asset.Ref)
	}
	if _, err := buildapi.ValueIntegrationOrdinal(asset); err != nil {
		return nil, err
	}
	if err := buildapi.ValueValidateIntegrationDependencies(asset); err != nil {
		return nil, err
	}
	var record any
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var d ConnectionDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || !slices.Contains(ConnectionKinds, d.Kind) {
			return nil, fmt.Errorf("invalid connection definition")
		}
		record = Connection{Name: d.Name, Title: d.Title, Kind: d.Kind, Marking: d.Marking}
	case platform.AssetSource:
		var d SourceDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Connection == "" || strings.Contains(d.URL, "://") {
			return nil, fmt.Errorf("source requires a portable connection and relative address")
		}
		conn, err := b.integrationID(platform.AssetConnection, d.Connection)
		if err != nil {
			return nil, err
		}
		dataset, err := b.integrationID(platform.AssetDataset, d.Dataset)
		if err != nil {
			return nil, err
		}
		record = Source{Name: d.Name, Title: d.Title, Connection: conn, Profile: d.Profile, URL: d.URL, Entity: d.Entity, Filter: d.Filter, Since: d.Since, Path: d.Path, Dataset: dataset, Object: d.Object, Key: d.Key, Mapping: d.Mapping, Stream: d.Stream, Every: d.Every}
	case platform.AssetDataset:
		var d DatasetDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Keep < 0 {
			return nil, fmt.Errorf("invalid dataset definition")
		}
		record = Dataset{Name: d.Name, Title: d.Title, Keep: d.Keep, Marking: d.Marking}
	case platform.AssetPipeline:
		var d PipelineDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Input == "" || d.OutputEnterprise != nil {
			return nil, fmt.Errorf("invalid or unsupported pipeline definition")
		}
		input, err := b.integrationID(platform.AssetDataset, d.Input)
		if err != nil {
			return nil, err
		}
		output, err := b.integrationID(platform.AssetDataset, d.OutputDataset)
		if err != nil {
			return nil, err
		}
		for i := range d.Steps {
			if d.Steps[i].Dataset != "" {
				d.Steps[i].Dataset, err = b.integrationID(platform.AssetDataset, d.Steps[i].Dataset)
				if err != nil {
					return nil, err
				}
			}
		}
		record = Pipeline{Name: d.Name, Title: d.Title, Input: input, Steps: d.Steps, Expectations: d.Expectations, OutputDataset: output, OutputObject: d.OutputObject, Key: d.Key, Every: d.Every}
	case platform.AssetWriteback:
		var d WritebackDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Connection == "" || d.Object == "" || d.On == "" {
			return nil, fmt.Errorf("invalid writeback definition")
		}
		conn, err := b.integrationID(platform.AssetConnection, d.Connection)
		if err != nil {
			return nil, err
		}
		record = Writeback{Name: d.Name, Title: d.Title, Connection: conn, Object: d.Object, On: d.On, Method: d.Method, Path: d.Path, Mapping: d.Mapping, Result: d.Result}
	case platform.AssetMatching:
		var d MatchingDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Object == "" {
			return nil, fmt.Errorf("invalid matching definition")
		}
		record = Match{Name: d.Name, Title: d.Title, Object: d.Object, Keys: d.Keys, Prefer: d.Prefer}
	}
	return record, nil
}

func (b *Build) acceptedIntegration(record any, kind platform.AssetKind) (any, error) {
	_, version, raw, _ := integrationMetadata(record)
	if raw == "" {
		if version == 0 {
			return record, nil
		} // explicit historical/direct-development owner
		return nil, fmt.Errorf("accepted integration definition is missing")
	}
	var asset platform.ReleaseAsset
	if err := buildapi.ValueDecodeIntegration([]byte(raw), &asset); err != nil {
		return nil, err
	}
	if asset.Ref.Kind != kind {
		return nil, fmt.Errorf("accepted integration kind differs")
	}
	return b.integrationConfiguration(asset)
}

// ExecutionSource overlays only source-local execution observations. Changing
// a mutable editor draft cannot replace an accepted source configuration.
func (b *Build) ExecutionSource(live Source) (Source, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetSource)
	if err != nil {
		return Source{}, err
	}
	r := fixed.(Source)
	r.Record, r.State, r.Puller, r.Cursor, r.Requested, r.Last = live.Record, live.State, live.Puller, live.Cursor, live.Requested, live.Last
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}
func (b *Build) ExecutionConnection(live Connection) (Connection, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetConnection)
	if err != nil {
		return Connection{}, err
	}
	r := fixed.(Connection)
	if r.Kind != live.Kind {
		return Connection{}, fmt.Errorf("connection binding kind differs from its accepted definition")
	}
	if live.Published != "" {
		var a platform.ReleaseAsset
		var d ConnectionDefinition
		_ = json.Unmarshal([]byte(live.Published), &a)
		if err := buildapi.ValueDecodeIntegration(a.Body, &d); err != nil {
			return Connection{}, err
		}
		if d.Credential && live.Secret == "" {
			return Connection{}, fmt.Errorf("connection credential binding is missing")
		}
	}
	r.Record, r.Address, r.Secret, r.AllowPrivate, r.State, r.Requested, r.Last = live.Record, live.Address, live.Secret, live.AllowPrivate, live.State, live.Requested, live.Last
	r.Marking = buildapi.HigherMarking(r.Marking, live.Marking)
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}
func (b *Build) ExecutionDataset(live Dataset) (Dataset, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetDataset)
	if err != nil {
		return Dataset{}, err
	}
	r := fixed.(Dataset)
	r.Record, r.Producer, r.Schema, r.Version, r.Last = live.Record, live.Producer, live.Schema, live.Version, live.Last
	r.Marking = buildapi.HigherMarking(r.Marking, live.Marking)
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}
func (b *Build) ExecutionPipeline(live Pipeline) (Pipeline, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetPipeline)
	if err != nil {
		return Pipeline{}, err
	}
	r := fixed.(Pipeline)
	r.Record, r.State, r.Runner, r.Requested, r.Last, r.Marking = live.Record, live.State, live.Runner, live.Requested, live.Last, live.Marking
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}
func (b *Build) ExecutionWriteback(live Writeback) (Writeback, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetWriteback)
	if err != nil {
		return Writeback{}, err
	}
	r := fixed.(Writeback)
	r.Record, r.State, r.Publisher, r.Sent, r.Failed, r.Rejected, r.Answers = live.Record, live.State, live.Publisher, live.Sent, live.Failed, live.Rejected, live.Answers
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}
func (b *Build) ExecutionMatching(live Match) (Match, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetMatching)
	if err != nil {
		return Match{}, err
	}
	r := fixed.(Match)
	r.Record, r.State, r.Publisher = live.Record, live.State, live.Publisher
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (b *Build) matchingProducer(name string) (platform.AssetRef, error) {
	var matches []platform.AssetRef
	for _, kind := range []platform.AssetKind{platform.AssetSource, platform.AssetPipeline} {
		rows, err := b.integrationInventory(kind)
		if err != nil {
			return platform.AssetRef{}, err
		}
		for _, row := range rows {
			n, _, _, _ := integrationMetadata(row)
			if n == name {
				matches = append(matches, platform.AssetRef{App: ID, Kind: kind, Name: n})
			}
		}
	}
	if len(matches) != 1 {
		return platform.AssetRef{}, fmt.Errorf("matching producer %s needs one source or pipeline owner", name)
	}
	return matches[0], nil
}

func validateIntegrationFamily(record any, current platform.ReleaseAsset) error {
	_, version, raw, history := integrationMetadata(record)
	if len(history) == 0 || len(history) > 64 {
		return fmt.Errorf("integration has no bounded retained version family")
	}
	seen := map[string]string{}
	maxOrdinal := 0
	found := false
	for _, image := range history {
		var asset platform.ReleaseAsset
		if err := buildapi.ValueDecodeIntegration([]byte(image), &asset); err != nil {
			return err
		}
		ordinal, err := buildapi.ValueIntegrationOrdinal(asset)
		if err != nil {
			return err
		}
		if err := buildapi.ValueValidateIntegrationDescriptor(asset); err != nil {
			return err
		}
		if asset.Ref != current.Ref || asset.ContractVersion != 1 {
			return fmt.Errorf("integration family identity differs")
		}
		if prior, ok := seen[asset.SourceVersion]; ok {
			return fmt.Errorf("integration family duplicates version (%t)", prior == image)
		}
		seen[asset.SourceVersion] = image
		maxOrdinal = max(maxOrdinal, ordinal)
		if image == raw {
			found = true
		}
	}
	if !found || maxOrdinal != version {
		return fmt.Errorf("integration family published pointer differs")
	}
	return nil
}

func (b *Build) freezeDirectIntegration(kind platform.AssetKind, record any) (any, error) {
	_, ordinal, raw, _ := integrationMetadata(record)
	asset, err := b.integrationAsset(kind, record, ordinal+1)
	if err != nil {
		if ordinal == 0 && raw == "" {
			return record, nil
		}
		return nil, err
	}
	if raw != "" {
		var prior platform.ReleaseAsset
		if err := buildapi.ValueDecodeIntegration([]byte(raw), &prior); err != nil {
			return nil, err
		}
		prior.SourceVersion = asset.SourceVersion
		if equalIntegrationAsset(prior, asset) {
			return record, nil
		}
	}
	fixed, err := retainIntegration(record, asset)
	if err != nil {
		return nil, err
	}
	image, err := json.Marshal(fixed)
	if err != nil {
		return nil, err
	}
	if err := b.validateIntegrationUpgrade(platform.AssetInstallation{Ref: asset.Ref, Schema: integrationType(kind) + ".publish", Record: integrationType(kind) + "/" + integrationRecord(fixed).ID, Image: image}); err != nil {
		return nil, err
	}
	return fixed, nil
}

func equalIntegrationAsset(a, z platform.ReleaseAsset) bool {
	if a.Ref != z.Ref || a.SourceVersion != z.SourceVersion || a.ContractVersion != z.ContractVersion || !slices.Equal(a.Requires, z.Requires) {
		return false
	}
	var left, right any
	return json.Unmarshal(a.Body, &left) == nil && json.Unmarshal(z.Body, &right) == nil && reflect.DeepEqual(left, right)
}

// retainedExecutionEquivalent checks the original issued definition against
// the current accepted one. Titles are presentation; all execution fields and
// dependency identities must remain identical. Mutable editor fields and the
// current environment are never used to reinterpret an earlier result.
func retainedExecutionEquivalent(record any, issuedVersion string) (bool, error) {
	_, ordinal, accepted, history := integrationMetadata(record)
	if accepted == "" {
		return ordinal == 0 && issuedVersion == "", nil
	}
	if issuedVersion == "" {
		return false, nil
	}
	var current platform.ReleaseAsset
	if err := buildapi.ValueDecodeIntegration([]byte(accepted), &current); err != nil {
		return false, err
	}
	if err := validateIntegrationFamily(record, current); err != nil {
		return false, err
	}
	if current.Ref.Kind != platform.AssetSource && current.Ref.Kind != platform.AssetPipeline {
		return false, fmt.Errorf("only source and pipeline results have execution versions")
	}
	if current.SourceVersion == issuedVersion {
		return true, nil
	}
	for _, raw := range history {
		var issued platform.ReleaseAsset
		if err := buildapi.ValueDecodeIntegration([]byte(raw), &issued); err != nil {
			return false, err
		}
		if issued.SourceVersion != issuedVersion {
			continue
		}
		if issued.Ref != current.Ref || issued.ContractVersion != current.ContractVersion || !slices.Equal(issued.Requires, current.Requires) {
			return false, nil
		}
		before, err := canonicalIntegrationExecution(issued)
		if err != nil {
			return false, err
		}
		after, err := canonicalIntegrationExecution(current)
		if err != nil {
			return false, err
		}
		return bytes.Equal(before, after), nil
	}
	return false, nil
}
func canonicalIntegrationExecution(asset platform.ReleaseAsset) ([]byte, error) {
	switch asset.Ref.Kind {
	case platform.AssetSource:
		var definition SourceDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		definition.Title = ""
		return json.Marshal(definition)
	case platform.AssetPipeline:
		var definition PipelineDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		definition.Title = ""
		return json.Marshal(definition)
	}
	return nil, fmt.Errorf("unsupported execution definition")
}
