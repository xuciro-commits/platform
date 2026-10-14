package platformserver

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ApplicationVersion struct {
	Version string    `json:"version"`
	Release string    `json:"release,omitempty"`
	At      time.Time `json:"at,omitzero"`
	Active  bool      `json:"active"`
}
type ApplicationData struct {
	Ref      platform.AssetRef `json:"ref"`
	Records  int               `json:"records"`
	Expected int64             `json:"expected,omitempty"`
}
type ApplicationWarning struct {
	Kind     string `json:"kind"`
	Resource string `json:"resource,omitempty"`
	Actual   int64  `json:"actual"`
	Expected int64  `json:"expected"`
}
type ApplicationRecovery struct {
	State          string `json:"state"`
	Predecessors   string `json:"predecessors"`
	Quarantined    bool   `json:"quarantined"`
	Scope          string `json:"scope"`
	PendingEffects int    `json:"pendingEffects"`
	FailedRuns     int    `json:"failedRuns"`
}
type ApplicationMonitoring struct {
	Accepted           int                      `json:"accepted"`
	Refused            int                      `json:"refused"`
	ConstraintFailures int                      `json:"constraintFailures"`
	RefusalRate        float64                  `json:"refusalRate"`
	Runs               ApplicationRunSummary    `json:"runs"`
	MemberQuota        *platform.AIQuota        `json:"memberQuota,omitempty"`
	AgentBudgets       []ApplicationAgentBudget `json:"agentBudgets"`
}
type ApplicationAgentBudget struct {
	Resource platform.AssetBinding `json:"resource"`
	Budget   platform.Budget       `json:"budget"`
}
type ApplicationRuntime struct {
	Artifact    platform.ApplicationArtifact `json:"artifact"`
	State       string                       `json:"state"`
	Scope       string                       `json:"scope"`
	Versions    []ApplicationVersion         `json:"versions"`
	Previous    string                       `json:"previous,omitempty"`
	Data        []ApplicationData            `json:"data"`
	Runs        int                          `json:"runs"`
	Warnings    []ApplicationWarning         `json:"warnings"`
	Recovery    ApplicationRecovery          `json:"recovery"`
	Monitoring  ApplicationMonitoring        `json:"monitoring"`
	Maintenance ApplicationMaintenance       `json:"maintenance"`
}

type ApplicationMaintenance struct {
	Migrations []ApplicationMigration  `json:"migrations"`
	Retired    []ApplicationRetirement `json:"retired"`
	Extensions []ApplicationExtension  `json:"extensions"`
	Retention  []ApplicationRetention  `json:"retention"`
}
type ApplicationMigration struct {
	ID    string `json:"id"`
	State string `json:"state"`
	From  string `json:"from"`
	To    string `json:"to"`
}
type ApplicationRetirement struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Migration string `json:"migration"`
}
type ApplicationExtension struct {
	Resource platform.AssetBinding  `json:"resource"`
	Base     string                 `json:"base"`
	Source   *platform.AssetBinding `json:"source,omitempty"`
}
type ApplicationRetention struct {
	Category string `json:"category"`
	Duration string `json:"duration"`
	State    string `json:"state"`
}

