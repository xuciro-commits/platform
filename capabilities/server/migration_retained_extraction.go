package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type retainedExtractionWrite struct {
	typ           string
	before, after map[string]json.RawMessage
	origin        records.Extraction
}

func (r *flowExecutionRuntime) applyRetainedExtractions(c platform.Caller, receipt *pb.ChangeRecord, writes []retainedExtractionWrite) *kernel.Error {
	if len(writes) > platform.TxnMaxSteps {
		return migrationRefusal(fmt.Errorf("retained extraction exceeds its decision budget"))
	}
	for _, write := range writes {
		et := r.records.types[write.typ]
		if et == nil || et.info.App != c.App {
			return migrationRefusal(fmt.Errorf("retained extraction crosses its authority"))
		}
		var id string
		_ = json.Unmarshal(write.after["id"], &id)
		prior := et.rows.Get(id)
		values := maps.Clone(write.after)
		kind := "create"
		if prior != nil {
			if prior.Extraction == nil || *prior.Extraction != write.origin || records.Header(prior.Value).Archived {
				return migrationRefusal(fmt.Errorf("retained extraction identity belongs to another source"))
			}
			kind = "write"
			raw, err := prior.Image()
			if err != nil {
				return migrationRefusal(err)
			}
			if err := json.Unmarshal(raw, &values); err != nil {
				return migrationRefusal(err)
			}
		} else if write.before != nil {
			return migrationRefusal(fmt.Errorf("retained extraction record disappeared"))
		}
		fields := []string{}
		for _, field := range et.info.Fields {
			if string(write.before[field.Name]) == string(write.after[field.Name]) {
				continue
			}
			name := field.Name
			if field.AliasOf != "" {
				field, _ = et.info.Field(field.AliasOf)
			}
			if field.ReadOnly && (et.info.Lifecycle == nil || et.info.Lifecycle.Field != field.Name) {
				return migrationRefusal(fmt.Errorf("retained extraction writes a retired field"))
			}
			fields = append(fields, field.Name)
			values[field.Name] = write.after[name]
		}
		if len(fields) == 0 {
			continue
		}
		if prior == nil && et.info.Lifecycle != nil {
			values[et.info.Lifecycle.Field] = platform.Raw(et.info.Lifecycle.Initial)
		}
		question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: kind, Type: write.typ, Record: id, Fields: fields}
		grant := r.tenant.authorizeFrom(r.records, c, question, r.at, false)
		if err := authorizationRefusal(grant); err != nil {
			return err
		}
		r.RecordAuthorization(c, question, grant)
		v := reflect.New(et.info.Go)
		if err := json.Unmarshal(jsonOf(values), v.Interface()); err != nil {
			return migrationRefusal(err)
		}
		if err := r.stagedDecision.Check(c, v.Elem().Interface()); err != nil {
			return err
		}
		if err := r.stagedDecision.Put(c, receipt, v.Elem().Interface()); err != nil {
			return err
		}
		origin := write.origin
		updated := *et.rows.Get(id)
		updated.Extraction = &origin
		et.setRow(id, &updated)
	}
	return nil
}

// Origin is immutable host provenance, never a writable business field.
func validateExtractionOrigins(recordView *recordStore) error {
	for ref := range recordView.writes {
		typ, id, _ := strings.Cut(ref, "/")
		et := recordView.types[typ]
		if et == nil || et.rows.Get(id) == nil {
			return fmt.Errorf("extraction row is absent")
		}
		if err := validateExtractionOrigin(recordView, typ, et.rows.Get(id).Extraction); err != nil {
			return err
		}
	}
	return nil
}

func validateExtractionOrigin(recordView *recordStore, typ string, origin *records.Extraction) error {
	if origin == nil {
		return nil
	}
	runs := recordView.types[platform.MigrationRunType]
	if runs == nil || runs.rows.Get(origin.Migration) == nil {
		return fmt.Errorf("extraction has no saved declaration")
	}
	run := runs.rows.Get(origin.Migration).Value.Interface().(platform.MigrationRun)
	if origin.Step < 0 || origin.Step >= len(run.Plan.Steps) {
		return fmt.Errorf("extraction has no declared step")
	}
	step := run.Plan.Steps[origin.Step]
	sourceType, sourceID, found := strings.Cut(origin.Source, "/")
	source := recordView.types[sourceType]
	if !found || sourceID == "" || step.Kind != "extract" || step.Target != typ || !slices.Contains(step.Types, sourceType) || source == nil || source.rows.Get(sourceID) == nil {
		return fmt.Errorf("extraction origin differs from its declared scope")
	}
	return nil
}
