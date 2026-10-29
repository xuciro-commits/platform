package build

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	return ValueDefinitionVersion + "." + string(kind) + "-" + strconv.Itoa(version)
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
	for ref, planned := range b.releaseDraftIDs {
		if ref.App == ID && ref.Kind == kind && planned == id {
			return ref.Name, nil
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
		body = WritebackDefinition{ObjectBindings: objectBindings(r.Object), Name: r.Name, Title: r.Title, Connection: conn, Object: r.Object, On: r.On, Method: r.Method, Transport: r.Transport, Path: r.Path, Mapping: r.Mapping, Result: r.Result}
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
		record = Writeback{Name: d.Name, Title: d.Title, Connection: conn, Object: d.Object, On: d.On, Method: d.Method, Transport: d.Transport, Path: d.Path, Mapping: d.Mapping, Result: d.Result}
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
	r.Publisher, r.PublisherScopes = live.Publisher, live.PublisherScopes
	r.Record, r.State, r.Puller, r.PullerScopes, r.Cursor, r.Requested, r.Last = live.Record, live.State, live.Puller, live.PullerScopes, live.Cursor, live.Requested, live.Last
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

// ConnectionConfiguration projects accepted declarations with local settings;
// delivery and historical reconstruction do not require a live environment.
func (b *Build) ConnectionConfiguration(live Connection) (Connection, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetConnection)
	if err != nil {
		return Connection{}, err
	}
	r := fixed.(Connection)
	if r.Kind != live.Kind {
		return Connection{}, fmt.Errorf("connection binding kind differs from its accepted definition")
	}
	r.Record, r.Address, r.Secret, r.AllowPrivate, r.State, r.Requested, r.Last = live.Record, live.Address, live.Secret, live.AllowPrivate, live.State, live.Requested, live.Last
	r.Marking = HigherMarking(r.Marking, live.Marking)
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (b *Build) ExecutionConnection(live Connection) (Connection, error) {
	connection, err := b.ConnectionConfiguration(live)
	if err != nil {
		return Connection{}, err
	}
	if connection.Archived || connection.State != "ready" || connection.Address == "" {
		return Connection{}, fmt.Errorf("connection is not ready; configure and check it before use")
	}
	if problem := b.validConnection(connection); problem != nil {
		return Connection{}, problem
	}
	if live.Published != "" {
		var asset platform.ReleaseAsset
		var definition ConnectionDefinition
		_ = json.Unmarshal([]byte(live.Published), &asset)
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return Connection{}, err
		}
		if definition.Credential && live.Secret == "" {
			return Connection{}, fmt.Errorf("connection credential binding is missing")
		}
	}
	return connection, nil
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
	r.Record, r.State, r.Runner, r.RunnerScopes, r.Requested, r.Last, r.Marking = live.Record, live.State, live.Runner, live.RunnerScopes, live.Requested, live.Last, live.Marking
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

// recordKind reports whether a draft is substituted in the shared definition
// inventories (objects, pages, applications, flows) rather than in its own
// owner's asset snapshot.
func recordKind(kind platform.AssetKind) bool {
	switch kind {
	case platform.AssetObject, platform.AssetPage, platform.AssetApp, platform.AssetFlow:
		return true
	}
	return false
}

// lookupEntity resolves an entity type with the joint candidate's draft objects
// in view. Outside a joint build it is exactly the host's installed registry.
func (b *Build) lookupEntity(typ string) (platform.EntityInfo, bool) {
	if info, ok := b.jointEntities[typ]; ok {
		return info, true
	}
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.host.Entity(typ)
}

func (b *Build) declaresRole(role string) bool {
	for _, model := range b.jointEntities {
		if _, known := model.Scope.Levels[role]; known {
			return true
		}
	}
	return slices.Contains(b.Manifest().AllRoles(), role)
}

// jointObjectEntities declares what each selected draft object would be
// installed as, using the same descriptor the publisher installs.
func (b *Build) jointObjectEntities(objects []Object, drafts []JointDraftRef) map[string]platform.EntityInfo {
	joint := map[string]platform.EntityInfo{}
	for _, draft := range drafts {
		if draft.Kind != platform.AssetObject {
			continue
		}
		i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID && !o.Archived })
		if i < 0 {
			continue
		}
		record := objects[i]
		info, err := platform.Describe(ID, Entity(record), func(reflect.Type) string { return "" })
		if err != nil {
			continue // the draft's own check reports why it cannot be published
		}
		joint[info.Type] = info
	}
	return joint
}

