package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

const observationResponseBytes = 1 << 20

type ObservationInfo struct {
	Ref            platform.AssetRef              `json:"ref"`
	Version        string                         `json:"version"`
	Definition     platform.ObservationDefinition `json:"definition"`
	SupportsLatest bool                           `json:"supportsLatest,omitempty"`
}

type ObservationRead struct {
	Record string    `json:"record"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	Mode   string    `json:"mode"`
	Limit  int       `json:"limit,omitempty"`
}

type ObservationEvaluate struct {
	Record string    `json:"record"`
	Until  time.Time `json:"until"`
	Key    string    `json:"key"`
}

type ObservationEvaluation struct {
	Observation *platform.ObservationResult `json:"observation,omitempty"`
	Matched     bool                        `json:"matched"`
	Receipt     *SubmissionAnswer           `json:"receipt,omitempty"`
	Reused      bool                        `json:"reused,omitempty"`
}

// An observation plan exists only while an original read is outside the
// tenant lock. It is never a durable input, row image or raw-data cache.
type observationReadPlan struct {
	info       ObservationInfo
	entity     platform.EntityInfo
	record     json.RawMessage
	connection build.Connection
	header     string
	device     string
	stamp      string
	sources    []string
	retained   bool
}

func (t *Tenant) Observations(m platform.Member) ([]ObservationInfo, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if refusal := t.admits(m); refusal != nil {
		return nil, refusal
	}
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return nil, observationUnavailable(err)
	}
	connections, _, refusal := platform.Find[build.Connection](t.automation(build.ID, false), platform.Query{Limit: 500})
	if refusal != nil {
		return nil, refusal
	}
	live := map[string]bool{}
	if owner, ok := t.app(build.ID).(IntegrationDefinitions); ok {
		for _, connection := range connections {
			if connection.State == "ready" && !connection.Archived {
				if fixed, err := owner.ExecutionConnection(connection); err == nil && fixed.Protocol != nil {
					live[fixed.Name] = true
				}
			}
		}
	}
	out := []ObservationInfo{}
	for _, definition := range definitions {
		if definition.Observation != nil {
			out = append(out, ObservationInfo{Ref: definition.Ref, Version: definition.Version, Definition: *definition.Observation, SupportsLatest: definition.Observation.Connection.App == build.ID && live[definition.Observation.Connection.Name]})
		}
	}
	return out, nil
}

func observationUnavailable(err error) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation read is unavailable: {reason}", err.Error())
}

func (t *Tenant) prepareObservation(m platform.Member, ref platform.AssetRef, record string, at time.Time, evaluate bool) (observationReadPlan, *kernel.Error) {
	var plan observationReadPlan
	m = t.authorizationMember(m)
	if refusal := t.admits(m); refusal != nil {
		return plan, refusal
	}
	if ref.Kind != platform.AssetObservation || ref.Check() != nil || record == "" || len(record) > 200 || strings.ContainsAny(record, "/\x00\r\n") {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a declared observation and original record")
	}
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return plan, observationUnavailable(err)
	}
	for _, definition := range definitions {
		if definition.Ref == ref && definition.Observation != nil {
			if definition.OwnerRecord != "" && !t.observationSourceActive(definition.OwnerRecord) {
				continue
			}
			plan.info = ObservationInfo{Ref: ref, Version: definition.Version, Definition: *definition.Observation}
			break
		}
	}
	if plan.info.Version == "" {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation is unavailable to this member")
	}
	definition := plan.info.Definition
	entity, found := t.entity(definition.Object.Name)
	if !found || entity.App != definition.Object.App {
		return plan, observationUnavailable(fmt.Errorf("observation object is unavailable"))
	}
	plan.entity = entity
	caller := platform.NewCaller(runtime{t}, m, ref.App, false, false)
	permission := ref.Name
	kind := "read"
	if evaluate {
		permission += ".evaluate"
		kind = "action"
	}
	decision := t.authorizeNative(caller, permission, kind, definition.Roles, true, authorizationDetails{At: at})
	if refusal := authorizationRefusal(decision); refusal != nil {
		return plan, refusal
	}
	fields := []string{definition.DeviceField}
	if evaluate {
		if definition.Decision == nil {
			return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This observation has no declared decision")
		}
		fields = append(fields, observationDecisionFields(*definition.Decision)...)
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)
	decision = t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: entity.Type, Record: record, Fields: fields}, at, false)
	if refusal := authorizationRefusal(decision); refusal != nil {
		return plan, refusal
	}
	if len(decision.Withheld) > 0 {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation fields are outside this member's permissions")
	}
	plan.record, _ = t.readRecordFrom(t.records, caller, entity.Type, record, at)
	if len(plan.record) == 0 {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation record is unavailable to this member")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(plan.record, &values) != nil || json.Unmarshal(values[definition.DeviceField], &plan.device) != nil || strings.TrimSpace(plan.device) == "" || len(plan.device) > 256 {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The original record needs a readable observation device identifier")
	}
	connections, _, refusal := platform.Find[build.Connection](t.automation(build.ID, false), platform.Query{Limit: 500})
	if refusal != nil {
		return plan, refusal
	}
	for _, connection := range connections {
		if connection.Name == definition.Connection.Name && !connection.Archived {
			if plan.connection.ID != "" {
				return plan, observationUnavailable(fmt.Errorf("observation connection is ambiguous"))
			}
			plan.connection = connection
		}
	}
	if plan.connection.ID == "" || plan.connection.State != "ready" {
		return plan, observationUnavailable(fmt.Errorf("observation connection is not ready"))
	}
	owner, ok := t.app(definition.Connection.App).(IntegrationDefinitions)
	if !ok {
		return plan, observationUnavailable(fmt.Errorf("observation connection has no original owner"))
	}
	plan.connection, err = owner.ExecutionConnection(plan.connection)
	if err != nil {
		return plan, observationUnavailable(err)
	}
	if plan.connection.Kind != "http" {
		return plan, observationUnavailable(fmt.Errorf("observation uses an HTTP storage adapter"))
	}
	// Connection markings retain the existing integration restriction; source
	// roles never downgrade confidential input for an ordinary business user.
	if build.Guarded(plan.connection.Marking) {
		marking := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: build.ConnectionType, Record: plan.connection.ID}, at, false)
		if refusal := authorizationRefusal(marking); refusal != nil {
			return plan, refusal
		}
	}
	if plan.connection.Secret != "" {
		credential, found := t.secret(plan.connection.Secret)
		if !found || len(bytes.TrimSpace(credential)) == 0 {
			return plan, observationUnavailable(fmt.Errorf("observation credential is unavailable"))
		}
		plan.header = strings.TrimSpace(string(credential))
	}
	plan.sources = []string{entity.Type + "/" + record}
	for _, field := range fields {
		plan.sources = append(plan.sources, entity.Type+"/"+record+"#"+field)
	}
	var actionVersion *platform.SchemaVersion
	if evaluate {
		owner := t.app(definition.Decision.Action.App)
		if owner == nil {
			return plan, observationUnavailable(fmt.Errorf("observation decision owner is unavailable"))
		}
		action, exists := owner.Manifest().Actions.Action(definition.Decision.Action.Name)
		if !exists {
			return plan, observationUnavailable(fmt.Errorf("observation decision action is unavailable"))
		}
		version, err := t.actionSchemaVersion(owner, action)
		if err != nil {
			return plan, observationUnavailable(err)
		}
		actionVersion = &version
	}
	plan.stamp, err = canonicalDigest([]any{plan.info, plan.entity, plan.record, plan.connection, m, plan.header, actionVersion})
	if err != nil {
		return plan, observationUnavailable(err)
	}
	return plan, nil
}

func (t *Tenant) observationSourceActive(record string) bool {
	typ, id, valid := strings.Cut(record, "/")
	if !valid {
		return false
	}
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	owner := t.records.types[typ]
	if owner == nil {
		return false
	}
	row := owner.rows.get(id)
	return row != nil && !recordOf(row.value).Archived && businessField(owner.info, row.value, "state") == "published"
}

func observationDecisionFields(decision platform.ObservationDecision) []string {
	var fields []string
	var visit func(platform.Expr)
	visit = func(expr platform.Expr) {
		if name, ok := strings.CutPrefix(expr.Ref, "record."); ok && name != "id" {
			fields = append(fields, strings.SplitN(name, ".", 2)[0])
		}
		for _, arg := range expr.Args {
			visit(arg)
		}
		if expr.In != nil {
			visit(*expr.In)
		}
	}
	visit(decision.When)
	for _, expr := range decision.Inputs {
		visit(expr)
	}
	return fields
}

func checkObservationRead(q *ObservationRead, now time.Time) error {
	if q.Mode == "latest" {
		if !q.Since.IsZero() || !q.Until.IsZero() || q.Limit != 0 {
			return fmt.Errorf("A current observation does not request a time window or point limit")
		}
		return nil
	}
	if !slices.Contains([]string{"summary", "points"}, q.Mode) || q.Since.IsZero() || q.Until.IsZero() || !q.Since.Before(q.Until) || q.Until.Sub(q.Since) > time.Duration(platform.ObservationMaxWindowSeconds)*time.Second || q.Until.After(now.Add(time.Minute)) {
		return fmt.Errorf("choose a bounded observation time window and summary or points")
	}
	if q.Mode == "summary" && q.Limit != 0 {
		return fmt.Errorf("summary does not request raw points")
	}
	if q.Mode == "points" && q.Limit == 0 {
		q.Limit = 1000
	}
	if q.Limit < 0 || q.Limit > 5000 {
		return fmt.Errorf("observation points accept at most 5000 values")
	}
	return nil
}

func (t *Tenant) fetchObservation(ctx context.Context, plan observationReadPlan, q ObservationRead) (platform.ObservationResult, error) {
	result := platform.ObservationResult{Ref: plan.info.Ref, Version: plan.info.Version, Object: plan.info.Definition.Object, Record: q.Record, Device: plan.device, Signal: plan.info.Definition.Signal, Sources: plan.sources}
	if q.Mode == "latest" && plan.connection.Protocol == nil {
		return result, fmt.Errorf("current device reads require a protocol connection")
	}
	if err := t.configureProtocol(ctx, plan.connection, plan.retained); err != nil {
		return result, err
	}
	body, err := json.Marshal(struct {
		Device string    `json:"device"`
		Signal string    `json:"signal"`
		Since  time.Time `json:"since"`
		Until  time.Time `json:"until"`
		Mode   string    `json:"mode"`
		Limit  int       `json:"limit,omitempty"`
	}{plan.device, plan.info.Definition.Signal, q.Since.UTC(), q.Until.UTC(), q.Mode, q.Limit})
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.connection.Resolve(plan.info.Definition.Path), bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("observation adapter address is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if plan.header != "" {
		request.Header.Set("Authorization", plan.header)
	}
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	response, err := send(request, plan.connection.AllowPrivate)
	if err != nil {
		return result, fmt.Errorf("observation storage did not answer")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("observation storage answered %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, observationResponseBytes+1))
	if err != nil || len(raw) > observationResponseBytes {
		return result, fmt.Errorf("observation answer exceeds its read budget")
	}
	var answer struct {
		Window   platform.ObservationWindow  `json:"window"`
		Points   []platform.ObservationPoint `json:"points,omitempty"`
		Coverage *platform.DriverCoverage    `json:"coverage,omitempty"`
		Samples  []platform.DriverSample     `json:"samples,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		return result, fmt.Errorf("observation storage returned an invalid shape")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("observation storage returned trailing data")
	}
	if q.Mode == "latest" {
		if len(answer.Samples) != 1 || answer.Window != (platform.ObservationWindow{}) || len(answer.Points) != 0 || answer.Coverage != nil {
			return result, fmt.Errorf("current observation needs exactly its declared point without a window")
		}
		sample := answer.Samples[0]
		if err := platform.CheckObservationLatest(sample, plan.device, plan.info.Definition.Signal, plan.info.Definition.Unit); err != nil {
			return result, err
		}
		if sample.Source.Connection != plan.connection.ID || sample.Source.Revision != plan.connection.Protocol.Revision() || sample.Source.Generation == "" {
			return result, fmt.Errorf("current observation has another connection or configuration revision")
		}
		result.Latest = &sample
		return result, nil
	}
	if len(answer.Samples) != 0 {
		return result, fmt.Errorf("current device readings cannot replace a historical observation window")
	}
	if err := platform.CheckObservationWindowRange(answer.Window, q.Since, q.Until, plan.info.Definition.Unit); err != nil {
		return result, err
	}
	if plan.connection.Protocol != nil || plan.info.Definition.RequireComplete {
		if answer.Coverage == nil {
			return result, fmt.Errorf("observation storage did not declare window coverage")
		}
		if err := platform.CheckObservationCoverage(*answer.Coverage, answer.Window); err != nil {
			return result, err
		}
	}
	if q.Mode == "summary" && len(answer.Points) > 0 || q.Mode == "points" && len(answer.Points) > q.Limit {
		return result, fmt.Errorf("observation storage exceeded the requested point shape")
	}
	if q.Mode == "points" {
		if err := platform.CheckObservationPoints(answer.Points, answer.Window, q.Limit); err != nil {
			return result, err
		}
	}
	result.Window, result.Points = answer.Window, answer.Points
	result.Coverage = answer.Coverage
	return result, nil
}

func (t *Tenant) ReadObservation(ctx context.Context, m platform.Member, ref platform.AssetRef, q ObservationRead, now time.Time) (platform.ObservationResult, *kernel.Error) {
	if err := checkObservationRead(&q, now); err != nil {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	t.mu.Lock()
	plan, refusal := t.prepareObservation(m, ref, q.Record, now, false)
	t.mu.Unlock()
	if refusal != nil {
		return platform.ObservationResult{}, refusal
	}
	if q.Until.Sub(q.Since) > time.Duration(plan.info.Definition.ExpectedRetentionSeconds)*time.Second {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation time window exceeds the declared retention")
	}
	result, err := t.fetchObservation(ctx, plan, q)
	if err != nil {
		return platform.ObservationResult{}, observationUnavailable(err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	current, refusal := t.prepareObservation(m, ref, q.Record, now, false)
	if refusal != nil {
		return platform.ObservationResult{}, refusal
	}
	if current.stamp != plan.stamp {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation source or permissions changed while reading")
	}
	return result, nil
}
