package platformserver

import automation "platformserver/engine/automation"

import (
	"encoding/json"
	"fmt"
	controlowner "platformserver/kernel/control"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	appowner "platformserver/engine/applications"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/studio/build"
	"slices"
	"time"
)

func (t *Tenant) Applications(m platform.Member) []platform.ApplicationView {
	return appowner.Applications(t.Definitions(m))
}
func (t *Tenant) PageView(m platform.Member, app, owner, page string) (platform.PageView, *kernel.Error) {
	return appowner.PageView(t.Definitions(m), t.Entities(m), t.Catalog(m), app, owner, page)
}
func (t *Tenant) ApplicationRuns(m platform.Member, ref platform.AssetRef, offset, limit int, now time.Time, filters ...platform.ApplicationRunFilter) (platform.ApplicationRunPage, *kernel.Error) {
	return t.applicationContext().Runs(m, ref, offset, limit, now, filters...)
}
func (t *Tenant) ApplicationRuntimeOf(m platform.Member, ref platform.AssetRef, now time.Time) (platform.ApplicationRuntime, *kernel.Error) {
	return t.applicationContext().Runtime(m, ref, now)
}
func (t *Tenant) bindApplications(resource platform.AssetRef, release, dependencies string, jobOwner ...string) ([]platform.AssetBinding, error) {
	return t.applicationContext().Bind(resource, release, dependencies, jobOwner...)
}
func (t *Tenant) applicationRunGraphsLocked(ref platform.AssetRef) (map[platform.AssetRef]platform.ReleaseAsset, map[string]map[platform.AssetRef]bool, error) {
	return t.applicationContext().Graphs(ref)
}
func checkRunApplicationBindings(record any) error {
	var bindings []platform.AssetBinding
	switch run := record.(type) {
	case platform.FlowInstance:
		bindings = run.Applications
	case platform.AgentRunRecord:
		bindings = run.Applications
	case build.FunctionRun:
		bindings = run.Applications
	}
	return appowner.CheckBindings(bindings)
}
func (h hostView) ApplicationBindings(resource platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
	return h.t.bindApplications(resource, release, dependencies)
}
func (t *Tenant) ApplicationRecordDiagnostic(m platform.Member, ref platform.AssetRef, typ, id string, now time.Time) (platform.ApplicationDiagnostic, *kernel.Error) {
	out := platform.ApplicationDiagnostic{Application: ref, Type: typ, Record: id, Decisions: []platform.ApplicationDecisionTrace{}, Runs: []platform.ApplicationRun{}}
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
		trace := platform.ApplicationDecisionTrace{Change: change.Change, Schema: change.Schema, At: change.At, Versions: []platform.ApplicationVersion{}}
		if definition := t.committed.DecisionDefinition(owner + "/" + change.Change); definition != nil && definition.Definition.Ref.Name == change.Schema {
			projection := *definition
			if projection.Observation != nil {
				projection.Observation = t.visibleObservationEvidence(m, projection.Observation, now)
			}
			projection.Dependencies = slices.DeleteFunc(slices.Clone(definition.Dependencies), func(binding platform.AssetBinding) bool { return !visible[binding.Ref] })
			projection.Applications = slices.DeleteFunc(slices.Clone(definition.Applications), func(binding platform.AssetBinding) bool { return !visible[binding.Ref] })
			trace.Definition, trace.Content = &projection, &projection.Contract
			for _, binding := range projection.Applications {
				if binding.Ref == ref {
					trace.Versions = append(trace.Versions, platform.ApplicationVersion{Version: binding.SourceVersion, Release: projection.Release, At: change.At, Active: binding.SourceVersion == runtime.Artifact.Version})
				}
			}
		}
		for _, receipt := range receipts {
			if receipt.GetChangeId() != change.Change {
				continue
			}
			if binding, exists := t.committed.SchemaVersion(owner + "/" + receipt.GetSubmission().GetIdempotencyKey()); exists && trace.Content == nil {
				trace.Content = &binding
			}
			break
		}
		out.Decisions = append(out.Decisions, trace)
	}
	t.mu.Unlock()
	for skip := 0; ; skip += 100 {
		page, refusal := t.ApplicationRuns(m, ref, skip, 100, now, platform.ApplicationRunFilter{Subject: fmt.Sprintf("%s/%s", typ, id)})
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
func (t *Tenant) applicationContext() appowner.Context {
	return appowner.Context{Tenant: t.ID, Lock: t.mu.Lock, Unlock: t.mu.Unlock, Admit: t.admits, Definitions: t.Definitions, Entity: t.entity, App: t.app,
		Assets: func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) }, Candidate: t.releases.Candidate,
		Candidates: func() map[string]json.RawMessage { return t.releases.Candidates() }, CloseCandidate: t.candidateWithBindings,
		Rows: func(m platform.Member, typ string, q platform.Query, at time.Time) ([]any, *kernel.Error) {
			page, err := t.recordSelectionFrom(t.records, m, typ, q, at)
			return page.Records, err
		},
		Computations: func(m platform.Member) []appowner.Computation {
			t.opsMu.Lock()
			effects := []platform.Effect{}
			for _, x := range t.outbound {
				if x.Endpoint == acceptedformat.OperationEndpoint {
					effects = append(effects, x.Effect)
				}
			}
			t.opsMu.Unlock()
			out := []appowner.Computation{}
			for _, effect := range effects {
				var binding operationBinding
				if json.Unmarshal([]byte(effect.Body), &binding) != nil || binding.Build != nil {
					continue
				}
				result, err := t.operationResult(platform.NewCaller(runtime{t}, m, controlowner.PlatformApp, false, false), effect.ID)
				if err != nil {
					continue
				}
				out = append(out, appowner.Computation{Effect: effect, Ref: platform.AssetRef{App: effect.App, Kind: platform.AssetCompute, Name: binding.Definition.Name}, Title: binding.Definition.Title, Call: binding.Call, Result: result})
			}
			return out
		}, Effects: func(m platform.Member) ([]platform.Effect, *kernel.Error) {
			value, err := t.read(m, "effects")
			effects, _ := value.([]platform.Effect)
			effects = slices.DeleteFunc(effects, func(effect platform.Effect) bool { return effect.Endpoint == acceptedformat.OperationEndpoint })
			return effects, err
		},
		Jobs: func(m platform.Member) ([]appowner.Job, *kernel.Error) {
			value, err := t.read(m, "work")
			tasks, _ := value.([]automation.Task)
			out := []appowner.Job{}
			for _, task := range tasks {
				out = append(out, appowner.Job{Applications: task.Applications, ID: task.ID, Kind: task.Kind, App: task.App, Title: task.Title, State: task.State, Error: task.Error, Last: task.Last, Withheld: task.Withheld})
			}
			return out, err
		},
		Readable: t.readable, Quarantined: t.quarantined, RecoveryAllowed: func(m platform.Member) *kernel.Error {
			c := platform.NewCaller(runtime{t}, m, controlowner.PlatformApp, false, false)
			return authorizationRefusal(t.authorizeNative(c, "application recovery", "read", []string{controlowner.Admin}, true))
		},
		Quota: func(m platform.Member, at time.Time) *platform.AIQuota {
			if t.ai == nil {
				return nil
			}
			quota := t.ai.Quota(m, t.limits(), at)
			return &quota
		},
		Refusals: func() []appowner.Refusal {
			out := []appowner.Refusal{}
			for _, r := range t.committed.AllRefusals() {
				out = append(out, appowner.Refusal{Submission: r.Submission, Error: r.Error, Business: r.Business, Issues: r.Issues})
			}
			return out
		},
		BusinessFacts: func() []platform.BusinessRefusalFact {
			out := []platform.BusinessRefusalFact{}
			for _, raw := range t.committed.AllAnswers("business") {
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
		},
		ActiveVersions: func() []appowner.ActiveVersion {
			out := []appowner.ActiveVersion{}
			for key, raw := range t.committed.AllAnswers("active") {
				if len(key) < 8 || key[:8] != "release:" {
					continue
				}
				release, err := acceptedformat.DecodeRelease(raw)
				if err != nil || !release.Active {
					continue
				}
				candidate, err := platform.ReadCandidate(release.CandidateID, release.Bytes)
				if err == nil {
					out = append(out, appowner.ActiveVersion{Candidate: candidate, At: release.At})
				}
			}
			return out
		},
		AI: t.applicationAILocked, AIUsage: t.applicationAIUsage, RecordCount: func(m platform.Member, typ string, at time.Time) (int, *kernel.Error) {
			page, err := t.Records(m, typ, platform.Query{Limit: 1}, at)
			return page.Total, err
		},
	}
}
