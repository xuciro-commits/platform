package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func migrationRefusal(err error) *kernel.Error {
	if err == nil {
		return nil
	}
	if refusal, ok := err.(*kernel.Error); ok {
		return refusal
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}

// A source authority stays readable but cannot accept ordinary writes while
// its target facts are being staged. The accepted run is the durable lock.
func (s *recordStore) migrationWriteLocked(info platform.EntityInfo, r *pb.ChangeRecord) *kernel.Error {
	progress := s.types[platform.MigrationRunType]
	if progress == nil || info.Type == platform.MigrationRunType {
		return nil
	}
	for _, value := range progress.rows.all() {
		run := value.value.Interface().(platform.MigrationRun)
		if run.State == "cancelled" || run.Plan.From != s.readRelease {
			continue
		}
		source, affected := run.SourceShapes[info.Type]
		if !affected {
			source, affected = run.TargetShapes[info.Type]
		}
		if !affected {
			continue
		}
		shape, err := canonicalDigest(info.Fields)
		if err != nil || shape != source {
			continue // The target definition has been installed.
		}
		sub := r.GetSubmission()
		if sub.GetPrincipalId() == "app:"+info.App && s.retiredApprovalReturnLocked(info, sub.GetSchema().GetName()) {
			continue
		}
		if sub.GetAuthority() == info.App && slices.Contains([]string{platform.MigrationRunType + ".advance", platform.MigrationRunType + ".cancel"}, sub.GetSchema().GetName()) && sub.GetTarget().GetType() == platform.MigrationRunType && sub.GetTarget().GetId() == run.ID {
			continue
		}
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This object is frozen by migration {migration}; finish its reviewed definition transition", run.ID)
	}
	return nil
}

func (s *recordStore) migrationDefinitionLocked(info platform.EntityInfo) error {
	progress := s.types[platform.MigrationRunType]
	if progress == nil {
		return nil
	}
	for _, value := range progress.rows.all() {
		run := value.value.Interface().(platform.MigrationRun)
		if run.State == "cancelled" || run.Plan.From != s.readRelease || run.SourceShapes[info.Type] == "" && run.TargetShapes[info.Type] == "" {
			continue
		}
		shape, err := canonicalDigest(info.Fields)
		want := run.SourceShapes[info.Type]
		if want == "" {
			want = run.TargetShapes[info.Type]
		}
		if err != nil || shape != want {
			return fmt.Errorf("object %s is frozen by migration %s; activate its sealed target or cancel its accepted batches", info.Type, run.ID)
		}
	}
	return nil
}

func (d *stagedDecision) BeginMigration(c platform.Caller, r *pb.ChangeRecord, plan platform.MigrationPlan, reviewID string, reviewedAt, at time.Time) (platform.MigrationRun, *kernel.Error) {
	if c.App != "build" || !c.Staging() || r == nil {
		return platform.MigrationRun{}, migrationRefusal(fmt.Errorf("migration is outside its private authority"))
	}
	review, err := d.tenant.reviewMigrationPlanLocked(c.Member, plan, at, reviewedAt)
	if err != nil {
		return platform.MigrationRun{}, migrationRefusal(err)
	}
	if review.ID != reviewID {
		return platform.MigrationRun{}, migrationRefusal(fmt.Errorf("migration review changed; review the current source again"))
	}
	prepared, err := d.tenant.prepareMigrationLocked(plan, c.Member, at, d.records)
	if err != nil {
		return platform.MigrationRun{}, migrationRefusal(err)
	}
	for _, value := range d.records.types[platform.MigrationRunType].rows.all() {
		prior := value.value.Interface().(platform.MigrationRun)
		if prior.State == "cancelled" {
			continue
		}
		for typ := range prior.SourceShapes {
			if _, overlaps := prepared.sources[typ]; overlaps && prior.Plan.From == plan.From {
				return platform.MigrationRun{}, migrationRefusal(fmt.Errorf("migration %s already owns this source; resume its saved progress", prior.ID))
			}
		}
	}
	run := platform.MigrationRun{Record: platform.Record{ID: r.GetSubmission().GetTarget().GetId()}, Plan: plan, Review: reviewID, State: "pending", StartedBy: c.ID, EvaluationAt: reviewedAt.UTC(), SourceShapes: map[string]string{}, TargetShapes: map[string]string{}, Targets: []string{}, Failures: []platform.MigrationFailure{}}
	for typ, target := range prepared.targets {
		shape, err := canonicalDigest(target.Fields)
		if err != nil {
			return run, migrationRefusal(err)
		}
		run.TargetShapes[typ] = shape
	}
	for _, typ := range prepared.types {
		source, found := prepared.sources[typ]
		if !found {
			continue
		}
		if source.App != c.App {
			return platform.MigrationRun{}, migrationRefusal(fmt.Errorf("migration batches must belong to the builder's authority"))
		}
		shape, err := canonicalDigest(source.Fields)
		if err != nil {
			return platform.MigrationRun{}, migrationRefusal(err)
		}
		run.SourceShapes[typ] = shape
		target, err := canonicalDigest(prepared.targets[typ].Fields)
		if err != nil {
			return platform.MigrationRun{}, migrationRefusal(err)
		}
		run.TargetShapes[typ] = target
		for id, row := range d.records.types[typ].rows.all() {
			if !recordOf(row.value).Archived {
				run.Targets = append(run.Targets, typ+"/"+id)
			}
		}
	}
	slices.Sort(run.Targets)
	if err := d.stageExtractionDefinitions(c, r, run, prepared); err != nil {
		return run, err
	}
	return run, nil
}

func (d *stagedDecision) AdvanceMigration(c platform.Caller, r *pb.ChangeRecord, run platform.MigrationRun, at time.Time) (platform.MigrationRun, *kernel.Error) {
	if c.App != "build" || !c.Staging() || r == nil || run.ID == "" {
		return run, migrationRefusal(fmt.Errorf("migration is outside its private authority"))
	}
	if run.State == "cancelled" {
		return run, nil
	}
	if run.State == "cancelling" {
		return d.cancelMigrationBatch(c, r, run)
	}
	if run.State == "complete" {
		return run, nil
	}
	if run.State == "blocked" {
		return run, migrationRefusal(fmt.Errorf("migration is blocked; inspect its recorded failures"))
	}
	if d.tenant.releases.active != run.Plan.From {
		return run, migrationRefusal(fmt.Errorf("migration source release changed"))
	}
	prepared, err := d.tenant.prepareMigrationLocked(run.Plan, c.Member, at, d.records)
	if err != nil {
		return run, migrationRefusal(err)
	}
	for typ, shape := range run.SourceShapes {
		current, err := canonicalDigest(prepared.sources[typ].Fields)
		if err != nil || current != shape {
			return run, migrationRefusal(fmt.Errorf("migration source shape changed"))
		}
		target, err := canonicalDigest(prepared.targets[typ].Fields)
		if err != nil || target != run.TargetShapes[typ] {
			return run, migrationRefusal(fmt.Errorf("migration target shape changed"))
		}
	}
	for typ, shape := range run.TargetShapes {
		actual, err := canonicalDigest(prepared.targets[typ].Fields)
		if err != nil || actual != shape {
			return run, migrationRefusal(fmt.Errorf("migration target shape changed"))
		}
	}
	if run.Step >= len(run.Plan.Steps) {
		run.State = "complete"
		return run, nil
	}
	step := run.Plan.Steps[run.Step]
	if step.Kind == "retire-action" {
		return d.stageActionRetirement(c, run, step)
	}
	run.State = "running"
	before := run
	type migrationWrite struct {
		typ, id      string
		values       map[string]json.RawMessage
		fields       []string
		grant        platform.AuthzDecision
		extract      bool
		source       string
		sourceValues map[string]json.RawMessage
	}
	writes := []migrationWrite{}
	processed := 0
	for _, ref := range run.Targets {
		if ref <= run.Cursor {
			continue
		}
		typ, id, _ := strings.Cut(ref, "/")
		if !slices.Contains(step.Types, typ) {
			run.Cursor = ref
			continue
		}
		et := d.records.types[typ]
		if et == nil || et.rows.get(id) == nil {
			return run, migrationRefusal(fmt.Errorf("migration source record disappeared"))
		}
		fields := []string{}
		for _, f := range et.info.Fields {
			fields = append(fields, f.Name)
		}
		read := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: fields}
		decision := d.tenant.authorizeFrom(d.records, c, read, at, false)
		if err := authorizationRefusal(decision); err != nil {
			return run, err
		}
		d.RecordAuthorization(c, read, decision)
		image, err := et.rows.get(id).image()
		if err != nil {
			return run, migrationRefusal(err)
		}
		result, err := prepared.program.Transform(run.Step, typ, image, platform.ExprContext{Me: run.StartedBy, Now: run.EvaluationAt, Today: run.EvaluationAt.UTC().Format(time.DateOnly)})
		var extraction *migrationWrite
		if err == nil && step.Kind == "extract" && result.Selected {
			var targetID string
			_ = json.Unmarshal(result.Extracted["id"], &targetID)
			target := d.records.types[step.Target]
			if target == nil {
				return run, migrationRefusal(fmt.Errorf("extraction target is not declared"))
			}
			if target.rows.get(targetID) != nil || slices.ContainsFunc(writes, func(w migrationWrite) bool { return w.typ == step.Target && w.id == targetID }) {
				err = fmt.Errorf("extraction identity %s/%s already exists", step.Target, targetID)
			} else {
				extraction = &migrationWrite{typ: step.Target, id: targetID, values: result.Extracted, extract: true, source: ref, sourceValues: result.Values}
				if target.info.Lifecycle != nil {
					extraction.values[target.info.Lifecycle.Field] = platform.Raw(target.info.Lifecycle.Initial)
				}
				for _, f := range step.Fields {
					extraction.fields = append(extraction.fields, f.To)
				}
				v := reflect.New(target.info.Go)
				if e := json.Unmarshal(jsonOf(extraction.values), v.Interface()); e != nil {
					err = e
				} else if refusal := d.Check(c, v.Elem().Interface()); refusal != nil {
					err = refusal
				}
				if err == nil {
					question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "create", Type: step.Target, Record: targetID, Fields: extraction.fields}
					extraction.grant = d.tenant.authorizeFrom(d.records, c, question, at, false)
					if refusal := authorizationRefusal(extraction.grant); refusal != nil {
						return run, refusal
					}
				}
			}
		}
		if err != nil {
			failure := platform.MigrationFailure{Type: typ, Record: id, Step: run.Step + 1, Reason: err.Error()}
			if step.OnFailure != "quarantine" {
				before.State = "blocked"
				before.Failures = append(slices.Clone(before.Failures), failure)
				return before, nil // No row in this failed batch has been put.
			}
			run.Failures = append(run.Failures, failure)
		} else if extraction != nil {
			writes = append(writes, *extraction)
			run.Migrated++
		} else if result.Changed || run.Plan.Scope.Where == nil && (step.Kind == "retype" || step.Kind == "rebind") && run.SourceShapes[typ] != run.TargetShapes[typ] {
			writeFields := []string{}
			for _, f := range step.Fields {
				writeFields = append(writeFields, f.To)
			}
			trialType := prepared.trial.records.types[typ]
			v := reflect.New(trialType.info.Go).Elem()
			raw, err := json.Marshal(result.Values)
			if err != nil || json.Unmarshal(raw, v.Addr().Interface()) != nil {
				return run, migrationRefusal(fmt.Errorf("migration value does not match its target shape"))
			}
			trialType.rows.set(id, &row{value: v, history: copyHistory(et.rows.get(id).history)})
			decision := d.tenant.authorizeFrom(prepared.trial.records, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "write", Type: typ, Record: id, Fields: writeFields}, at, false)
			if err := authorizationRefusal(decision); err != nil {
				return run, err
			}
			writes = append(writes, migrationWrite{typ: typ, id: id, values: result.Values, fields: writeFields, grant: decision})
			run.Migrated++
		}
		run.Processed++
		run.Cursor = ref
		processed++
		if processed >= step.Batch {
			break
		}
	}
	remaining := slices.ContainsFunc(run.Targets, func(ref string) bool {
		typ, _, _ := strings.Cut(ref, "/")
		return ref > run.Cursor && slices.Contains(step.Types, typ)
	})
	if !remaining {
		run.Step++
		run.Cursor = ""
		if run.Step == len(run.Plan.Steps) {
			run.State = "complete"
			if len(run.Failures) > 0 {
				run.State = "blocked"
			}
		}
	}
	for _, write := range writes {
		kind := "write"
		if write.extract {
			kind = "create"
		}
		d.RecordAuthorization(c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: kind, Type: write.typ, Record: write.id, Fields: write.fields}, write.grant)
		if write.extract {
			v := reflect.New(d.records.types[write.typ].info.Go)
			if err := json.Unmarshal(jsonOf(write.values), v.Interface()); err != nil {
				return run, migrationRefusal(err)
			}
			if err := d.Put(c, r, v.Elem().Interface()); err != nil {
				return run, err
			}
			written := d.records.types[write.typ]
			updated := *written.rows.get(write.id)
			updated.extraction = &extractionOrigin{Migration: run.ID, Source: write.source, Step: before.Step}
			written.rows.set(write.id, &updated)
			sourceType, sourceID, _ := strings.Cut(write.source, "/")
			question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "write", Type: sourceType, Record: sourceID}
			grant := d.tenant.authorizeFrom(d.records, c, question, at, false)
			if err := authorizationRefusal(grant); err != nil {
				return run, err
			}
			d.RecordAuthorization(c, question, grant)
			if err := d.putMigrationImage(c, r, d.records.types[sourceType], sourceID, write.sourceValues, run.SourceShapes[sourceType]); err != nil {
				return run, err
			}
			run.Extractions = append(slices.Clone(run.Extractions), platform.MigrationExtraction{Source: write.source, Type: write.typ, ID: write.id, Step: before.Step})
			continue
		}
		if err := d.putMigrationImage(c, r, d.records.types[write.typ], write.id, write.values, run.SourceShapes[write.typ]); err != nil {
			return run, err
		}
	}
	return run, nil
}

