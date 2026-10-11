package build

import (
	"encoding/json"
	buildapi "platformserver/internal/host/buildapi"

	"sort"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

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
			t := buildapi.ValueTypeOf(v)
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
		fixed, err := b.ExecutionDataset(ds)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
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
			ds.Marking = buildapi.HigherMarking(ds.Marking, payload.Marking)
			c.Put(r, ds)
			c.Put(r, DatasetVersion{Record: platform.Record{ID: buildapi.VersionID(ds.ID, ds.Version)}, Dataset: ds.ID, Version: ds.Version, At: now, Rows: len(payload.Rows), Data: data})
			keep := fixed.Keep
			if keep <= 0 {
				keep = DatasetKeepDefault
			}
			if old := ds.Version - keep; old >= 1 {
				if v, ok := platform.Get[DatasetVersion](c, buildapi.VersionID(ds.ID, old)); ok && !v.Archived {
					v.Archived, v.Data = true, json.RawMessage("[]")
					c.Put(r, v)
				}
			}
		}, nil
	})
}