// DraftReleaseAssetsMulti substitutes every selected saved draft in one
// candidate image. Simple kinds keep their owner's existing single-draft
// checks; record kinds are substituted through one shared inventory so a page
// or flow may reference an object that is itself only a draft here.
func (b *Build) DraftReleaseAssetsMulti(drafts []JointDraftRef, member platform.Member) (JointDraftAssets, error) {
	var result JointDraftAssets
	if len(drafts) == 0 {
		return result, fmt.Errorf("select at least one saved draft")
	}
	if len(drafts) > JointDraftLimit {
		return result, fmt.Errorf("a joint candidate accepts at most %d drafts", JointDraftLimit)
	}
	seen := map[platform.AssetRef]bool{}
	for _, draft := range drafts {
		ref := platform.AssetRef{App: ID, Kind: draft.Kind, Name: draft.ID}
		if draft.ID == "" || seen[ref] {
			return result, fmt.Errorf("a draft must be named once")
		}
		seen[ref] = true
	}
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return result, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return result, err
	}
	before, err := b.ReleaseAssets()
	if err != nil {
		return result, err
	}
	// The joint candidate's own object drafts answer entity lookups, so a page,
	// flow or reference may name an object that is not installed yet.
	restore, restoreMode, restoreDeclarations := b.jointEntities, b.jointMode, b.jointDeclarations
	if len(drafts) > 1 {
		b.jointMode = true
	}
	b.jointEntities = b.jointObjectEntities(objects, drafts)
	b.jointDeclarations = maps.Clone(b.jointDeclarations)
	if b.jointDeclarations == nil {
		b.jointDeclarations = map[platform.AssetRef]platform.ReleaseAsset{}
	}
	defer func() { b.jointEntities, b.jointMode, b.jointDeclarations = restore, restoreMode, restoreDeclarations }()

	inv := draftInventories{objects: objects, pages: pages, apps: apps, processes: processes}
	overrides := map[platform.AssetRef]platform.ReleaseAsset{}
	removed := map[platform.AssetRef]bool{}
	// Agents consume selected queries; evaluation plans pin selected function
	// versions. Produce those dependencies before their consumers, independent
	// of the order in which the builder selected them.
	declarations := slices.Clone(drafts)
	priority := func(kind platform.AssetKind) int {
		switch kind {
		case platform.AssetEvaluation:
			return 2
		case platform.AssetAgent:
			return 1
		}
		return 0
	}
	slices.SortStableFunc(declarations, func(a, b JointDraftRef) int {
		return cmp.Compare(priority(a.Kind), priority(b.Kind))
	})
	for _, draft := range declarations {
		if !recordKind(draft.Kind) {
			// The owner's own single-draft path validates and substitutes it.
			_, after, prior, next, hadPrior, err := b.singleDraftAssets(draft.Kind, draft.ID)
			if err != nil {
				return result, err
			}
			asset, ok := findReleaseAsset(after, next)
			if !ok {
				return result, fmt.Errorf("draft %s did not produce %s", draft.ID, next)
			}
			overrides[next] = asset
			b.jointDeclarations[next] = asset
			if hadPrior && prior != next {
				removed[prior] = true
			}
			if hadPrior {
				result.Priors = append(result.Priors, prior)
			}
			result.Nexts = append(result.Nexts, next)
			continue
		}
	}
	// Resolve the selected immutable integration declarations before checking
	// objects that refer back to them. Nothing is installed or marked published.
	for _, draft := range drafts {
		if !recordKind(draft.Kind) {
			continue
		}
		prior, next, hadPrior, err := b.applyRecordDraft(&inv, draft.Kind, draft.ID, member)
		// A refused draft still names the asset it would have replaced: the
		// review can say what is installed now beside why the draft failed.
		if hadPrior {
			result.Priors = append(result.Priors, prior)
			if prior != next {
				removed[prior] = true
			}
		}
		if next != (platform.AssetRef{}) {
			result.Nexts = append(result.Nexts, next)
		}
		if err != nil {
			return result, err
		}
	}
	recordAssets, err := releaseAssets(inv.objects, inv.pages, inv.apps, inv.processes, b.Manifest().Version)
	if err == nil {
		recordAssets, err = b.bindConfirmationAssets(recordAssets)
	}
	if err == nil {
		recordAssets, err = b.bindDispatchAssets(recordAssets)
	}
	if err != nil {
		return result, err
	}
	after := recordAssets
	// releaseAssets rebuilds records and their generated actions together; the
	// installed snapshot contributes the owners' remaining assets, with the
	// selected drafts substituted.
	rebuilt := map[platform.AssetRef]bool{}
	for _, asset := range recordAssets {
		rebuilt[asset.Ref] = true
	}
	for _, asset := range before {
		if substitute, ok := overrides[asset.Ref]; ok {
			after = append(after, substitute)
			delete(overrides, asset.Ref)
			continue
		}
		if removed[asset.Ref] || rebuilt[asset.Ref] {
			continue
		}
		if asset.Ref.Kind == platform.AssetAction {
			var action struct {
				Target string `json:"target"`
			}
			if json.Unmarshal(asset.Body, &action) != nil {
				return result, fmt.Errorf("installed action %s has no valid target", asset.Ref)
			}
			if rebuilt[platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetObject, Name: action.Target}] {
				continue // The rebuilt object owns its complete generated catalog.
			}
		}
		after = append(after, asset)
	}
	for _, substitute := range overrides {
		after = append(after, substitute)
	}
	result.Before, result.After = before, after
	return result, nil
}

