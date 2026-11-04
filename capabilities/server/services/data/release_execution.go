package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"time"
)

func (o *Owner) ExecutionSource(live Source) (Source, error) {
	fixed, err := o.acceptedIntegration(live, platform.AssetSource)
	if err != nil {
		return Source{}, err
	}
	r := fixed.(Source)
	r.Publisher, r.PublisherScopes = live.Publisher, live.PublisherScopes
	r.Record, r.State, r.Puller, r.PullerScopes, r.Cursor, r.Requested, r.Last = live.Record, live.State, live.Puller, live.PullerScopes, live.Cursor, live.Requested, live.Last
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (o *Owner) ConnectionConfiguration(live Connection) (Connection, error) {
	fixed, err := o.acceptedIntegration(live, platform.AssetConnection)
	if err != nil {
		return Connection{}, err
	}
	r := fixed.(Connection)
	if r.Kind != live.Kind {
		return Connection{}, fmt.Errorf("connection binding kind differs from its accepted definition")
	}
	r.Record, r.Address, r.Secret, r.AllowPrivate, r.State, r.Requested, r.Last = live.Record, live.Address, live.Secret, live.AllowPrivate, live.State, live.Requested, live.Last
	r.Marking = platform.HigherMarking(r.Marking, live.Marking)
	r.Compartments = platform.JoinCompartments(r.Compartments, live.Compartments)
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (o *Owner) ExecutionConnection(live Connection) (Connection, error) {
	connection, err := o.ConnectionConfiguration(live)
	if err != nil {
		return Connection{}, err
	}
	if connection.Archived || connection.State != "ready" || connection.Address == "" {
		return Connection{}, fmt.Errorf("connection is not ready; configure and check it before use")
	}
	if problem := o.validConnection(connection); problem != nil {
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

func (o *Owner) ExecutionDataset(live Dataset) (Dataset, error) {
	fixed, err := o.acceptedIntegration(live, platform.AssetDataset)
	if err != nil {
		return Dataset{}, err
	}
	r := fixed.(Dataset)
	r.Record, r.Producer, r.Schema, r.Version, r.Last = live.Record, live.Producer, live.Schema, live.Version, live.Last
	r.Marking = platform.HigherMarking(r.Marking, live.Marking)
	r.Compartments = platform.JoinCompartments(r.Compartments, live.Compartments)
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (o *Owner) ExecutionPipeline(live Pipeline) (Pipeline, error) {
	fixed, err := o.acceptedIntegration(live, platform.AssetPipeline)
	if err != nil {
		return Pipeline{}, err
	}
	r := fixed.(Pipeline)
	r.Record, r.State, r.Runner, r.RunnerScopes, r.Requested, r.Last, r.Marking, r.Compartments = live.Record, live.State, live.Runner, live.RunnerScopes, live.Requested, live.Last, live.Marking, live.Compartments
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (o *Owner) ExecutionWriteback(live Writeback) (Writeback, error) {
	fixed, err := o.acceptedIntegration(live, platform.AssetWriteback)
	if err != nil {
		return Writeback{}, err
	}
	r := fixed.(Writeback)
	r.Record, r.State, r.Publisher, r.Sent, r.Failed, r.Rejected, r.Answers = live.Record, live.State, live.Publisher, live.Sent, live.Failed, live.Rejected, live.Answers
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (o *Owner) ExecutionMatching(live Match) (Match, error) {
	fixed, err := o.acceptedIntegration(live, platform.AssetMatching)
	if err != nil {
		return Match{}, err
	}
	r := fixed.(Match)
	r.Record, r.State, r.Publisher = live.Record, live.State, live.Publisher
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (o *Owner) matchingProducer(name string) (platform.AssetRef, error) {
	var matches []platform.AssetRef
	for _, kind := range []platform.AssetKind{platform.AssetSource, platform.AssetPipeline} {
		rows, err := o.integrationInventory(kind)
		if err != nil {
			return platform.AssetRef{}, err
		}
		for _, row := range rows {
			n, _, _, _ := integrationMetadata(row)
			if n == name {
				matches = append(matches, platform.AssetRef{App: App, Kind: kind, Name: n})
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

func (o *Owner) freezeDirectIntegration(kind platform.AssetKind, record any) (any, error) {
	_, ordinal, raw, _ := integrationMetadata(record)
	asset, err := o.integrationAsset(kind, record, ordinal+1)
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
	if err := o.validateIntegrationUpgrade(platform.AssetInstallation{Ref: asset.Ref, Schema: integrationDescriptor(kind).publication(), Record: integrationDescriptor(kind).record(integrationRecord(fixed).ID), Image: image}); err != nil {
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

func EqualAsset(a, z platform.ReleaseAsset) bool { return equalIntegrationAsset(a, z) }

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
		definition.ObjectBindings = nil
		return json.Marshal(definition)
	case platform.AssetPipeline:
		var definition PipelineDefinition
		if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
			return nil, err
		}
		definition.Title = ""
		definition.ObjectBindings = nil
		return json.Marshal(definition)
	}
	return nil, fmt.Errorf("unsupported execution definition")
}

type ConnectionDefinition struct {
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Kind         string   `json:"kind"`
	Marking      string   `json:"marking,omitempty"`
	Compartments []string `json:"compartments,omitempty"`
	Credential   bool     `json:"credential,omitempty"`
}

type ListenerDefinition struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Connector string `json:"connector"`
	Path      string `json:"path"`
	Secret    string `json:"secret"`
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
	Key          string   `json:"key,omitempty"`
	Stream       bool     `json:"stream,omitempty"`
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Keep         int      `json:"keep,omitempty"`
	Marking      string   `json:"marking,omitempty"`
	Compartments []string `json:"compartments,omitempty"`
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
	return integrationEntryOfKind(kind) != nil
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
			expected = append(expected, platform.AssetRef{App: App, Kind: kind, Name: name})
		}
	}
	object := func(name string) {
		if name == "" {
			return
		}
		owner := App
		if prefix, _, ok := strings.Cut(name, "."); ok {
			owner = prefix
		}
		expected = append(expected, platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: name})
	}
	switch asset.Ref.Kind {
	case platform.AssetSchedule:
		var d ScheduleDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return err
		}
		add(platform.AssetPipeline, d.Pipeline)
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
		legacy := platform.AssetRef{App: App, Kind: platform.AssetAction, Name: d.Object + ".confirmation"}
		if !slices.Contains([]string{"confirmation", "dispatch", "transaction"}, d.On) || d.On == "confirmation" && slices.Contains(asset.Requires, legacy) {
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
				if ref.App == App && (ref.Kind == platform.AssetSource || ref.Kind == platform.AssetPipeline) && ref.Name == preference.Producer {
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
	if asset.Ref.App != App || !ValueIntegrationKind(asset.Ref.Kind) || asset.ContractVersion != 1 {
		return fmt.Errorf("unsupported integration descriptor")
	}
	if _, err := ValueIntegrationOrdinal(asset); err != nil {
		return err
	}
	var record any
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		record = new(ConnectionDefinition)
	case platform.AssetListener:
		record = new(ListenerDefinition)
	case platform.AssetSource:
		record = new(SourceDefinition)
	case platform.AssetMediaSet:
		record = new(MediaSetDefinition)
	case platform.AssetSchedule:
		record = new(ScheduleDefinition)
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
	case *ListenerDefinition:
		if !ontology.Named(d.Name) || !listenerSegment.MatchString(d.Path) || !listenerSegment.MatchString(d.Secret) || strings.TrimSpace(d.Connector) == "" {
			return fmt.Errorf("invalid portable listener")
		}
	case *SourceDefinition:
		if d.Connection == "" || strings.Contains(d.URL, "://") || !slices.Contains([]string{"", "json", "csv", "odata", "table"}, d.Profile) {
			return fmt.Errorf("invalid portable source")
		}
	case *ScheduleDefinition:
		every, err := time.ParseDuration(d.Every)
		if d.Pipeline == "" || err != nil || every < time.Minute || every > 366*24*time.Hour {
			return fmt.Errorf("invalid schedule definition")
		}
	case *MediaSetDefinition:
		if err := CheckMediaDefinition(d.Items, d.History, d.Blobs); err != nil {
			return err
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
