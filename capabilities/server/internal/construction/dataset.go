package construction

import (
	"encoding/json"
	"fmt"

	"time"

	"platformserver/platform"
)

// Datasets (ADR-0024、ADR-0062 I-C): someone else's facts kept as they came -
// a sequence of versions, each the rows one pull or one pipeline run produced,
// with the schema inferred from them. A dataset is not a business object: no
// business permission reads it, only the builder; a pipeline turns it into
// records, and every record can say which version and row it came from.
const (
	DatasetType        = "build.dataset"
	DatasetVersionType = "build.datasetversion"
	SchemaDatasetLoad  = "build.dataset.load"
	DatasetKeepDefault = 3
)

type Dataset struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Keep is how many versions stay; older ones are archived.
	Keep int `json:"keep,omitempty" title:"Versions kept" help:"How many versions stay readable; default 3"`
	// Marking (ADR-0025): raised by what loads it - the connection, the pipeline's input - and by hand; lowered only by hand.
	Marking string `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking"`
	// Producer is what loads it: the source or pipeline that writes versions (set by them, informational).
	Producer string         `json:"producer,omitempty" field:"readonly"`
	Schema   []DatasetField `json:"schema,omitempty" field:"readonly" type:"json"`
	Version  int            `json:"version,omitempty" field:"readonly" title:"Latest version"`
	Last     *DatasetLoad   `json:"last,omitempty" field:"readonly" type:"json" title:"Latest load"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// DatasetField is one inferred column: its name and the widest type seen.
type DatasetField struct {
	Name string `json:"name"`
	Type string `json:"type"` // string, number, boolean, date, json
}

// DatasetLoad is what one version holds.
type DatasetLoad struct {
	At      time.Time `json:"at"`
	Version int       `json:"version"`
	Rows    int       `json:"rows"`
	Bytes   int       `json:"bytes"`
	// Drift lists columns that appeared or vanished since the previous version.
	Drift []string `json:"drift,omitempty"`
}

// DatasetVersion is one version's rows, kept until the dataset's Keep passes it.
type DatasetVersion struct {
	platform.Record
	Dataset string          `json:"dataset" field:"required" ref:"build.dataset"`
	Version int             `json:"version" field:"required"`
	At      time.Time       `json:"at"`
	Rows    int             `json:"rows"`
	Data    json.RawMessage `json:"data" type:"json"`
	Request string          `json:"requestKey,omitempty" field:"readonly" title:"Request key"`
}

func ValueTypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		if _, err := time.Parse(time.RFC3339, x); err == nil {
			return "date"
		}
		if len(x) == 10 {
			if _, err := time.Parse("2006-01-02", x); err == nil {
				return "date"
			}
		}
		return "string"
	default:
		return "json"
	}
}

// VersionID names a dataset's version record.
func VersionID(dataset string, version int) string { return fmt.Sprintf("%s@%d", dataset, version) }

// Rows are a dataset version's rows (the latest when version is 0).
func DatasetRows(c platform.Caller, dataset string, version int) ([]map[string]any, int, error) {
	ds, ok := platform.Get[Dataset](c, dataset)
	if !ok {
		return nil, 0, fmt.Errorf("the dataset %s does not exist", dataset)
	}
	if version <= 0 {
		version = ds.Version
	}
	if version == 0 {
		return nil, 0, nil
	}
	v, ok := platform.Get[DatasetVersion](c, VersionID(dataset, version))
	if !ok {
		return nil, 0, fmt.Errorf("version %d of %s is no longer kept", version, ds.Title)
	}
	var rows []map[string]any
	if err := json.Unmarshal(v.Data, &rows); err != nil {
		return nil, 0, fmt.Errorf("version %d of %s is unreadable", version, ds.Title)
	}
	return rows, version, nil
}