// Original migration records and installed extension declarations remain the
// authorities. A declared retention duration does not invent a deletion job.
func (t *Tenant) applicationMaintenanceLocked(m platform.Member, artifact platform.ApplicationArtifact, now time.Time) (ApplicationMaintenance, *kernel.Error) {
	out := ApplicationMaintenance{Migrations: []ApplicationMigration{}, Retired: []ApplicationRetirement{}, Extensions: []ApplicationExtension{}, Retention: []ApplicationRetention{}}
	models := map[string]bool{}
	for _, binding := range artifact.Model {
		if binding.Ref.Kind == platform.AssetObject {
			models[binding.Ref.Name] = true
		}
	}
	for _, binding := range artifact.Model {
		if binding.Ref.Kind != platform.AssetObject {
			continue
		}
		info, ok := t.entity(binding.Ref.Name)
		if ok && info.Extends != "" {
			extension := ApplicationExtension{Resource: binding, Base: info.Extends}
			if info.ExtensionSource != nil && models[info.ExtensionSource.Base.Ref.Name] {
				source := info.ExtensionSource.Base
				extension.Source = &source
			}
			out.Extensions = append(out.Extensions, extension)
		}
	}
	page, refusal := t.recordSelectionFrom(t.records, m, platform.MigrationRunType, platform.Query{Archived: true}, now)
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return out, refusal
	}
	for _, raw := range page.Records {
		var run platform.MigrationRun
		if json.Unmarshal(platform.Raw(raw), &run) != nil {
			continue
		}
		candidate, err := t.releases.candidate(run.Plan.To)
		if err != nil || !slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == artifact.Ref }) {
			continue
		}
		out.Migrations = append(out.Migrations, ApplicationMigration{ID: run.ID, State: run.State, From: run.Plan.From, To: run.Plan.To})
		for _, step := range run.Plan.Steps {
			for _, typ := range step.Types {
				if !models[typ] {
					continue
				}
				if step.Kind == "retire-action" {
					out.Retired = append(out.Retired, ApplicationRetirement{Type: typ, Name: step.Action, Kind: "action", State: run.State, Migration: run.ID})
				}
				if step.Kind == "retire" {
					for _, field := range step.Fields {
						out.Retired = append(out.Retired, ApplicationRetirement{Type: typ, Name: field.From, Kind: "field", State: run.State, Migration: run.ID})
					}
				}
			}
		}
	}
	if artifact.Profile != nil {
		for category, duration := range artifact.Profile.Retention {
			out.Retention = append(out.Retention, ApplicationRetention{Category: category, Duration: duration, State: "declared"})
		}
		slices.SortFunc(out.Retention, func(a, b ApplicationRetention) int { return strings.Compare(a.Category, b.Category) })
	}
	return out, nil
}

