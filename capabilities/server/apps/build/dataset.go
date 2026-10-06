package build

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Datasets (ADR-0071, ADR-0069 I-C): someone else's facts kept as they came -
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
	// Marking (ADR-0075): raised by what loads it - the connection, the pipeline's input - and by hand; lowered only by hand.
	Marking string `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking"`
	// Producer is what loads it: the source or pipeline that writes versions (set by them, informational).
	Producer string         `json:"producer,omitempty" field:"readonly"`
	Schema   []DatasetField `json:"schema,omitempty" field:"readonly" type:"json"`
	Version  int            `json:"version,omitempty" field:"readonly" title:"Latest version"`
	Last     *DatasetLoad   `json:"last,omitempty" field:"readonly" type:"json" title:"Latest load"`
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
}

func (b *Build) datasetEntity() platform.Entity {
	return platform.Entity{Type: DatasetType, Title: "Dataset", Plural: "Datasets", Model: Dataset{}, Display: "title",
		Description: "Rows as they came from a source or a pipeline: versioned, schema inferred, read only by builders.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"}}
}

func (b *Build) datasetVersionEntity() platform.Entity {
	return platform.Entity{Type: DatasetVersionType, Title: "Dataset version", Plural: "Dataset versions", Model: DatasetVersion{}, Display: "version",
		Description: "One version of a dataset: its rows at one load.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Roles: []string{Builder, Integrator}, Capability: "integrations"}}
}

func datasetActions() []platform.Action {
	return []platform.Action{{Schema: SchemaDatasetLoad, Target: DatasetType, Capability: "integrations", Title: "Load a version", Description: "Append the rows one pull or run produced as the dataset's next version.", Roles: []string{Builder, Integrator},
		Payload: []platform.Field{{Name: "rows", Type: "json", Required: true, Description: "The rows, an array of objects"}, {Name: "producer", Type: "string", Description: "The source or pipeline that produced them"}, {Name: "marking", Type: "string", Description: "The marking the rows carry; raises the dataset's"}}}}
}

// DatasetSchema infers columns from rows: the widest type each column shows.
func DatasetSchema(rows []map[string]any) []DatasetField {
	types := map[string]string{}
	for _, row := range rows {
		for k, v := range row {
			t := typeOf(v)
			if prev, ok := types[k]; !ok || prev == "" {
				types[k] = t
			} else if prev != t && t != "" {
				types[k] = "string"
			}
		}
	}
	out := make([]DatasetField, 0, len(types))
	for k, t := range types {
		if t == "" {
			t = "string"
		}
		out = append(out, DatasetField{Name: k, Type: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func typeOf(v any) string {
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

func (b *Build) submitDatasetLoad(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Rows     []map[string]any `json:"rows"`
			Producer string           `json:"producer"`
			Marking  string           `json:"marking"`
		}
		ds, ok := platform.Get[Dataset](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dataset does not exist")
		}
		if json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Rows == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A load carries an array of rows")
		}
		if len(payload.Rows) > sourceRows {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A version holds at most {n} rows", sourceRows)
		}
		return func(r *pb.ChangeRecord) {
			schema := DatasetSchema(payload.Rows)
			var drift []string
			before := map[string]bool{}
			for _, f := range ds.Schema {
				before[f.Name] = true
			}
			after := map[string]bool{}
			for _, f := range schema {
				after[f.Name] = true
				if ds.Version > 0 && !before[f.Name] {
					drift = append(drift, "+"+f.Name)
				}
			}
			for name := range before {
				if !after[name] {
					drift = append(drift, "-"+name)
				}
			}
			sort.Strings(drift)
			data, _ := json.Marshal(payload.Rows)
			ds.Version++
			ds.Schema = schema
			ds.Last = &DatasetLoad{At: now, Version: ds.Version, Rows: len(payload.Rows), Bytes: len(data), Drift: drift}
			if payload.Producer != "" {
				ds.Producer = payload.Producer
			}
			ds.Marking = HigherMarking(ds.Marking, payload.Marking)
			c.Put(r, ds)
			c.Put(r, DatasetVersion{Record: platform.Record{ID: VersionID(ds.ID, ds.Version)}, Dataset: ds.ID, Version: ds.Version, At: now, Rows: len(payload.Rows), Data: data})
			keep := ds.Keep
			if keep <= 0 {
				keep = DatasetKeepDefault
			}
			if old := ds.Version - keep; old >= 1 {
				if v, ok := platform.Get[DatasetVersion](c, VersionID(ds.ID, old)); ok && !v.Archived {
					v.Archived, v.Data = true, json.RawMessage("[]")
					c.Put(r, v)
				}
			}
		}, nil
	})
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