func (t *Tenant) migrationActivationLocked(candidate string) (bool, error) {
	progress := t.records.types[platform.MigrationRunType]
	if progress == nil {
		return false, nil
	}
	migrated := false
	for _, value := range progress.rows.all() {
		run := value.value.Interface().(platform.MigrationRun)
		if run.State == "cancelled" || run.Plan.From != t.releases.active {
			continue
		}
		if run.Plan.To != candidate || run.State != "complete" || len(run.Failures) > 0 {
			return false, fmt.Errorf("migration %s must finish without unresolved failures before activating its sealed target", run.ID)
		}
		migrated = true
	}
	return migrated, nil
}

func (t *Tenant) completedActionRetirement(action string) bool {
	progress := t.records.types[platform.MigrationRunType]
	if progress == nil {
		return false
	}
	for _, value := range progress.rows.all() {
		run := value.value.Interface().(platform.MigrationRun)
		if run.Plan.From == t.releases.active && run.State == "complete" && len(run.Failures) == 0 && slices.ContainsFunc(run.Plan.Steps, func(step platform.MigrateStep) bool { return step.Kind == "retire-action" && step.Action == action }) {
			return true
		}
	}
	return false
}

// Cancellation writes compensating facts; it never erases accepted history.
// It uses the captured source image, so it evaluates no migration expression.
func (d *stagedDecision) cancelMigrationBatch(c platform.Caller, r *pb.ChangeRecord, run platform.MigrationRun) (platform.MigrationRun, *kernel.Error) {
	if d.tenant.releases.active != run.Plan.From {
		return run, migrationRefusal(fmt.Errorf("an activated target cannot cancel its source migration"))
	}
	batch := run.Plan.Steps[0].Batch
	processed := 0
	refs := slices.Clone(run.Targets)
	for _, extraction := range run.Extractions {
		refs = append(refs, extraction.Type+"/"+extraction.ID)
	}
	slices.Sort(refs)
	refs = slices.Compact(refs)
	for _, ref := range refs {
		if ref <= run.Cursor {
			continue
		}
		typ, id, _ := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if et == nil || et.rows.get(id) == nil {
			return run, migrationRefusal(fmt.Errorf("migration source record disappeared"))
		}
		current := et.rows.get(id)
		fields := []string{}
		for _, field := range et.info.Fields {
			fields = append(fields, field.Name)
		}
		request := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: fields}
		grant := d.tenant.authorizeFrom(d.records, c, request, d.at, false)
		if err := authorizationRefusal(grant); err != nil {
			return run, err
		}
		d.RecordAuthorization(c, request, grant)
		if !slices.Contains(run.Targets, ref) {
			question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "archive", Type: typ, Record: id, Action: typ + ".archive"}
			grant := d.tenant.authorizeFrom(d.records, c, question, d.at, false)
			if err := authorizationRefusal(grant); err != nil {
				return run, err
			}
			d.RecordAuthorization(c, question, grant)
			archived := copyOf(et.info.Go, current.value.Interface())
			record := recordOf(archived)
			record.Archived = true
			if err := d.Put(c, r, archived.Interface()); err != nil {
				return run, err
			}
		}
		if source := projectionFor(current.projection, run.Plan.From); source != nil {
			if source.Shape != run.SourceShapes[typ] {
				return run, migrationRefusal(fmt.Errorf("migration source projection changed"))
			}
			var values map[string]json.RawMessage
			if err := json.Unmarshal(source.Value, &values); err != nil {
				return run, migrationRefusal(err)
			}
			if err := d.putMigrationImage(c, r, et, id, values, run.SourceShapes[typ]); err != nil {
				return run, err
			}
			updated := *et.rows.get(id)
			remaining := updated.projection.Prior
			updated.projection = nil
			if len(remaining) > 0 {
				retained := remaining[0]
				retained.Prior = slices.Clone(remaining[1:])
				updated.projection = &retained
			}
			et.rows.set(id, &updated)
		}
		run.Cursor = ref
		processed++
		if processed == batch {
			break
		}
	}
	if !slices.ContainsFunc(refs, func(ref string) bool { return ref > run.Cursor }) {
		run.State, run.Cursor = "cancelled", ""
	}
	return run, nil
}