// ApplicationRuntimeOf projects the installed owners and original release
// receipts. It stores no application copy, aggregate or health state.
func (t *Tenant) ApplicationRuntimeOf(m platform.Member, ref platform.AssetRef, now time.Time) (ApplicationRuntime, *kernel.Error) {
	out := ApplicationRuntime{Scope: "authorized", State: "healthy", Versions: []ApplicationVersion{}, Data: []ApplicationData{}, Warnings: []ApplicationWarning{}}
	if ref.Kind != platform.AssetApp || ref.Check() != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a published application")
	}
	t.mu.Lock()
	if err := t.admits(m); err != nil {
		t.mu.Unlock()
		return out, err
	}
	if t.quarantined() {
		c := platform.NewCaller(runtime{t}, m, PlatformApp, false, false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "application recovery", "read", []string{Admin}, true)); refusal != nil {
			t.mu.Unlock()
			return out, refusal
		}
		out.State = "isolated"
		out.Artifact = platform.ApplicationArtifact{Ref: ref, Model: []platform.AssetBinding{}, Rules: []platform.AssetBinding{}, Pages: []platform.AssetBinding{}, Flows: []platform.AssetBinding{}, AI: []platform.AssetBinding{}, Integrations: []platform.AssetBinding{}, Roles: []platform.ApplicationRole{}, Depends: []platform.AssetBinding{}, Environment: []platform.ApplicationBinding{}}
		out.Recovery = ApplicationRecovery{State: "isolated", Predecessors: "unverified", Quarantined: true, Scope: "tenant"}
		t.mu.Unlock()
		return out, nil
	}
	visible := map[platform.AssetRef]bool{}
	fields := map[platform.AssetRef]map[string]bool{}
	for _, definition := range t.Definitions(m) {
		visible[definition.Ref] = true
		if definition.Application != nil {
			for _, resource := range definition.Application.Resources {
				if resource.Kind == platform.AssetFlow {
					visible[resource] = true
				}
			}
		}
		if definition.Entity != nil {
			fields[definition.Ref] = map[string]bool{}
			for _, field := range definition.Entity.Fields {
				fields[definition.Ref][field.Name] = true
			}
		}
	}
	if !visible[ref] {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Application is unavailable to this member")
	}
	assets, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	candidate, err := t.candidateWithBindings([]platform.AssetRef{ref}, assets, []platform.AssetRef{ref})
	if err != nil {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	artifact, err := platform.ArtifactOfApplication(ref, candidate.Assets)
	if err != nil {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	out.Artifact = artifact
	out.Monitoring.AgentBudgets = []ApplicationAgentBudget{}
	if t.ai != nil && len(artifact.AI) > 0 {
		quota := t.ai.Quota(m, t.limits(), now)
		out.Monitoring.MemberQuota = &quota
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetAgent || !visible[asset.Ref] {
			continue
		}
		var descriptor platform.AgentReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
			t.mu.Unlock()
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
		}
		out.Monitoring.AgentBudgets = append(out.Monitoring.AgentBudgets, ApplicationAgentBudget{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Budget: descriptor.Budget})
	}
	model := map[string]bool{}
	for _, binding := range artifact.Model {
		if visible[binding.Ref] {
			model[binding.Ref.Name] = true
		}
	}
	accepted := map[string]bool{}
	owners := map[string]bool{}
	for _, binding := range artifact.Model {
		owner := t.app(binding.Ref.App)
		if owner == nil || !visible[binding.Ref] || binding.Ref.Kind != platform.AssetObject || owners[binding.Ref.App] {
			continue
		}
		owners[binding.Ref.App] = true
		for _, receipt := range owner.AcceptedLedger().RecordsFor(t.ID) {
			sub := receipt.GetSubmission()
			if sub == nil || !model[sub.GetTarget().GetType()] || sub.GetPrincipalId() != m.ID && !t.Readable(m, target(sub), now) {
				continue
			}
			accepted[binding.Ref.App+"/"+receipt.GetChangeId()] = true
		}
	}
	out.Monitoring.Accepted = len(accepted)
	for _, refusal := range t.committed.refusals {
		var sub pb.Submission
		if protojson.Unmarshal(refusal.Submission, &sub) != nil || !model[sub.GetTarget().GetType()] || sub.GetPrincipalId() != m.ID && !t.Readable(m, target(&sub), now) {
			continue
		}
		out.Monitoring.Refused++
		if slices.ContainsFunc(refusal.Issues, func(issue platform.FieldIssue) bool { return issue.Constraint != "" }) {
			out.Monitoring.ConstraintFailures++
		}
	}
	if count := out.Monitoring.Accepted + out.Monitoring.Refused; count > 0 {
		out.Monitoring.RefusalRate = float64(out.Monitoring.Refused) / float64(count)
	}
	for key, raw := range t.committed.answers {
		if !strings.HasPrefix(key, "release:") {
			continue
		}
		release, err := decodeAcceptedRelease(raw)
		if err != nil || !release.Active {
			continue
		}
		candidate, err := platform.ReadCandidate(release.CandidateID, release.Bytes)
		if err != nil {
			continue
		}
		if !slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref }) {
			continue
		}
		version, err := platform.ArtifactOfApplication(ref, candidate.Assets)
		if err != nil {
			t.mu.Unlock()
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application version history is unavailable")
		}
		out.Versions = append(out.Versions, ApplicationVersion{Version: version.Version, Release: release.CandidateID, At: release.At, Active: version.Version == artifact.Version})
	}
	slices.SortFunc(out.Versions, func(a, b ApplicationVersion) int {
		if a.At.After(b.At) {
			return -1
		}
		if a.At.Before(b.At) {
			return 1
		}
		return strings.Compare(a.Release, b.Release)
	})
	for _, version := range out.Versions {
		if !version.Active {
			out.Previous = version.Version
			break
		}
	}
	if len(out.Versions) == 0 {
		out.Versions = append(out.Versions, ApplicationVersion{Version: artifact.Version, Active: true})
	}
	// Member projection never exposes dependency identities unavailable through
	// discovery, although the opaque complete content identity stays unchanged.
	project := func(bindings []platform.AssetBinding) []platform.AssetBinding {
		return slices.DeleteFunc(slices.Clone(bindings), func(binding platform.AssetBinding) bool { return !visible[binding.Ref] })
	}
	out.Artifact.Model, out.Artifact.Rules, out.Artifact.Pages = project(artifact.Model), project(artifact.Rules), project(artifact.Pages)
	out.Artifact.Flows, out.Artifact.AI, out.Artifact.Integrations = project(artifact.Flows), project(artifact.AI), project(artifact.Integrations)
	out.Artifact.Depends = project(artifact.Depends)
	out.Artifact.Environment = slices.DeleteFunc(slices.Clone(artifact.Environment), func(binding platform.ApplicationBinding) bool { return !visible[binding.Ref] })
	out.Artifact.Roles = slices.DeleteFunc(slices.Clone(artifact.Roles), func(role platform.ApplicationRole) bool {
		return !visible[role.Resource.Ref] || !slices.Contains(m.RolesIn(role.Resource.Ref.App), "builder") && !slices.Contains(m.RolesIn(role.Resource.Ref.App), role.Role)
	})
	for i := range out.Artifact.Roles {
		role := &out.Artifact.Roles[i]
		role.ReadFields = slices.DeleteFunc(slices.Clone(role.ReadFields), func(name string) bool { return !fields[role.Resource.Ref][name] })
		role.WriteFields = slices.DeleteFunc(slices.Clone(role.WriteFields), func(name string) bool { return !fields[role.Resource.Ref][name] })
	}
	if artifact.Profile != nil {
		profile := *artifact.Profile
		profile.ExpectedRecords = map[string]int64{}
		for _, binding := range out.Artifact.Model {
			if count := artifact.Profile.ExpectedRecords[binding.Ref.Name]; count > 0 {
				profile.ExpectedRecords[binding.Ref.Name] = count
			}
		}
		out.Artifact.Profile = &profile
	}
	maintenance, refusal := t.applicationMaintenanceLocked(m, out.Artifact, now)
	if refusal != nil {
		t.mu.Unlock()
		return out, refusal
	}
	out.Maintenance = maintenance
	t.mu.Unlock()
	for _, binding := range out.Artifact.Model {
		if binding.Ref.Kind != platform.AssetObject {
			continue
		}
		page, refusal := t.Records(m, binding.Ref.Name, platform.Query{Limit: 1}, now)
		if refusal != nil {
			out.State = "degraded"
			continue
		}
		data := ApplicationData{Ref: binding.Ref, Records: page.Total}
		if artifact.Profile != nil {
			data.Expected = artifact.Profile.ExpectedRecords[binding.Ref.Name]
		}
		out.Data = append(out.Data, data)
		if data.Expected > 0 && int64(data.Records) > data.Expected {
			out.Warnings = append(out.Warnings, ApplicationWarning{Kind: "records", Resource: binding.Ref.Name, Actual: int64(data.Records), Expected: data.Expected})
		}
	}
	runs, refusal := t.ApplicationRuns(m, ref, 0, 100, now)
	if refusal != nil {
		return out, refusal
	}
	out.Runs = runs.Total
	if artifact.Profile != nil && artifact.Profile.ExpectedRuns > 0 && int64(out.Runs) > artifact.Profile.ExpectedRuns {
		out.Warnings = append(out.Warnings, ApplicationWarning{Kind: "runs", Actual: int64(out.Runs), Expected: artifact.Profile.ExpectedRuns})
	}
	out.Recovery.FailedRuns, out.Recovery.PendingEffects = runs.Summary.Failed, runs.Summary.PendingEffects
	out.Monitoring.Runs = runs.Summary
	if len(out.Warnings) > 0 || out.Recovery.FailedRuns > 0 {
		out.State = "degraded"
	}
	out.Recovery.State = "ready"
	out.Recovery.Predecessors, out.Recovery.Scope = "admitted", "authorized"
	if out.Recovery.PendingEffects > 0 || out.Recovery.FailedRuns > 0 {
		out.Recovery.State = "attention"
	}
	return out, nil
}

