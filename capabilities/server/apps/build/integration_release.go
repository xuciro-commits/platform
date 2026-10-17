package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
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
	case platform.AssetObservation:
		return ObservationType
	case platform.AssetMatching:
		return MatchType
	}
	return ""
}
func integrationKindOf(typ string) platform.AssetKind {
	for _, kind := range []platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetObservation} {
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
	case platform.AssetSource:
		return reflect.TypeFor[Source]()
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
	case Source:
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

func (b *Build) prospectiveIntegration(kind platform.AssetKind, id string) (any, bool, error) {
	name, err := b.integrationName(kind, id)
	if err != nil {
		return nil, false, nil
	}
	asset, found := b.jointDeclarations[platform.AssetRef{App: ID, Kind: kind, Name: name}]
	if !found {
		return nil, false, nil
	}
	configuration, err := b.integrationConfiguration(asset)
	return configuration, true, err
}
func (b *Build) integrationAsset(kind platform.AssetKind, record any, ordinal int) (platform.ReleaseAsset, error) {
	asset := platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: kind}, ContractVersion: 1, SourceVersion: integrationVersion(ordinal, kind)}
	name, _, _, _ := integrationMetadata(record)
	asset.Ref.Name = name
	var body any
	objectBindings := func(typ string) map[string]platform.AssetRef {
		if typ == "" {
			return nil
		}
		owner, _, _ := strings.Cut(typ, ".")
		if info, ok := b.lookupEntity(typ); ok {
			owner = info.App
		}
		return map[string]platform.AssetRef{typ: {App: owner, Kind: platform.AssetObject, Name: typ}}
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
		body = SourceDefinition{ObjectBindings: objectBindings(r.Object), Name: r.Name, Title: r.Title, Connection: conn, Profile: r.Profile, URL: r.URL, Entity: r.Entity, Filter: r.Filter, Since: r.Since, Path: r.Path, Dataset: dataset, Object: r.Object, Key: r.Key, Mapping: r.Mapping, Stream: r.Stream, Every: r.Every}
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
			}
		}
		body = PipelineDefinition{ObjectBindings: objectBindings(r.OutputObject), Name: r.Name, Title: r.Title, Input: input, Steps: steps, Expectations: r.Expectations, OutputDataset: output, OutputObject: r.OutputObject, OutputEnterprise: r.OutputEnterprise, Key: r.Key, Every: r.Every}
		if r.OutputEnterprise != nil {
			return asset, fmt.Errorf("portable enterprise landing requires a separate owner migration and is unsupported")
		}
	case Writeback:
		conn, err := b.integrationName(platform.AssetConnection, r.Connection)
		if err != nil {
			return asset, err
		}
		body = WritebackDefinition{ObjectBindings: objectBindings(r.Object), Name: r.Name, Title: r.Title, Connection: conn, Object: r.Object, On: r.On, Method: r.Method, Path: r.Path, Mapping: r.Mapping, Result: r.Result}
	case Observation:
		d, err := b.observationDefinition(r)
		if err != nil {
			return asset, err
		}
		if err := platform.CheckObservationDefinition(d, b.lookupEntity); err != nil {
			return asset, err
		}
		if d.Decision != nil {
			owner, action, ok := b.observationAction(d)
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
			ref, err := b.matchingProducer(preference.Producer)
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

// New candidates read a current description of an old accepted integration.
// Stable bindings come only from that version's frozen edges, never today's
// editable producer rows. Published and Versions continue to hold old bytes.
func integrationCandidateAsset(asset platform.ReleaseAsset) (platform.ReleaseAsset, error) {
	if asset.Provides != nil {
		return asset, nil
	}
	if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
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
	if asset.Ref.Kind == platform.AssetWriteback && definition.On == "confirmation" && slices.Contains(asset.Requires, platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: definition.Object + ".confirmation"}) {
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

func (b *Build) integrationAssets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, kind := range []platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetObservation} {
		rows, err := b.integrationInventory(kind)
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
	c := b.host.Automation(platform.Caller{}, ID)
	var problem *kernel.Error
	switch r := record.(type) {
	case Source:
		problem = b.checkSource(c, r)
	case Pipeline:
		problem = b.checkPipeline(c, r)
	case Writeback:
		problem = b.checkWriteback(c, r)
	case Match:
		problem = b.checkMatch(c, r)
	}
	if problem != nil {
		err = fmt.Errorf("%s", problem.Message)
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
	if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
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
		record = Connection{Name: d.Name, Title: d.Title, Kind: d.Kind, Marking: d.Marking}
	case platform.AssetSource:
		var d SourceDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
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
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || d.Keep < 0 {
			return nil, fmt.Errorf("invalid dataset definition")
		}
		record = Dataset{Name: d.Name, Title: d.Title, Keep: d.Keep, Marking: d.Marking}
	case platform.AssetPipeline:
		var d PipelineDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
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
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
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
	case platform.AssetObservation:
		var d platform.ObservationDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Check() != nil || d.Name != asset.Ref.Name || d.Connection.App != ID {
			return nil, fmt.Errorf("invalid observation source definition")
		}
		conn, err := b.integrationID(platform.AssetConnection, d.Connection.Name)
		if err != nil {
			return nil, err
		}
		record = Observation{Name: d.Name, Title: d.Title, Description: d.Description, Connection: conn, Object: d.Object.Name, DeviceField: d.DeviceField, Signal: d.Signal, Unit: d.Unit, Path: d.Path, WindowSeconds: d.WindowSeconds, ExpectedRetentionSeconds: d.ExpectedRetentionSeconds, ExpectedGranularitySeconds: d.ExpectedGranularitySeconds, Roles: slices.Clone(d.Roles), Scheduled: d.Scheduled, Decision: d.Decision}
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

func (b *Build) acceptedIntegration(record any, kind platform.AssetKind) (any, error) {
	_, version, raw, _ := integrationMetadata(record)
	if raw == "" {
		if version == 0 {
			return record, nil
		} // explicit historical/direct-development owner
		return nil, fmt.Errorf("accepted integration definition is missing")
	}
	var asset platform.ReleaseAsset
	if err := ValueDecodeIntegration([]byte(raw), &asset); err != nil {
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
		if err := ValueDecodeIntegration(a.Body, &d); err != nil {
			return Connection{}, err
		}
		if d.Credential && live.Secret == "" {
			return Connection{}, fmt.Errorf("connection credential binding is missing")
		}
	}
	r.Record, r.Address, r.Secret, r.AllowPrivate, r.State, r.Requested, r.Last = live.Record, live.Address, live.Secret, live.AllowPrivate, live.State, live.Requested, live.Last
	r.Marking = HigherMarking(r.Marking, live.Marking)
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
	r.Marking = HigherMarking(r.Marking, live.Marking)
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
		if err := ValueDecodeIntegration([]byte(image), &asset); err != nil {
			return err
		}
		ordinal, err := ValueIntegrationOrdinal(asset)
		if err != nil {
			return err
		}
		if err := ValueValidateIntegrationDescriptor(asset); err != nil {
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
		if ordinal == 0 && raw == "" && kind != platform.AssetObservation {
			return record, nil
		}
		return nil, err
	}
	if raw != "" {
		var prior platform.ReleaseAsset
		if err := ValueDecodeIntegration([]byte(raw), &prior); err != nil {
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
	if ValueIntegrationKind(a.Ref.Kind) && (a.Provides == nil) != (z.Provides == nil) {
		var err error
		if a.Provides == nil {
			a, err = integrationCandidateAsset(a)
		} else {
			z, err = integrationCandidateAsset(z)
		}
		if err != nil {
			return false
		}
	}
	if a.Provides != nil && z.Provides != nil && !reflect.DeepEqual(a.Provides, z.Provides) {
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
	if err := ValueDecodeIntegration([]byte(accepted), &current); err != nil {
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
		if err := ValueDecodeIntegration([]byte(raw), &issued); err != nil {
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
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		definition.Title = ""
		// Fixed Requires already compares the resolved object owner. Binding
		// metadata added to new definitions does not change an issued operation.
		definition.ObjectBindings = nil
		return json.Marshal(definition)
	case platform.AssetPipeline:
		var definition PipelineDefinition
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		definition.Title = ""
		// Fixed Requires already compares the resolved object owner. Binding
		// metadata added to new definitions does not change an issued operation.
		definition.ObjectBindings = nil
		return json.Marshal(definition)
	}
	return nil, fmt.Errorf("unsupported execution definition")
}

// Portable definitions are typed owner contracts. Local identities, addresses,
// credentials and mutable data/execution observations never enter these shapes.
type ConnectionDefinition struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Marking    string `json:"marking,omitempty"`
	Credential bool   `json:"credential,omitempty"`
}

type SourceDefinition struct {
	ObjectBindings map[string]platform.AssetRef `json:"objectBindings,omitempty"`
	Name           string                       `json:"name"`
	Title          string                       `json:"title"`
	Connection     string                       `json:"connection"`
	Profile        string                       `json:"profile,omitempty"`
	URL            string                       `json:"url,omitempty"`
	Entity         string                       `json:"entity,omitempty"`
	Filter         string                       `json:"filter,omitempty"`
	Since          string                       `json:"since,omitempty"`
	Path           string                       `json:"path,omitempty"`
	Dataset        string                       `json:"dataset,omitempty"`
	Object         string                       `json:"object,omitempty"`
	Key            string                       `json:"key,omitempty"`
	Mapping        []SourceField                `json:"mapping,omitempty"`
	Stream         bool                         `json:"stream,omitempty"`
	Every          string                       `json:"every,omitempty"`
}

type DatasetDefinition struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Keep    int    `json:"keep,omitempty"`
	Marking string `json:"marking,omitempty"`
}

type PipelineDefinition struct {
	ObjectBindings   map[string]platform.AssetRef `json:"objectBindings,omitempty"`
	Name             string                       `json:"name"`
	Title            string                       `json:"title"`
	Input            string                       `json:"input"`
	Steps            []Step                       `json:"steps"`
	Expectations     []Expectation                `json:"expectations,omitempty"`
	OutputDataset    string                       `json:"outputDataset,omitempty"`
	OutputObject     string                       `json:"outputObject,omitempty"`
	OutputEnterprise *EnterpriseTarget            `json:"outputEnterprise,omitempty"`
	Key              string                       `json:"key,omitempty"`
	Every            string                       `json:"every,omitempty"`
}

type WritebackDefinition struct {
	ObjectBindings map[string]platform.AssetRef `json:"objectBindings,omitempty"`
	Name           string                       `json:"name"`
	Title          string                       `json:"title"`
	Connection     string                       `json:"connection"`
	Object         string                       `json:"object"`
	On             string                       `json:"on"`
	Method         string                       `json:"method,omitempty"`
	Path           string                       `json:"path,omitempty"`
	Mapping        []SourceField                `json:"mapping,omitempty"`
	Result         []SourceField                `json:"result,omitempty"`
}

type MatchingDefinition struct {
	ObjectBindings map[string]platform.AssetRef `json:"objectBindings,omitempty"`
	Name           string                       `json:"name"`
	Title          string                       `json:"title"`
	Object         string                       `json:"object"`
	Keys           []MatchKey                   `json:"keys"`
	Prefer         []Preference                 `json:"prefer,omitempty"`
	Producers      map[string]platform.AssetRef `json:"producers,omitempty"`
}

func ValueIntegrationKind(kind platform.AssetKind) bool {
	switch kind {
	case platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetObservation:
		return true
	}
	return false
}

func ValueDecodeIntegration(raw []byte, value any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("integration descriptor has trailing data")
	}
	return nil
}

func ValueIntegrationOrdinal(asset platform.ReleaseAsset) (int, error) {
	prefix := ValueDefinitionVersion + "." + string(asset.Ref.Kind) + "-"
	ordinal, found := strings.CutPrefix(asset.SourceVersion, prefix)
	v, err := strconv.Atoi(ordinal)
	if !found || err != nil || v < 1 || v > 64 || strconv.Itoa(v) != ordinal {
		return 0, fmt.Errorf("unsupported integration version %s", asset.SourceVersion)
	}
	return v, nil
}

// ExecutionVersion identifies the accepted definition itself, including
// activating a previously retained version rather than the largest ordinal.
func ValueIntegrationExecutionVersion(raw string) string {
	var asset platform.ReleaseAsset
	if json.Unmarshal([]byte(raw), &asset) != nil {
		return ""
	}
	return asset.SourceVersion
}

func (s Source) ExecutionVersion() string { return ValueIntegrationExecutionVersion(s.Published) }

func (p Pipeline) ExecutionVersion() string { return ValueIntegrationExecutionVersion(p.Published) }

func ValueValidateIntegrationDependencies(asset platform.ReleaseAsset) error {
	if asset.Provides != nil {
		return platform.CheckAssetDependencies(asset)
	}
	if asset.Ref.Kind == platform.AssetObservation {
		return fmt.Errorf("observation source requires its complete dependency description")
	}
	var expected []platform.AssetRef
	add := func(kind platform.AssetKind, name string) {
		if name != "" {
			expected = append(expected, platform.AssetRef{App: ID, Kind: kind, Name: name})
		}
	}
	object := func(name string) {
		if name == "" {
			return
		}
		owner := ID
		if prefix, _, ok := strings.Cut(name, "."); ok {
			owner = prefix
		}
		expected = append(expected, platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: name})
	}
	switch asset.Ref.Kind {
	case platform.AssetSource:
		var d SourceDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return err
		}
		add(platform.AssetConnection, d.Connection)
		add(platform.AssetDataset, d.Dataset)
		object(d.Object)
	case platform.AssetPipeline:
		var d PipelineDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return err
		}
		add(platform.AssetDataset, d.Input)
		add(platform.AssetDataset, d.OutputDataset)
		for _, s := range d.Steps {
			add(platform.AssetDataset, s.Dataset)
		}
		object(d.OutputObject)
	case platform.AssetWriteback:
		var d WritebackDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return err
		}
		add(platform.AssetConnection, d.Connection)
		object(d.Object)
		legacy := platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: d.Object + ".confirmation"}
		if !slices.Contains([]string{"confirmation", "dispatch", "transaction"}, d.On) || d.On == "confirmation" && slices.Contains(asset.Requires, legacy) {
			// Retain exact historical virtual edges when reading old images.
			// Explicit confirmation/dispatch requests depend on their object/connection.
			add(platform.AssetAction, d.Object+"."+d.On)
		}
	case platform.AssetMatching:
		var d MatchingDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return err
		}
		object(d.Object)
		for _, preference := range d.Prefer {
			var producers []platform.AssetRef
			for _, ref := range asset.Requires {
				if ref.App == ID && (ref.Kind == platform.AssetSource || ref.Kind == platform.AssetPipeline) && ref.Name == preference.Producer {
					producers = append(producers, ref)
				}
			}
			if len(producers) != 1 {
				return fmt.Errorf("matching producer lacks its exact release dependency")
			}
			expected = append(expected, producers[0])
		}
	}
	slices.SortFunc(expected, func(a, z platform.AssetRef) int { return strings.Compare(a.String(), z.String()) })
	expected = slices.Compact(expected)
	if !slices.Equal(expected, asset.Requires) {
		return fmt.Errorf("integration descriptor dependencies differ from its release closure")
	}
	return nil
}

