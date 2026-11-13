package applications

import (
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"slices"
	"strings"
	"time"
)

func (ctx Context) maintenance(artifact platform.ApplicationArtifact) platform.ApplicationMaintenance {
	out := platform.ApplicationMaintenance{Extensions: []platform.ApplicationExtension{}, Retention: []platform.ApplicationRetention{}}
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
		info, ok := ctx.Entity(binding.Ref.Name)
		if ok && info.Extends != "" {
			extension := platform.ApplicationExtension{Resource: binding, Base: info.Extends}
			if info.ExtensionSource != nil && models[info.ExtensionSource.Base.Ref.Name] {
				source := info.ExtensionSource.Base
				extension.Source = &source
			}
			out.Extensions = append(out.Extensions, extension)
		}
	}
	if artifact.Profile != nil {
		for category, duration := range artifact.Profile.Retention {
			out.Retention = append(out.Retention, platform.ApplicationRetention{Category: category, Duration: duration, State: "declared"})
		}
		slices.SortFunc(out.Retention, func(a, b platform.ApplicationRetention) int { return strings.Compare(a.Category, b.Category) })
	}
	return out
}

func (ctx Context) Runtime(m platform.Member, ref platform.AssetRef, now time.Time) (platform.ApplicationRuntime, *kernel.Error) {
	out := platform.ApplicationRuntime{Scope: "authorized", State: "healthy", Versions: []platform.ApplicationVersion{}, Data: []platform.ApplicationData{}, Warnings: []platform.ApplicationWarning{}}
	if ref.Kind != platform.AssetApp || ref.Check() != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a published application")
	}
	if err := ctx.Admit(m); err != nil {
		return out, err
	}
	if ctx.Quarantined() {
		if refusal := ctx.RecoveryAllowed(m); refusal != nil {
			return out, refusal
		}
		out.State = "isolated"
		out.Artifact = platform.ApplicationArtifact{Ref: ref, Model: []platform.AssetBinding{}, Rules: []platform.AssetBinding{}, Pages: []platform.AssetBinding{}, Flows: []platform.AssetBinding{}, AI: []platform.AssetBinding{}, Integrations: []platform.AssetBinding{}, Roles: []platform.ApplicationRole{}, Depends: []platform.AssetBinding{}, Environment: []platform.ApplicationBinding{}}
		out.Recovery = platform.ApplicationRecovery{State: "isolated", Predecessors: "unverified", Quarantined: true, Scope: "tenant"}
		return out, nil
	}
	visible := map[platform.AssetRef]bool{}
	fields := map[platform.AssetRef]map[string]bool{}
	for _, definition := range ctx.Definitions(m) {
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
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Application is unavailable to this member")
	}
	assets, err := ctx.Assets()
	if err != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	candidate, err := ctx.CloseCandidate([]platform.AssetRef{ref}, assets, []platform.AssetRef{ref})
	if err != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	artifact, err := platform.ArtifactOfApplication(ref, candidate.Assets)
	if err != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	out.Artifact = artifact
	out.Monitoring.AgentBudgets = []platform.ApplicationAgentBudget{}
	if len(artifact.AI) > 0 {
		out.Monitoring.MemberQuota = ctx.Quota(m, now)
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetAgent || !visible[asset.Ref] {
			continue
		}
		var descriptor platform.AgentReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
		}
		out.Monitoring.AgentBudgets = append(out.Monitoring.AgentBudgets, platform.ApplicationAgentBudget{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Budget: descriptor.Budget})
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
		owner := ctx.App(binding.Ref.App)
		if owner == nil || !visible[binding.Ref] || binding.Ref.Kind != platform.AssetObject || owners[binding.Ref.App] {
			continue
		}
		owners[binding.Ref.App] = true
		for _, receipt := range owner.AcceptedLedger().RecordsFor(ctx.Tenant) {
			sub := receipt.GetSubmission()
			if sub == nil || !model[sub.GetTarget().GetType()] || sub.GetPrincipalId() != m.ID && !ctx.Readable(m, sub.GetTarget().GetType()+"/"+sub.GetTarget().GetId(), now) {
				continue
			}
			accepted[binding.Ref.App+"/"+receipt.GetChangeId()] = true
		}
	}
	out.Monitoring.Accepted = len(accepted)
	codeCounts := map[string]platform.RefusalCodeCount{}
	for _, refusal := range ctx.Refusals() {
		var sub pb.Submission
		if protojson.Unmarshal(refusal.Submission, &sub) != nil || !model[sub.GetTarget().GetType()] || sub.GetPrincipalId() != m.ID && !ctx.Readable(m, sub.GetTarget().GetType()+"/"+sub.GetTarget().GetId(), now) {
			continue
		}
		out.Monitoring.Refused++
		category, code := actions.RefusalCategory(refusal.Error, refusal.Business, refusal.Issues)
		if category == "business" {
			out.Monitoring.BusinessRefusals++
		}
		codeKey := category + "/" + sub.GetTarget().GetType() + "/" + code
		count := codeCounts[codeKey]
		count.Category, count.Code, count.Type = category, code, sub.GetTarget().GetType()
		count.Count++
		codeCounts[codeKey] = count
		if slices.ContainsFunc(refusal.Issues, func(issue platform.FieldIssue) bool { return issue.Constraint != "" }) {
			out.Monitoring.ConstraintFailures++
		}
	}
	for _, fact := range ctx.businessFacts() {
		typ, _, _ := strings.Cut(fact.Subject, "/")
		if !model[typ] || fact.Member != m.ID && !ctx.Readable(m, fact.Subject, now) {
			continue
		}
		out.Monitoring.BusinessRefusals++
		key := "business/" + typ + "/" + fact.Code
		count := codeCounts[key]
		count.Category, count.Code, count.Type = "business", fact.Code, typ
		count.Count++
		codeCounts[key] = count
	}
	for _, count := range codeCounts {
		out.Monitoring.RefusalCodes = append(out.Monitoring.RefusalCodes, count)
	}
	slices.SortFunc(out.Monitoring.RefusalCodes, func(a, b platform.RefusalCodeCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Category+"/"+a.Type+"/"+a.Code, b.Category+"/"+b.Type+"/"+b.Code)
	})
	if count := out.Monitoring.Accepted + out.Monitoring.Refused; count > 0 {
		out.Monitoring.RefusalRate = float64(out.Monitoring.Refused) / float64(count)
	}
	for _, release := range ctx.activeVersions() {
		candidate := release.Candidate
		if !slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref }) {
			continue
		}
		version, err := platform.ArtifactOfApplication(ref, candidate.Assets)
		if err != nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application version history is unavailable")
		}
		out.Versions = append(out.Versions, platform.ApplicationVersion{Version: version.Version, Release: candidate.ID, At: release.At, Active: version.Version == artifact.Version})
	}
	slices.SortFunc(out.Versions, func(a, b platform.ApplicationVersion) int {
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
		out.Versions = append(out.Versions, platform.ApplicationVersion{Version: artifact.Version, Active: true})
	}
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
	out.Maintenance = ctx.maintenance(out.Artifact)
	var refusal *kernel.Error
	out.AI, refusal = ctx.AI(m, candidate.Assets, visible)
	if refusal != nil {
		return out, refusal
	}
	out.AI = ctx.AIUsage(m, ref, out.AI)
	for _, binding := range out.Artifact.Model {
		if binding.Ref.Kind != platform.AssetObject {
			continue
		}
		count, refusal := ctx.RecordCount(m, binding.Ref.Name, now)
		if refusal != nil {
			out.State = "degraded"
			continue
		}
		data := platform.ApplicationData{Ref: binding.Ref, Records: count}
		if artifact.Profile != nil {
			data.Expected = artifact.Profile.ExpectedRecords[binding.Ref.Name]
		}
		out.Data = append(out.Data, data)
		if data.Expected > 0 && int64(data.Records) > data.Expected {
			out.Warnings = append(out.Warnings, platform.ApplicationWarning{Kind: "records", Resource: binding.Ref.Name, Actual: int64(data.Records), Expected: data.Expected})
		}
	}
	runs, refusal := ctx.Runs(m, ref, 0, 100, now)
	if refusal != nil {
		return out, refusal
	}
	out.Runs = runs.Total
	if artifact.Profile != nil && artifact.Profile.ExpectedRuns > 0 && int64(out.Runs) > artifact.Profile.ExpectedRuns {
		out.Warnings = append(out.Warnings, platform.ApplicationWarning{Kind: "runs", Actual: int64(out.Runs), Expected: artifact.Profile.ExpectedRuns})
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

func (ctx Context) businessFacts() []platform.BusinessRefusalFact {
	out := []platform.BusinessRefusalFact{}
	for _, raw := range ctx.Answers("business") {
		var header struct{ Kind string }
		if json.Unmarshal(raw, &header) != nil || header.Kind != "record-batch" {
			continue
		}
		result, _, err := acceptedformat.DecodeBatch(raw)
		if err == nil {
			out = append(out, result.Business...)
		}
	}
	return out
}

func (ctx Context) activeVersions() []ActiveVersion {
	out := []ActiveVersion{}
	for key, raw := range ctx.Answers("active") {
		if !strings.HasPrefix(key, "release:") {
			continue
		}
		release, err := acceptedformat.DecodeRelease(raw)
		if err != nil || !release.Active {
			continue
		}
		candidate, err := platform.ReadCandidate(release.CandidateID, release.Bytes)
		if err == nil {
			out = append(out, ActiveVersion{Candidate: candidate, At: release.At})
		}
	}
	return out
}
