package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

type MigrationReview struct {
	ID     string                 `json:"id"`
	From   string                 `json:"from"`
	To     string                 `json:"to"`
	Types  []MigrationReviewType  `json:"types"`
	Steps  int                    `json:"steps"`
	Issues []MigrationReviewIssue `json:"issues"`
}

type MigrationReviewIssue struct {
	Type   string `json:"type"`
	Record string `json:"record"`
	Step   int    `json:"step"`
	Reason string `json:"reason"`
}

type MigrationReviewType struct {
	Type        string `json:"type"`
	Records     int    `json:"records"`
	SourceShape string `json:"sourceShape"`
	TargetShape string `json:"targetShape"`
}

// ReviewMigrationPlan uses the original private release installation path to
// compile the target shape; no live definition or business row is changed.
func (t *Tenant) ReviewMigrationPlan(m platform.Member, plan platform.MigrationPlan, at time.Time) (MigrationReview, error) {
	if err := t.admits(m); err != nil {
		return MigrationReview{}, err
	}
	if err := plan.Check(); err != nil {
		return MigrationReview{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	m = t.authorizationMember(m)
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/migrations/review", "read", []string{build.Builder, build.Publisher}, true)); err != nil {
		return MigrationReview{}, err
	}
	if t.releases.active != plan.From {
		return MigrationReview{}, fmt.Errorf("migration source must be the active sealed release")
	}
	from, err := platform.ReadCandidate(plan.From, t.releases.raw(plan.From))
	if err != nil {
		return MigrationReview{}, err
	}
	to, err := platform.ReadCandidate(plan.To, t.releases.raw(plan.To))
	if err != nil {
		return MigrationReview{}, err
	}
	types := []string{}
	for _, step := range plan.Steps {
		types = append(types, step.Types...)
		if step.Target != "" {
			types = append(types, step.Target)
		}
	}
	slices.Sort(types)
	types = slices.Compact(types)
	records := t.records.forkRecords()
	sources := map[string]platform.EntityInfo{}
	for _, typ := range types {
		old := records.types[typ]
		if old == nil {
			continue
		}
		ref := platform.AssetRef{App: old.info.App, Kind: platform.AssetObject, Name: typ}
		if !slices.ContainsFunc(from.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref }) {
			return MigrationReview{}, fmt.Errorf("migration source type is outside the sealed release")
		}
		sources[typ] = old.info
		old.rows = map[string]*row{}
	}
	view, err := t.assetInstallationView(records)
	if err != nil {
		return MigrationReview{}, err
	}
	registry, err := view.assetRegistryLocked()
	if err != nil {
		return MigrationReview{}, err
	}
	context, err := view.assetReleaseContextLocked(registry, to.Assets, m, at, true)
	if err != nil {
		return MigrationReview{}, err
	}
	end, err := registry.BeginBootstrap(to.Assets)
	if err != nil {
		return MigrationReview{}, err
	}
	defer end()
	plans, err := registry.PrepareReleaseAssets(to.Assets, context)
	if err != nil {
		return MigrationReview{}, err
	}
	installations := []releaseInstallation{}
	for _, plan := range plans {
		before, err := view.installationPredecessor(plan.Record)
		if err != nil {
			return MigrationReview{}, err
		}
		ref := plan.Ref
		installation := releaseInstallation{Before: before, Ref: &ref, Record: plan.Record}
		installation.Schema, installation.Image = plan.Schema, plan.Image
		installations = append(installations, installation)
	}
	trial, err := view.stageReleaseInstallationLocked(installations, true, true)
	if err != nil {
		return MigrationReview{}, err
	}
	targets := map[string]platform.EntityInfo{}
	for _, typ := range types {
		if next := trial.records.types[typ]; next != nil {
			targets[typ] = next.info
		}
	}
	program, err := platform.CompileMigrationPlan(plan, sources, targets)
	if err != nil {
		return MigrationReview{}, err
	}
	review := MigrationReview{From: plan.From, To: plan.To, Steps: len(plan.Steps), Types: []MigrationReviewType{}, Issues: []MigrationReviewIssue{}}
	witnesses := map[string]string{}
	for _, typ := range types {
		source, exists := sources[typ]
		if !exists {
			continue
		}
		et := t.records.types[typ]
		fields := []string{}
		for _, field := range source.Fields {
			fields = append(fields, field.Name)
		}
		for id := range et.rows {
			decision := t.authorizeFrom(t.records, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "read", Type: typ, Record: id, Fields: fields}, at, false)
			if err := authorizationRefusal(decision); err != nil {
				return MigrationReview{}, err
			}
			image, err := et.rows[id].image()
			if err != nil {
				return MigrationReview{}, err
			}
			witness, err := canonicalDigest(image)
			if err != nil {
				return MigrationReview{}, err
			}
			witnesses[typ+"/"+id] = witness
			for index, step := range plan.Steps {
				if !slices.Contains(step.Types, typ) {
					continue
				}
				result, err := program.Transform(index, typ, image, platform.ExprContext{Me: m.ID, Now: at, Today: at.UTC().Format(time.DateOnly)})
				if err != nil {
					review.Issues = append(review.Issues, MigrationReviewIssue{Type: typ, Record: id, Step: index + 1, Reason: err.Error()})
					break
				}
				image, err = json.Marshal(result.Values)
				if err != nil {
					return MigrationReview{}, err
				}
			}
		}
		sourceHash, err := canonicalDigest(source.Fields)
		if err != nil {
			return MigrationReview{}, err
		}
		targetHash, err := canonicalDigest(targets[typ].Fields)
		if err != nil {
			return MigrationReview{}, err
		}
		review.Types = append(review.Types, MigrationReviewType{Type: typ, Records: len(et.rows), SourceShape: sourceHash, TargetShape: targetHash})
	}
	slices.SortFunc(review.Issues, func(a, b MigrationReviewIssue) int {
		if a.Type < b.Type || a.Type == b.Type && a.Record < b.Record {
			return -1
		}
		if a.Type > b.Type || a.Type == b.Type && a.Record > b.Record {
			return 1
		}
		return a.Step - b.Step
	})
	review.ID, err = canonicalDigest(struct {
		Plan    platform.MigrationPlan
		Shapes  []MigrationReviewType
		Records map[string]string
		Member  string
	}{plan, review.Types, witnesses, m.ID})
	return review, err
}