func findReleaseAsset(assets []platform.ReleaseAsset, ref platform.AssetRef) (platform.ReleaseAsset, bool) {
	i := slices.IndexFunc(assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return platform.ReleaseAsset{}, false
	}
	return assets[i], true
}

// draftInventories are the builder's four record families as one candidate
// sees them: installed records with the selected drafts substituted in.
type draftInventories struct {
	objects   []Object
	pages     []Page
	apps      []Application
	processes []Process
}

// applyRecordDraft substitutes one saved record draft into the shared
// inventories. Validation runs against the joint candidate's entities, and the
// host's own install checks re-run when the candidate is installed; the
// builder-level checks that need only the candidate's image run here.
func (b *Build) applyRecordDraft(inv *draftInventories, kind platform.AssetKind, id string, member platform.Member) (prior, next platform.AssetRef, hadPrior bool, err error) {
	joint := b.jointMode
	switch kind {
	case platform.AssetFlow:
		i := slices.IndexFunc(inv.processes, func(p Process) bool { return p.ID == id })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft process %q not found", id)
		}
		record := inv.processes[i]
		if was, ok := ReadPublication[Process](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		source := b.installed[record.Object]
		models := []platform.Entity{}
		for _, object := range inv.objects {
			if _, selected := b.jointEntities[TypeOf(object.Name)]; !selected || object.Archived {
				continue
			}
			model := Entity(object)
			models = append(models, model)
			if model.Type == record.Object {
				source = model
			}
		}
		if problem := b.checkFlow(record, source, models...); problem != nil {
			err = fmt.Errorf("%s", problem.Message)
		} else if record.Version >= 64 {
			err = fmt.Errorf("a process may retain at most 64 published versions")
		} else if b.host.BuildProcesses() == nil {
			err = fmt.Errorf("this tenant runs no processes")
		} else {
			record.Version++
			if record.Every != "" || record.Continuous != nil && record.Continuous.Intake != nil {
				record.Scheduler = member.ID
			} else {
				record.Scheduler = ""
			}
			if !joint {
				err = b.host.BuildProcesses().Validate(b, b.flowOf(record))
			}
			if err == nil {
				record.Published = published(record)
				record.Versions = append(slices.Clone(record.Versions), record.Published)
				record.State = "published"
				inv.processes[i] = record
			}
		}
	case platform.AssetObject:
		i := slices.IndexFunc(inv.objects, func(o Object) bool { return o.ID == id && !o.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft object %q not found", id)
		}
		record := inv.objects[i]
		if was, ok := ReadPublication[Object](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		if err = b.captureExtensionSource(&record); err != nil {
			return prior, next, hadPrior, err
		}
		if err = b.captureObjectSchema(&record); err != nil {
			return prior, next, hadPrior, err
		}
		if err = b.Check(record, record.ID, b.host.Caller(platform.Caller{}, member, ID).Rules()); err == nil {
			err = b.validateObjectInstallation(b.host.Caller(platform.Caller{}, member, ID), record)
		}
		if err == nil {
			inv.objects[i].Published = published(record)
		}
	case platform.AssetPage:
		i := slices.IndexFunc(inv.pages, func(p Page) bool { return p.ID == id && !p.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft page %q not found", id)
		}
		record := inv.pages[i]
		if was, ok := ReadPublication[Page](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: was.Name}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: record.Name}
		if err = b.checkPage(record); err == nil && !joint {
			err = b.host.ValidateInstallPage(descriptor(record))
		}
		if err == nil {
			err = retainPagePublication(&record)
			inv.pages[i] = record
		}
	case platform.AssetApp:
		i := slices.IndexFunc(inv.apps, func(a Application) bool { return a.ID == id && !a.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft application %q not found", id)
		}
		record := inv.apps[i]
		if was, ok := ReadPublication[Application](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: was.Name}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: record.Name}
		if err = b.checkApplication(record); err == nil && !joint {
			err = b.host.ValidateInstallApplication(applicationDescriptor(record))
		}
		if err == nil {
			inv.apps[i].Published = published(record)
		}
	default:
		err = fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	return prior, next, hadPrior, err
}

// singleDraftAssets is the owner's original one-draft comparison, unchanged for
// simple kinds and routed through the shared record substitution otherwise.
func (b *Build) singleDraftAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	if ValueIntegrationKind(kind) {
		return b.integrationDraftAssets(kind, id)
	}
	if kind == platform.AssetEvaluation || kind == platform.AssetTestPlan {
		return b.testPlanDraftAssets(id)
	}
	if kind == platform.AssetSampleSet {
		return b.sampleSetDraftAssets(id)
	}
	if kind == platform.AssetPropertyType {
		return b.propertyTypeDraftAssets(id)
	}
	if kind == platform.AssetLinkType {
		return b.linkTypeDraftAssets(id)
	}
	if kind == platform.AssetQuery {
		return b.queryDraftAssets(id)
	}
	if kind == platform.AssetCompute {
		if _, ok := platform.Get[Table](b.host.Automation(platform.Caller{}, ID), id); ok {
			return b.tableDraftAssets(id)
		}
		return b.CodeDraftAssets(id)
	}
	if kind == platform.AssetAlert {
		return b.alertDraftAssets(id)
	}
	if kind == platform.AssetFunction {
		return b.functionDraftAssets(id)
	}
	if kind == platform.AssetAgent {
		return b.agentDraftAssets(id)
	}
	if !recordKind(kind) {
		return nil, nil, prior, next, false, fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	assets, err := b.DraftReleaseAssetsMulti([]JointDraftRef{{Kind: kind, ID: id}}, platform.Member{})
	if err != nil {
		return nil, nil, prior, next, false, err
	}
	if len(assets.Priors) > 0 {
		prior, hadPrior = assets.Priors[0], true
	}
	if len(assets.Nexts) > 0 {
		next = assets.Nexts[0]
	}
	return assets.Before, assets.After, prior, next, hadPrior, nil
}

// ReferencedDrafts selects changed or unpublished application dependencies;
// unchanged declarations retain their installed versions. For other record
// roots it discovers unpublished objects, pages and workflows. Their non-record
// dependencies remain with the original owner's publication check.
func (b *Build) ReferencedDrafts(kind platform.AssetKind, id string) ([]JointDraftRef, error) {
	if id == "" {
		return nil, fmt.Errorf("a saved draft is required")
	}
	if kind == platform.AssetApp {
		// Publication selects changed declarations, while trials intentionally
		// substitute every reachable draft. Unchanged dependencies retain the
		// installed versions already pinned by their consumers.
		changes, err := b.SavedDraftChanges(b.host.Automation(platform.Caller{}, ID), "", "", "")
		if err != nil {
			return nil, err
		}
		selected := map[JointDraftRef]bool{{Kind: kind, ID: id}: true}
		for _, change := range changes {
			selected[JointDraftRef{Kind: change.Ref.Kind, ID: change.ID}] = true
		}
		drafts, err := b.applicationDrafts(id, selected)
		return slices.DeleteFunc(drafts, func(draft JointDraftRef) bool { return draft.Kind == kind && draft.ID == id }), err
	}
	if !recordKind(kind) {
		return nil, fmt.Errorf("%s drafts have no record dependencies to select", kind)
	}
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	find := func(want platform.AssetKind, name string) (JointDraftRef, bool) {
		switch want {
		case platform.AssetObject:
			for _, o := range objects {
				if o.Published == "" && (o.Name == name || TypeOf(o.Name) == name) {
					return JointDraftRef{Kind: want, ID: o.ID}, true
				}
			}
		case platform.AssetPage:
			for _, p := range pages {
				if p.Published == "" && (p.Name == name || TypeOf(p.Name) == name) {
					return JointDraftRef{Kind: want, ID: p.ID}, true
				}
			}
		case platform.AssetFlow:
			for _, p := range processes {
				if p.Published == "" && (p.Name == name || TypeOf(p.Name) == name) {
					return JointDraftRef{Kind: want, ID: p.ID}, true
				}
			}
		}
		return JointDraftRef{}, false
	}
	var result []JointDraftRef
	seen := map[JointDraftRef]bool{{Kind: kind, ID: id}: true}
	queue := []JointDraftRef{{Kind: kind, ID: id}}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		dependencies := map[platform.AssetKind][]string{}
		switch next.Kind {
		case platform.AssetObject:
			i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved object draft %s", next.ID)
			}
			for _, field := range objects[i].Fields {
				if field.Type == "reference" && field.Ref != "" {
					dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], field.Ref)
				}
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return p.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved page draft %s", next.ID)
			}
			if pages[i].Object != "" {
				dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], pages[i].Object)
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return a.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved application draft %s", next.ID)
			}
			for _, name := range apps[i].Pages {
				dependencies[platform.AssetPage] = append(dependencies[platform.AssetPage], name)
			}
			for _, resource := range apps[i].Resources {
				dependencies[resource.Kind] = append(dependencies[resource.Kind], resource.Name)
			}
		case platform.AssetFlow:
			i := slices.IndexFunc(processes, func(p Process) bool { return p.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved workflow draft %s", next.ID)
			}
			if processes[i].Object != "" {
				dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], processes[i].Object)
			}
		}
		for want, names := range dependencies {
			if !recordKind(want) {
				continue // the owner that owns that draft reports its own gap
			}
			for _, name := range names {
				ref, ok := find(want, name)
				if !ok || seen[ref] {
					continue
				}
				if len(seen) >= JointDraftLimit {
					return nil, fmt.Errorf("a joint candidate accepts at most %d drafts", JointDraftLimit)
				}
				seen[ref] = true
				result = append(result, ref)
				queue = append(queue, ref)
			}
		}
	}
	return result, nil
}