func ValueValidateIntegrationDescriptor(asset platform.ReleaseAsset) error {
	if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
		return fmt.Errorf("unsupported integration descriptor")
	}
	if _, err := ValueIntegrationOrdinal(asset); err != nil {
		return err
	}
	var record any
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		record = new(ConnectionDefinition)
	case platform.AssetSource:
		record = new(SourceDefinition)
	case platform.AssetDataset:
		record = new(DatasetDefinition)
	case platform.AssetPipeline:
		record = new(PipelineDefinition)
	case platform.AssetWriteback:
		record = new(WritebackDefinition)
	case platform.AssetObservation:
		record = new(platform.ObservationDefinition)
	case platform.AssetMatching:
		record = new(MatchingDefinition)
	}
	if err := ValueDecodeIntegration(asset.Body, record); err != nil {
		return err
	}
	value := reflect.ValueOf(record).Elem()
	if value.FieldByName("Name").String() != asset.Ref.Name || asset.Ref.Check() != nil || value.FieldByName("Title").String() == "" {
		return fmt.Errorf("integration descriptor identity or title is invalid")
	}
	switch d := record.(type) {
	case *ConnectionDefinition:
		if !slices.Contains(ConnectionKinds, d.Kind) || !slices.Contains([]string{"", "internal", "confidential", "restricted"}, d.Marking) {
			return fmt.Errorf("unknown connection kind")
		}
	case *SourceDefinition:
		if d.Connection == "" || strings.Contains(d.URL, "://") || !slices.Contains([]string{"", "json", "csv", "odata", "table"}, d.Profile) {
			return fmt.Errorf("invalid portable source")
		}
	case *DatasetDefinition:
		if d.Keep < 0 || !slices.Contains([]string{"", "internal", "confidential", "restricted"}, d.Marking) {
			return fmt.Errorf("invalid dataset retention or marking")
		}
	case *PipelineDefinition:
		if d.Input == "" || d.OutputEnterprise != nil || (d.OutputDataset == "") == (d.OutputObject == "") {
			return fmt.Errorf("invalid portable pipeline")
		}
	case *WritebackDefinition:
		if d.Connection == "" || d.Object == "" || d.On == "" || strings.ContainsAny(d.On, "./ ") || strings.Contains(d.Path, "://") {
			return fmt.Errorf("invalid portable writeback")
		}
	case *platform.ObservationDefinition:
		if err := d.Check(); err != nil {
			return err
		}
	case *MatchingDefinition:
		if d.Object == "" || len(d.Keys) == 0 && len(d.Prefer) == 0 {
			return fmt.Errorf("invalid matching rule")
		}
	}
	return ValueValidateIntegrationDependencies(asset)
}
