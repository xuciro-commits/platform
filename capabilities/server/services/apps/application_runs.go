package applications

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (c Context) ScopedRuns(m platform.Member, ref platform.AssetRef, offset, limit int, now time.Time, filters ...platform.ApplicationRunFilter) (platform.ApplicationRunPage, *kernel.Error) {
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
	m = c.AuthorizationMember(m)
	if refusal := c.refreshRunIndex(m, ref, now); refusal == nil {
		if page, err := c.runsFromIndex(m, ref, offset, limit, filters...); err == nil {
			return page, nil
		}
	}
	return c.Runs(m, ref, offset, limit, now, filters...)
}

func (c Context) refreshRunIndex(m platform.Member, ref platform.AssetRef, now time.Time) *kernel.Error {
	key := "application/" + ref.App + "/" + ref.Name + "/" + m.ID
	var refusal *kernel.Error
	c.RefreshRunIndex(key, now, func() bool {
		refusal = c.rebuildRunIndex(m, ref, key, now)
		return refusal == nil
	})
	return refusal
}

func (c Context) rebuildRunIndex(m platform.Member, ref platform.AssetRef, key string, now time.Time) *kernel.Error {
	runs := []platform.ApplicationRun{}
	for skip := 0; ; {
		page, refusal := c.Runs(m, ref, skip, 100, now)
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
	if err := c.ReplaceRunIndex(key, entries); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNSPECIFIED, Message: err.Error()}
	}
	return nil
}

func (c Context) runsFromIndex(m platform.Member, ref platform.AssetRef, offset, limit int, filters ...platform.ApplicationRunFilter) (platform.ApplicationRunPage, error) {
	indexed, err := c.QueryRunIndex("application/" + ref.App + "/" + ref.Name + "/" + m.ID)
	if err != nil {
		return platform.ApplicationRunPage{}, err
	}
	title := ""
	for _, definition := range c.Definitions(m) {
		if definition.Ref == ref && definition.Application != nil {
			title = definition.Application.Title
			break
		}
	}
	return IndexedRuns(indexed, ref, title, offset, limit, filters...), nil
}

type DecisionChange struct {
	Change, Schema string
	At             time.Time
}

func (c Context) RecordDiagnostic(m platform.Member, ref platform.AssetRef, typ, id string, now time.Time) (platform.ApplicationDiagnostic, *kernel.Error) {
	out := platform.ApplicationDiagnostic{Application: ref, Type: typ, Record: id, Decisions: []platform.ApplicationDecisionTrace{}, Runs: []platform.ApplicationRun{}}
	runtime, refusal := c.RuntimeOf(m, ref, now)
	if refusal != nil {
		return out, refusal
	}
	if !slices.ContainsFunc(runtime.Artifact.Model, func(binding platform.AssetBinding) bool {
		return binding.Ref.Kind == platform.AssetObject && binding.Ref.Name == typ
	}) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record is outside this application")
	}
	history, refusal := c.RecordHistory(m, typ, id, now)
	if refusal != nil {
		return out, refusal
	}
	owner := c.RecordOwner(typ)
	receipts := c.ReceiptKeys(owner)
	visible := map[platform.AssetRef]bool{}
	for _, definition := range c.Definitions(m) {
		visible[definition.Ref] = true
	}
	for _, change := range history {
		trace := platform.ApplicationDecisionTrace{Change: change.Change, Schema: change.Schema, At: change.At, Versions: []platform.ApplicationVersion{}}
		if definition := c.Decision(owner + "/" + change.Change); definition != nil && definition.Definition.Ref.Name == change.Schema {
			projection := *definition
			if projection.Observation != nil {
				projection.Observation = c.VisibleEvidence(m, projection.Observation, now)
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
		if key, found := receipts[change.Change]; found && trace.Content == nil {
			if binding, exists := c.SchemaVersion(owner + "/" + key); exists {
				trace.Content = &binding
			}
		}
		out.Decisions = append(out.Decisions, trace)
	}
	subject := typ + "/" + id
	for skip := 0; ; skip += 100 {
		page, refusal := c.RunsPage(m, ref, skip, 100, now, platform.ApplicationRunFilter{Subject: subject})
		if refusal != nil {
			return out, refusal
		}
		for _, run := range page.Runs {
			if run.Subject == subject {
				out.Runs = append(out.Runs, run)
			}
		}
		if skip+len(page.Runs) >= page.Total {
			break
		}
	}
	return out, nil
}