// ReleaseAssets returns the builder's installed, unfiltered definitions. The
// member-facing Definitions projection must never be used to calculate a
// release identity: it omits rules and fields the reader cannot see.
func (b *Build) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	assets, err := releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	if err != nil {
		return nil, err
	}
	for _, read := range []func() ([]platform.ReleaseAsset, error){
		b.functionAssets, b.agentAssets, b.queryAssets, b.linkTypeAssets, b.propertyTypeAssets,
		b.CodeReleaseAssets, b.alertAssets, b.integrationAssets, b.testPlanAssets, b.sampleSetAssets,
	} {
		owned, err := read()
		if err != nil {
			return nil, err
		}
		assets = append(assets, owned...)
	}
	assets, err = b.bindConfirmationAssets(assets)
	if err != nil {
		return nil, err
	}
	return b.bindDispatchAssets(assets)
}

// FlowReleaseAsset reads a retained native version, not the current draft or
// latest installed version. The host closes its bindings through all owners.
func (b *Build) FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error) {
	processes, err := b.processInventory()
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
			p, ok := ReadPublication[Process](raw)
			if ok && p.Name == name && p.Version == version {
				return processReleaseAsset(p, b.Manifest().Version)
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("process %s version %d is not retained", name, version)
}

func (b *Build) releaseInventory() ([]Object, []Page, []Application, error) {
	if b.host == nil {
		return nil, nil, nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, ID)
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder objects: %w", err)
	}
	pages, err := readDefinitionInventory[Page](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder pages: %w", err)
	}
	apps, err := readDefinitionInventory[Application](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder applications: %w", err)
	}
	return objects, pages, apps, nil
}