// This gate consumes durable progress and exact source/target shapes. It
// neither evaluates transformations nor treats a client flag as permission.
func (t *Tenant) migratedStorageLocked(old, next platform.EntityInfo) bool {
	progress := t.records.types[platform.MigrationRunType]
	if progress == nil {
		return false
	}
	source, sourceErr := canonicalDigest(old.Fields)
	target, targetErr := canonicalDigest(next.Fields)
	if sourceErr != nil || targetErr != nil {
		return false
	}
	for _, value := range progress.rows.all() {
		run := value.value.Interface().(platform.MigrationRun)
		if run.Plan.From == t.releases.active && run.State == "complete" && len(run.Failures) == 0 && run.SourceShapes[old.Type] == source && run.TargetShapes[old.Type] == target {
			return true
		}
	}
	return false
}

func (t *Tenant) validateMigratedRecordsLocked(trial *Tenant, member platform.Member, at time.Time) error {
	checks := t.newStagedDecision()
	checks.records = trial.records.forkRecords()
	checks.at = at
	checks.constraintWrites = map[string]constraintWrite{}
	for _, et := range checks.records.sortedTypes() {
		old := t.records.types[et.info.Type]
		if old == nil || !t.migratedStorageLocked(old.info, et.info) {
			continue
		}
		c := platform.NewCaller(checks, member, et.info.App, false, false)
		fields := []string{}
		for _, field := range et.info.Fields {
			fields = append(fields, field.Name)
		}
		for id, current := range et.rows.all() {
			if recordOf(current.value).Archived {
				continue
			}
			request := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.info.Type, Record: id, Fields: fields}
			if err := authorizationRefusal(t.authorizeFrom(checks.records, c, request, at, false)); err != nil {
				return err
			}
			if err := checks.Check(c, current.value.Interface()); err != nil {
				return fmt.Errorf("migrated %s/%s does not satisfy its target declaration: %w", et.info.Type, id, err)
			}
			checks.constraintWrites[et.info.Type+"/"+id] = constraintWrite{caller: c}
		}
	}
	if err := checks.checkFinalConstraints(); err != nil {
		return fmt.Errorf("migrated target constraints: %w", err)
	}
	return nil
}

