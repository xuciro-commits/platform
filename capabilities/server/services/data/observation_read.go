package data

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
	"platformserver/platform"
)

const observationResponseBytes = 1 << 20

type ObservationHost struct {
	ObservationMember       func(platform.Member) (platform.Member, *kernel.Error)
	ObservationDefinitions  func(platform.Member) ([]platform.Definition, error)
	ObservationSourceActive func(record string) bool
	Entity                  func(typ string) (platform.EntityInfo, bool)
	AuthorizeObservation    func(m platform.Member, app, permission, kind string, roles []string, at time.Time) *kernel.Error
	AuthorizeRead           func(m platform.Member, app string, q platform.AuthzRequest, at time.Time) (withheld bool, refusal *kernel.Error)
	ReadRecord              func(m platform.Member, app, typ, id string, at time.Time) (json.RawMessage, *kernel.Error)
	ObservationConnections  func() ([]Connection, int, *kernel.Error)
	DataOwner               func() *Owner
	CredentialHeader        func(secret, scope, address string) ([]byte, bool)
	ActionSchemaVersion     func(platform.AssetRef) (platform.SchemaVersion, error)
	Send                    func(*http.Request, bool) (*http.Response, error)
	Rules                   func() platform.RuleEngine
}

type Observations struct{ host ObservationHost }

func NewObservations(host ObservationHost) Observations { return Observations{host: host} }

type ObservationInfo struct {
	Ref        platform.AssetRef              `json:"ref"`
	Version    string                         `json:"version"`
	Definition platform.ObservationDefinition `json:"definition"`
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

type ObservationPlan struct {
	Info       ObservationInfo
	Entity     platform.EntityInfo
	Record     json.RawMessage
	Connection Connection
	Header     string
	Device     string
	Stamp      string
	Sources    []string
}

func ObservationUnavailable(err error) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation read is unavailable: {reason}", err.Error())
}

func (o Observations) Prepare(m platform.Member, ref platform.AssetRef, record string, at time.Time, evaluate bool) (ObservationPlan, *kernel.Error) {
	var plan ObservationPlan
	m, refusal := o.host.ObservationMember(m)
	if refusal != nil {
		return plan, refusal
	}
	if ref.Kind != platform.AssetObservation || ref.Check() != nil || record == "" || len(record) > 200 || strings.ContainsAny(record, "/\x00\r\n") {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a declared observation and original record")
	}
	definitions, err := o.host.ObservationDefinitions(m)
	if err != nil {
		return plan, ObservationUnavailable(err)
	}
	for _, definition := range definitions {
		if definition.Ref == ref && definition.Observation != nil {
			if definition.OwnerRecord != "" && !o.host.ObservationSourceActive(definition.OwnerRecord) {
				continue
			}
			plan.Info = ObservationInfo{Ref: ref, Version: definition.Version, Definition: *definition.Observation}
			break
		}
	}
	if plan.Info.Version == "" {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation is unavailable to this member")
	}
	definition := plan.Info.Definition
	entity, found := o.host.Entity(definition.Object.Name)
	if !found || entity.App != definition.Object.App {
		return plan, ObservationUnavailable(fmt.Errorf("observation object is unavailable"))
	}
	plan.Entity = entity
	permission := ref.Name
	kind := "read"
	if evaluate {
		permission += ".evaluate"
		kind = "action"
	}
	if refusal := o.host.AuthorizeObservation(m, ref.App, permission, kind, definition.Roles, at); refusal != nil {
		return plan, refusal
	}
	fields := []string{definition.DeviceField}
	if evaluate {
		if definition.Decision == nil {
			return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This observation has no declared decision")
		}
		fields = append(fields, ObservationDecisionFields(*definition.Decision)...)
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)
	withheld, refusal := o.host.AuthorizeRead(m, ref.App, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "read", Type: entity.Type, Record: record, Fields: fields}, at)
	if refusal != nil {
		return plan, refusal
	}
	if withheld {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation fields are outside this member's permissions")
	}
	plan.Record, _ = o.host.ReadRecord(m, ref.App, entity.Type, record, at)
	if len(plan.Record) == 0 {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation record is unavailable to this member")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(plan.Record, &values) != nil || json.Unmarshal(values[definition.DeviceField], &plan.Device) != nil || strings.TrimSpace(plan.Device) == "" || len(plan.Device) > 256 {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The original record needs a readable observation device identifier")
	}
	connections, _, refusal := o.host.ObservationConnections()
	if refusal != nil {
		return plan, refusal
	}
	for _, connection := range connections {
		if connection.Name == definition.Connection.Name && !connection.Archived {
			if plan.Connection.ID != "" {
				return plan, ObservationUnavailable(fmt.Errorf("observation connection is ambiguous"))
			}
			plan.Connection = connection
		}
	}
	if plan.Connection.ID == "" || plan.Connection.State != "ready" {
		return plan, ObservationUnavailable(fmt.Errorf("observation connection is not ready"))
	}
	owner := o.host.DataOwner()
	if owner == nil {
		return plan, ObservationUnavailable(fmt.Errorf("observation connection has no original owner"))
	}
	plan.Connection, err = owner.ExecutionConnection(plan.Connection)
	if err != nil {
		return plan, ObservationUnavailable(err)
	}
	if plan.Connection.Kind != "http" {
		return plan, ObservationUnavailable(fmt.Errorf("observation uses an HTTP storage adapter"))
	}
	if !(platform.MarkingSet{Level: plan.Connection.Marking, Compartments: plan.Connection.Compartments}).Cleared(m) {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The data carries a marking the caller is not cleared for")
	}
	if plan.Connection.Secret != "" {
		credential, found := o.host.CredentialHeader(plan.Connection.Secret, "connection/"+plan.Connection.ID, plan.Connection.Address)
		if !found || len(bytes.TrimSpace(credential)) == 0 {
			return plan, ObservationUnavailable(fmt.Errorf("observation credential is unavailable"))
		}
		plan.Header = strings.TrimSpace(string(credential))
	}
	plan.Sources = []string{entity.Type + "/" + record}
	for _, field := range fields {
		plan.Sources = append(plan.Sources, entity.Type+"/"+record+"#"+field)
	}
	var actionVersion *platform.SchemaVersion
	if evaluate {
		version, err := o.host.ActionSchemaVersion(definition.Decision.Action)
		if err != nil {
			return plan, ObservationUnavailable(err)
		}
		actionVersion = &version
	}
	plan.Stamp, err = platform.CanonicalDigest([]any{plan.Info, plan.Entity, plan.Record, plan.Connection, m, plan.Header, actionVersion})
	if err != nil {
		return plan, ObservationUnavailable(err)
	}
	return plan, nil
}