// The host caps one read page at 500; the existing release bound is 1000.
// Read the complete bounded inventory or fail, never silently omit assets.
func readDefinitionInventory[T any](c platform.Caller) ([]T, error) {
	rows, err := readDefinitionInventoryOf(c, reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	var all []T
	for _, row := range rows {
		all = append(all, row.(T))
	}
	return all, nil
}

func readDefinitionInventoryOf(c platform.Caller, typ reflect.Type) ([]any, error) {
	var all []any
	for {
		rows, count, err := c.FindOf(typ, platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
		if err != nil {
			return nil, err
		}
		if count > 1000 {
			return nil, fmt.Errorf("builder release inventory exceeds 1000 definitions of one kind")
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("builder definition inventory is incomplete")
		}
	}
}

// DraftReleaseAssets substitutes precisely one saved draft in the owner's
// inventory. It does not install or publish it. Both snapshots are returned
// so the host can compare identical code dependencies under one tenant lock.
// Only the authenticated builder-facing host path may call this method.
// DraftReleaseAssets substitutes precisely one saved draft: the owner's own
// comparison for an asset kind, or one joint record substitution. The multi
// path is DraftReleaseAssetsMulti.
func (b *Build) DraftReleaseAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return b.singleDraftAssets(kind, id)
}

// PublishedPage reports whether the generated page of an object was
// intentionally replaced by an explicit installed page of the same name.
func (b *Build) PublishedPage(name string) (bool, error) {
	_, pages, _, err := b.releaseInventory()
	if err != nil {
		return false, err
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		saved, ok := ReadPublication[Page](record.Published)
		if !ok {
			return false, fmt.Errorf("invalid published page %s", record.ID)
		}
		if saved.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (b *Build) validateObjectInstallation(c platform.Caller, record Object) error {
	if processes := b.host.BuildProcesses(); processes != nil {
		current, exists := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
		if previous, ok := ReadPublication[Object](current.Published); exists && ok {
			oldBody, err := definitionBody(previous, TypeOf(previous.Name))
			if err != nil {
				return err
			}
			newBody, err := definitionBody(record, TypeOf(record.Name))
			if err != nil {
				return err
			}
			if string(oldBody) != string(newBody) {
				running, err := processes.HasUnboundRunningDependency(TypeOf(previous.Name))
				if err != nil {
					return err
				}
				if running && !CompatibleObjectExpansion(previous, record) {
					return fmt.Errorf("finish the running processes before changing their object definition")
				}
			}
		}
	}
	entity := Entity(record)
	processes, err := b.processInventory()
	if err != nil {
		return err
	}
	current, _ := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
	previous, _ := ReadPublication[Object](current.Published)
	if !b.jointMode {
		for _, process := range processes {
			if process.Published == "" {
				continue
			}
			raw, _ := json.Marshal(process)
			installed, err := ReadProcessImage(raw)
			if err != nil {
				return err
			}
			if installed.Object != entity.Type && installed.Object != TypeOf(previous.Name) {
				continue
			}
			if problem := b.checkFlowOn(installed, entity); problem != nil {
				return fmt.Errorf("%s", problem.Message)
			}
		}
	}
	pages, generated, err := b.objectInstallationPages(record)
	if err != nil {
		return err
	}
	if b.jointMode {
		return nil // The complete private installation checks replaced and retained dependents together.
	}
	return b.host.ValidateInstallDependents(c, entity, objectActions(record, entity), generated, pages...)
}

// An explicitly published page owns its name after it replaces an object's
// generated page. Updating the object must not silently overwrite that page.
func (b *Build) objectInstallationPages(record Object) ([]platform.Page, string, error) {
	explicit, err := b.PublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{CompileObjectPage(record)}, record.Name, nil
}

// releaseAssets uses the saved publication, never a later mutable draft. The
// object body retains every declarative field (including access, conditions
// and approvals), excluding only record identity/stamps and editor state.
// An action depends on that complete object, so changing a rule changes its
// closed release even when the action's display metadata stays the same.
func releaseAssets(objects []Object, pages []Page, apps []Application, processes []Process, sourceVersion string) ([]platform.ReleaseAsset, error) {
	var assets []platform.ReleaseAsset
	index := map[platform.AssetRef]int{}
	add := func(asset platform.ReleaseAsset, replacesGeneratedPage bool) error {
		if i, exists := index[asset.Ref]; exists {
			if replacesGeneratedPage && asset.Ref.Kind == platform.AssetPage {
				assets[i] = asset
				return nil
			}
			return fmt.Errorf("builder release asset %s is declared twice", asset.Ref)
		}
		index[asset.Ref] = len(assets)
		assets = append(assets, asset)
		return nil
	}
	for _, record := range objects {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Object
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published object %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published object %s belongs to another record", record.ID)
		}
		ref := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(saved.Name)}
		body, err := definitionBody(saved, ref.Name)
		if err != nil {
			return nil, err
		}
		asset, err := platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
		for _, action := range objectActions(saved, Entity(saved)) {
			actionBody, err := json.Marshal(struct {
				Schema string          `json:"schema"`
				Target string          `json:"target"`
				Roles  []string        `json:"roles"`
				Action platform.Action `json:"action"`
			}{Schema: action.Schema, Target: action.Target, Roles: action.Roles, Action: action})
			if err != nil {
				return nil, fmt.Errorf("encode action %s: %w", action.Schema, err)
			}
			asset, err := platform.WithDependencies(platform.ReleaseAsset{
				Ref:             platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: sourceVersion, Body: actionBody,
			})
			if err != nil {
				return nil, err
			}
			if err := add(asset, false); err != nil {
				return nil, err
			}
		}
		pageAsset, err := platform.PageReleaseAsset(ID, sourceVersion, CompileObjectPage(saved))
		if err != nil {
			return nil, err
		}
		if err := add(pageAsset, false); err != nil {
			return nil, err
		}
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Page
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published page %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published page %s belongs to another record", record.ID)
		}
		asset, err := platform.PageReleaseAsset(ID, sourceVersion, descriptor(saved))
		if err != nil {
			return nil, err
		}
		if err := add(asset, true); err != nil {
			return nil, err
		}
	}
	for _, record := range apps {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Application
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published application %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published application %s belongs to another record", record.ID)
		}
		asset, err := platform.ApplicationReleaseAsset(ID, sourceVersion, applicationDescriptor(saved))
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		saved, err := ReadProcessImage(image)
		if err != nil {
			return nil, err
		}
		asset, err := processReleaseAsset(saved, sourceVersion)
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(assets, func(a, b platform.ReleaseAsset) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return assets, nil
}

func processReleaseAsset(saved Process, sourceVersion string) (platform.ReleaseAsset, error) {
	fl := platform.Flow{Name: saved.Name, Subject: saved.Object}
	ref := platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: TypeOf(fl.Name)}
	var subject platform.AssetRef
	if fl.Subject != "" {
		owner, _, _ := strings.Cut(fl.Subject, ".")
		subject = platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: fl.Subject}
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
				owner = ID
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: step.Function.Name}
			if owner != ID || step.Function.Version == 0 {
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
				owner = ID
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetCompute, Name: step.Operation.Name}
			if owner == ID && step.Operation.Version > 0 {
				operations = append(operations, platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".compute-" + strconv.Itoa(step.Operation.Version)})
			} else {
				dependencies = append(dependencies, ref)
			}
		}
		if step.Query != "" {
			ref := platform.AssetRef{App: step.App, Kind: platform.AssetQuery, Name: step.Query}
			if step.App == ID && step.QueryVersion > 0 {
				queries = append(queries, platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".query-" + strconv.Itoa(step.QueryVersion)})
			} else {
				dependencies = append(dependencies, ref)
			}
		}
		if step.Flow != "" {
			name := step.Flow
			if !strings.HasPrefix(name, ID+".") {
				name = TypeOf(name)
			}
			dependencies = append(dependencies, platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: name})
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
	definition := json.RawMessage(published(saved))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(definition, &fields); err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "version", "layout"} {
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

// ProcessFromAsset reconstructs the bounded owner definition, checking
// that its compiled bindings are exactly the ones in the release envelope.
// A sandbox assigns its own native version ordinal; that counter is not the
// semantic content identity or a dependency version.
func (b *Build) ProcessFromAsset(asset platform.ReleaseAsset) (Process, error) {
	var envelope platform.FlowReleaseDescriptor
	var p Process
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetFlow || asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil {
		return p, fmt.Errorf("unsupported process release asset %s", asset.Ref)
	}
	if err := json.Unmarshal(asset.Body, &envelope); err != nil {
		return p, err
	}
	if err := json.Unmarshal(envelope.Definition, &p); err != nil {
		return p, err
	}
	expected, err := processReleaseAsset(p, asset.SourceVersion)
	if err != nil {
		return p, err
	}
	var got, want any
	if json.Unmarshal(asset.Body, &got) != nil || json.Unmarshal(expected.Body, &want) != nil || expected.Ref != asset.Ref || !reflect.DeepEqual(got, want) {
		return p, fmt.Errorf("process %s definition and compiled bindings differ", asset.Ref)
	}
	p.ID, p.Version, p.State = asset.Ref.Name, 1, "published"
	p.Published = published(p)
	p.Versions = []string{p.Published}
	return p, nil
}

func definitionBody(value Object, typ string) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "installed", "published"} {
		delete(body, key)
	}
	body["type"], err = json.Marshal(typ)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
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
	ObjectBindings   map[string]platform.AssetRef    `json:"objectBindings,omitempty"`
	Name             string                          `json:"name"`
	Title            string                          `json:"title"`
	Input            string                          `json:"input"`
	Steps            []platform.TransformStep        `json:"steps"`
	Expectations     []platform.TransformExpectation `json:"expectations,omitempty"`
	OutputDataset    string                          `json:"outputDataset,omitempty"`
	OutputObject     string                          `json:"outputObject,omitempty"`
	OutputEnterprise *EnterpriseTarget               `json:"outputEnterprise,omitempty"`
	Key              string                          `json:"key,omitempty"`
	Every            string                          `json:"every,omitempty"`
}

