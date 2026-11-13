package host

import automation "platformserver/services/automate/runtime"

import (
	"context"
	"encoding/json"
	"fmt"
	"platformserver/services/functions/runtime"
	"platformserver/services/objects"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	appowner "platformserver/services/apps"
	"platformserver/services/resources/authoring"
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
	return recordRead(t, context.Background(), func(t *Tenant) (platform.ApplicationRunPage, *kernel.Error) {
		return t.scopedApplicationRuns(m, ref, offset, limit, now, filters...)
	})
}

func (t *Tenant) scopedApplicationRuns(m platform.Member, ref platform.AssetRef, offset, limit int, now time.Time, filters ...platform.ApplicationRunFilter) (platform.ApplicationRunPage, *kernel.Error) {
	if ref.Kind != platform.AssetApp || ref.Check() != nil || offset < 0 || limit < 1 || limit > 100 {
		return platform.ApplicationRunPage{Application: ref, Runs: []platform.ApplicationRun{}}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an application and a run page of 1–100")
	}
	if len(filters) > 0 {
		filter := filters[0]
		if filter.Kind != "" && !slices.Contains([]string{"flow", "agent", "compute", "function", "effect", "job", "dispatch"}, filter.Kind) ||
			len(filter.State) > 64 || len(filter.Subject) > 512 || !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
			return platform.ApplicationRunPage{Application: ref, Runs: []platform.ApplicationRun{}}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid application run filter")
		}
	}
	m = t.authorizationMember(m)
	if refusal := t.refreshApplicationRunIndex(m, ref, now); refusal == nil {
		if page, err := t.applicationRunsFromIndex(m, ref, offset, limit, filters...); err == nil {
			return page, nil
		}
	}
	return t.applicationContext().Runs(m, ref, offset, limit, now, filters...)
}

func (t *Tenant) refreshApplicationRunIndex(m platform.Member, ref platform.AssetRef, now time.Time) *kernel.Error {
	key := "application/" + ref.App + "/" + ref.Name + "/" + m.ID
	var refusal *kernel.Error
	t.runIndex.Refresh(key, now, func() ([]monitoring.MonitoringCoverage, bool) {
		refusal = t.rebuildApplicationRunIndex(m, ref, key, now)
		return nil, refusal == nil
	})
	return refusal
}

func (t *Tenant) rebuildApplicationRunIndex(m platform.Member, ref platform.AssetRef, key string, now time.Time) *kernel.Error {
	ctx := t.applicationContext()
	runs := []platform.ApplicationRun{}
	for skip := 0; ; {
		page, refusal := ctx.Runs(m, ref, skip, 100, now)
		if refusal != nil {
			return refusal
		}
		runs = append(runs, page.Runs...)
		skip += len(page.Runs)
		if len(page.Runs) == 0 || skip >= page.Total || skip >= 10000 {
			break
		}
	}
	entries := make([]platform.RunIndexEntry, 0, len(runs))
	for _, run := range runs {
		raw, err := json.Marshal(run)
		if err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		entries = append(entries, platform.RunIndexEntry{ID: run.Kind + "/" + run.ID, Scope: m.ID, Type: "application", App: ref.Name, Status: run.State, At: run.Created, Payload: raw})
	}
	if err := t.Log().ReplaceRunIndex(t.ID, key, entries); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: err.Error()}
	}
	return nil
}

func (t *Tenant) applicationRunsFromIndex(m platform.Member, ref platform.AssetRef, offset, limit int, filters ...platform.ApplicationRunFilter) (platform.ApplicationRunPage, error) {
	scope := "application/" + ref.App + "/" + ref.Name + "/" + m.ID
	indexed, err := t.Log().QueryRunIndex(platform.RunIndexQuery{Tenant: t.ID, Scope: scope, Offset: 0, Limit: 1000})
	if err != nil {
		return platform.ApplicationRunPage{}, err
	}
	title := ""
	if definitions, err := t.DefinitionsFor(m); err == nil {
		for _, definition := range definitions {
			if definition.Ref == ref && definition.Application != nil {
				title = definition.Application.Title
				break
			}
		}
	}
	return appowner.IndexedRuns(indexed.Entries, ref, title, offset, limit, filters...), nil
}