func ObservationDecisionFields(decision platform.ObservationDecision) []string {
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

func CheckObservationRead(q *ObservationRead, now time.Time) error {
	if !slices.Contains([]string{"summary", "points", "intervals"}, q.Mode) || q.Since.IsZero() || q.Until.IsZero() || !q.Since.Before(q.Until) || q.Until.Sub(q.Since) > time.Duration(platform.ObservationMaxWindowSeconds)*time.Second || q.Until.After(now.Add(time.Minute)) {
		return fmt.Errorf("choose a bounded observation time window and summary or points")
	}
	if q.Mode == "summary" && q.Limit != 0 {
		return fmt.Errorf("summary does not request raw points")
	}
	if (q.Mode == "points" || q.Mode == "intervals") && q.Limit == 0 {
		q.Limit = 1000
	}
	if q.Limit < 0 || q.Limit > 5000 {
		return fmt.Errorf("observation points accept at most 5000 values")
	}
	return nil
}

func (o Observations) Fetch(ctx context.Context, plan ObservationPlan, q ObservationRead, now time.Time) (platform.ObservationResult, error) {
	result := platform.ObservationResult{Ref: plan.Info.Ref, Version: plan.Info.Version, Object: plan.Info.Definition.Object, Record: q.Record, Device: plan.Device, Signal: plan.Info.Definition.Signal, Sources: plan.Sources}
	if q.Mode == "intervals" && plan.Info.Definition.Intervals == nil {
		return result, fmt.Errorf("the observation has no declared state interval analysis")
	}
	body, err := json.Marshal(struct {
		Device string    `json:"device"`
		Signal string    `json:"signal"`
		Since  time.Time `json:"since"`
		Until  time.Time `json:"until"`
		Mode   string    `json:"mode"`
		Limit  int       `json:"limit,omitempty"`
	}{plan.Device, plan.Info.Definition.Signal, q.Since.UTC(), q.Until.UTC(), q.Mode, q.Limit})
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.Connection.Resolve(plan.Info.Definition.Path), bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("observation adapter address is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if plan.Header != "" {
		request.Header.Set("Authorization", plan.Header)
	}
	response, err := o.host.Send(request, plan.Connection.AllowPrivate)
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
		Window    platform.ObservationWindow         `json:"window"`
		Points    []platform.ObservationPoint        `json:"points,omitempty"`
		Coverage  *platform.ObservationCoverage      `json:"coverage,omitempty"`
		Intervals *platform.ObservationIntervalInput `json:"intervals,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		return result, fmt.Errorf("observation storage returned an invalid shape")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("observation storage returned trailing data")
	}
	if q.Mode == "intervals" {
		if answer.Intervals == nil || answer.Window != (platform.ObservationWindow{}) || len(answer.Points) > 0 || answer.Coverage != nil || !answer.Intervals.Since.Equal(q.Since) || !answer.Intervals.Until.Equal(q.Until) || len(answer.Intervals.Points) > q.Limit {
			return result, fmt.Errorf("state observations must match their requested window and bounded point shape")
		}
		analysis, err := platform.AnalyzeObservationIntervals(*plan.Info.Definition.Intervals, *answer.Intervals, now, o.host.Rules())
		if err != nil {
			return result, err
		}
		result.Intervals = &analysis
		return result, nil
	}
	if answer.Intervals != nil {
		return result, fmt.Errorf("state intervals cannot replace a numeric observation window")
	}
	if err := platform.CheckObservationWindowRange(answer.Window, q.Since, q.Until, plan.Info.Definition.Unit); err != nil {
		return result, err
	}
	if plan.Info.Definition.RequireComplete {
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