type WritebackDefinition struct {
	ObjectBindings map[string]platform.AssetRef `json:"objectBindings,omitempty"`
	Name           string                       `json:"name"`
	Title          string                       `json:"title"`
	Connection     string                       `json:"connection"`
	Object         string                       `json:"object"`
	On             string                       `json:"on"`
	Method         string                       `json:"method,omitempty"`
	Transport      string                       `json:"transport,omitempty"`
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
		if err := CheckWritebackTransport(d.Transport, d.Method); err != nil {
			return err
		}
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

type JointDraftAssets struct {
	Before []platform.ReleaseAsset `json:"-"`
	After  []platform.ReleaseAsset `json:"-"`
	Priors []platform.AssetRef     `json:"-"`
	Nexts  []platform.AssetRef     `json:"-"`
}

const JointDraftLimit = 128

// JointDraftRef names one saved draft in a joint candidate.
type JointDraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}

// DraftChange reads an owner declaration, not additional publication state.
type DraftChange struct {
	Ref        platform.AssetRef  `json:"ref"`
	Type       string             `json:"type"`
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Variant    string             `json:"variant,omitempty"`
	Published  bool               `json:"published"`
	Changed    platform.Stamp     `json:"changed"`
	Fields     []DraftChangeField `json:"fields"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

// Lists carry names only; the selected draft's read includes field values.
type DraftChangeField struct {
	Name   string          `json:"name"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Adding an optional scalar cannot change the meaning of an existing process:
// every old field, state, access rule and action remains exactly as declared.
// New actions may consume the new field; host activation still needs a plan
// when the object already stores rows.
func CompatibleObjectExpansion(previous, next Object) bool {
	if previous.Name != next.Name || len(next.Fields) <= len(previous.Fields) || len(previous.States) != len(next.States) || len(previous.Access) != len(next.Access) {
		return false
	}
	if !reflect.DeepEqual(previous.Constraints, next.Constraints) {
		return false // changing invariants is not an optional storage-only expansion
	}
	for i, state := range previous.States {
		if !reflect.DeepEqual(state, next.States[i]) {
			return false
		}
	}
	for i, access := range previous.Access {
		if !reflect.DeepEqual(access, next.Access[i]) {
			return false
		}
	}
	equal := func(a, b any) bool {
		x, xe := json.Marshal(a)
		y, ye := json.Marshal(b)
		return xe == nil && ye == nil && string(x) == string(y)
	}
	for _, field := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(other Field) bool { return other.Name == field.Name })
		if index < 0 || !equal(field, next.Fields[index]) {
			return false
		}
	}
	for _, action := range previous.Actions {
		index := slices.IndexFunc(next.Actions, func(other Action) bool { return other.Name == action.Name })
		if index < 0 || !equal(action, next.Actions[index]) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}