func (t *Tenant) ApplicationRuntimeOf(m platform.Member, ref platform.AssetRef, now time.Time) (platform.ApplicationRuntime, *kernel.Error) {
	if t.quarantined() {
		return t.applicationContext().Runtime(m, ref, now)
	}
	return recordRead(t, context.Background(), func(t *Tenant) (platform.ApplicationRuntime, *kernel.Error) {
		return t.applicationContext().Runtime(m, ref, now)
	})
}
func (t *Tenant) bindApplications(resource platform.AssetRef, release, dependencies string, jobOwner ...string) ([]platform.AssetBinding, error) {
	return t.applicationContext().Bind(resource, release, dependencies, jobOwner...)
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
	return platform.CheckApplicationBindings(bindings)
}
func (h hostView) ApplicationBindings(resource platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
	return h.t.bindApplications(resource, release, dependencies)
}
func (t *Tenant) ApplicationRecordDiagnostic(m platform.Member, ref platform.AssetRef, typ, id string, now time.Time) (platform.ApplicationDiagnostic, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (platform.ApplicationDiagnostic, *kernel.Error) {
		return t.scopedApplicationRecordDiagnostic(m, ref, typ, id, now)
	})
}

func (t *Tenant) scopedApplicationRecordDiagnostic(m platform.Member, ref platform.AssetRef, typ, id string, now time.Time) (platform.ApplicationDiagnostic, *kernel.Error) {
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
	view, refusal := t.recordDetail(m, typ, id, now, objects.RecordSectionHistory)
	if refusal != nil {
		return out, refusal
	}
	owner := t.Projection.Type(typ).Info().App
	ledger := t.App(owner).AcceptedLedger()
	receipts := ledger.RecordsFor(t.ID)
	visible := map[platform.AssetRef]bool{}
	for _, definition := range t.Definitions(m) {
		visible[definition.Ref] = true
	}
	for _, change := range view.History {
		trace := platform.ApplicationDecisionTrace{Change: change.Change, Schema: change.Schema, At: change.At, Versions: []platform.ApplicationVersion{}}
		if definition := t.Committed.DecisionDefinition(owner + "/" + change.Change); definition != nil && definition.Definition.Ref.Name == change.Schema {
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
			if binding, exists := t.Committed.SchemaVersion(owner + "/" + receipt.GetSubmission().GetIdempotencyKey()); exists && trace.Content == nil {
				trace.Content = &binding
			}
			break
		}
		out.Decisions = append(out.Decisions, trace)
	}
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
	return appowner.Context{Tenant: t.ID, Admit: t.Admits, Definitions: t.Definitions, Entity: t.Entity, App: t.App,
		Assets: func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) }, Candidate: t.releases.Candidate,
		Candidates: func() map[string]json.RawMessage { return t.releases.Candidates() }, CloseCandidate: t.candidateWithBindings,
		Rows: func(m platform.Member, typ string, q platform.Query, at time.Time) ([]any, *kernel.Error) {
			page, err := t.recordSelectionFrom(t.Projection, m, typ, q, at)
			return page.Records, err
		},
		Computations: func(m platform.Member) []appowner.Computation {
			effects := []platform.Effect{}
			for _, x := range t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.OperationEndpoint }) {
				effects = append(effects, x.Effect)
			}
			out := []appowner.Computation{}
			for _, effect := range effects {
				var binding compute.OperationBinding
				if json.Unmarshal([]byte(effect.Body), &binding) != nil || binding.Build != nil {
					continue
				}
				result, err := t.operationResult(platform.NewCaller(runtime{t: t}, m, controlowner.PlatformApp, false, false), effect.ID)
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
			c := platform.NewCaller(runtime{t: t}, m, controlowner.PlatformApp, false, false)
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
			for _, r := range t.Committed.AllRefusals() {
				out = append(out, appowner.Refusal{Submission: r.Submission, Error: r.Error, Business: r.Business, Issues: r.Issues})
			}
			return out
		},
		BusinessFacts: func() []platform.BusinessRefusalFact {
			out := []platform.BusinessRefusalFact{}
			for _, raw := range t.Committed.AllAnswers("business") {
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
			for key, raw := range t.Committed.AllAnswers("active") {
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
			page, err := t.Records(t.readContext(), m, typ, platform.Query{Limit: 1}, at)
			return page.Total, err
		},
	}
}