func (d *stagedDecision) putMigrationImage(c platform.Caller, r *pb.ChangeRecord, et *entityType, id string, values map[string]json.RawMessage, shape string) *kernel.Error {
	prior := et.rows.get(id)
	before, err := prior.image()
	if err != nil {
		return migrationRefusal(err)
	}
	source := copyOf(et.info.Go, prior.value.Interface())
	if err := d.Put(c, r, source.Interface()); err != nil {
		return err
	}
	updated := *et.rows.get(id)
	updated.history = copyHistory(updated.history)
	current := &updated
	projection, err := json.Marshal(current.value.Interface())
	if err != nil {
		return migrationRefusal(err)
	}
	var header map[string]json.RawMessage
	_ = json.Unmarshal(projection, &header)
	for _, name := range []string{"id", "revision", "created", "changed", "archived", "versionPins"} {
		if value, found := header[name]; found {
			values[name] = value
		} else {
			delete(values, name)
		}
	}
	actual, err := json.Marshal(values)
	if err != nil {
		return migrationRefusal(err)
	}
	var old map[string]json.RawMessage
	_ = json.Unmarshal(before, &old)
	last := &current.history[len(current.history)-1]
	last.Fields = nil
	keys := []string{}
	for name := range values {
		keys = append(keys, name)
	}
	for name := range old {
		if _, found := values[name]; !found {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	for _, name := range keys {
		if slices.Contains([]string{"id", "revision", "created", "changed", "archived", "versionPins"}, name) {
			continue
		}
		if string(old[name]) != string(values[name]) {
			last.Fields = append(last.Fields, FieldChange{Field: name, Before: old[name], After: values[name]})
		}
	}
	current.original = actual
	retained := current.projection
	current.projection = &recordProjection{From: d.tenant.releases.active, Shape: shape, Value: projection}
	if retained != nil {
		if retained.From != current.projection.From {
			previous := *retained
			previous.Prior = nil
			current.projection.Prior = append(current.projection.Prior, previous)
		}
		current.projection.Prior = append(current.projection.Prior, retained.Prior...)
	}
	et.rows.set(id, current)
	return nil
}
