package construction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Portable definitions are typed owner contracts. Local identities, addresses,
// credentials and mutable data/execution observations never enter these shapes.
type ConnectionDefinition struct {
	Name       string                  `json:"name"`
	Title      string                  `json:"title"`
	Kind       string                  `json:"kind"`
	Marking    string                  `json:"marking,omitempty"`
	Credential bool                    `json:"credential,omitempty"`
	Protocol   *platform.DriverBinding `json:"protocol,omitempty"`
}

// PortableDriverBinding keeps the logical point/command shape, never site addresses or credentials.
func PortableDriverBinding(binding *platform.DriverBinding) *platform.DriverBinding {
	if binding == nil {
		return nil
	}
	var out platform.DriverBinding
	_ = json.Unmarshal(platform.Raw(binding), &out)
	out.Configuration.Connection = nil
	out.Configuration.Scenario, out.Configuration.Seed = "", 0
	for i := range out.Configuration.Devices {
		out.Configuration.Devices[i].Settings = nil
	}
	for i := range out.Configuration.Points {
		out.Configuration.Points[i].Address = ""
	}
	return &out
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