type ApplicationDecisionTrace struct {
	Change     string                  `json:"change"`
	Schema     string                  `json:"schema"`
	At         time.Time               `json:"at"`
	Content    *platform.SchemaVersion `json:"content,omitempty"`
	Versions   []ApplicationVersion    `json:"versions"`
	Definition *DecisionDefinition     `json:"definition,omitempty"`
}
type ApplicationDiagnostic struct {
	Application platform.AssetRef          `json:"application"`
	Type        string                     `json:"type"`
	Record      string                     `json:"record"`
	Decisions   []ApplicationDecisionTrace `json:"decisions"`
	Runs        []ApplicationRun           `json:"runs"`
}

func (t *Tenant) ApplicationRecordDiagnostic(m platform.Member, ref platform.AssetRef, typ, id string, now time.Time) (ApplicationDiagnostic, *kernel.Error) {
	out := ApplicationDiagnostic{Application: ref, Type: typ, Record: id, Decisions: []ApplicationDecisionTrace{}, Runs: []ApplicationRun{}}
	runtime, refusal := t.ApplicationRuntimeOf(m, ref, now)
	if refusal != nil {
		return out, refusal
	}
	if !slices.ContainsFunc(runtime.Artifact.Model, func(binding platform.AssetBinding) bool {
		return binding.Ref.Kind == platform.AssetObject && binding.Ref.Name == typ
	}) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record is outside this application")
	}
	view, refusal := t.RecordOf(m, typ, id, now)
	if refusal != nil {
		return out, refusal
	}
	t.mu.Lock()
	owner := t.records.types[typ].info.App
	ledger := t.app(owner).AcceptedLedger()
	receipts := ledger.RecordsFor(t.ID)
	visible := map[platform.AssetRef]bool{}
	for _, definition := range t.Definitions(m) {
		visible[definition.Ref] = true
	}
	for _, change := range view.History {
		trace := ApplicationDecisionTrace{Change: change.Change, Schema: change.Schema, At: change.At, Versions: []ApplicationVersion{}}
		if definition := t.committed.decisionDefinitions[owner+"/"+change.Change]; definition != nil && definition.Definition.Ref.Name == change.Schema {
			projection := *definition
			projection.Dependencies = slices.DeleteFunc(slices.Clone(definition.Dependencies), func(binding platform.AssetBinding) bool { return !visible[binding.Ref] })
			projection.Applications = slices.DeleteFunc(slices.Clone(definition.Applications), func(binding platform.AssetBinding) bool { return !visible[binding.Ref] })
			trace.Definition, trace.Content = &projection, &projection.Contract
			for _, binding := range projection.Applications {
				if binding.Ref == ref {
					trace.Versions = append(trace.Versions, ApplicationVersion{Version: binding.SourceVersion, Release: projection.Release, At: change.At, Active: binding.SourceVersion == runtime.Artifact.Version})
				}
			}
		}
		for _, receipt := range receipts {
			if receipt.GetChangeId() != change.Change {
				continue
			}
			if binding, exists := t.committed.schemaVersions[owner+"/"+receipt.GetSubmission().GetIdempotencyKey()]; exists && trace.Content == nil {
				trace.Content = &binding
			}
			break
		}
		out.Decisions = append(out.Decisions, trace)
	}
	t.mu.Unlock()
	for skip := 0; ; skip += 100 {
		page, refusal := t.ApplicationRuns(m, ref, skip, 100, now, ApplicationRunFilter{Subject: fmt.Sprintf("%s/%s", typ, id)})
		if refusal != nil {
			return out, refusal
		}
		for _, run := range page.Runs {
			if run.Subject == fmt.Sprintf("%s/%s", typ, id) {
				out.Runs = append(out.Runs, run)
			}
		}
		if skip+len(page.Runs) >= page.Total {
			break
		}
	}
	return out, nil
}
