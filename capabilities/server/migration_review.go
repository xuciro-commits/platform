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
	At     time.Time              `json:"at"`
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
	return t.reviewMigrationPlanLocked(m, plan, at)
}

func (t *Tenant) reviewMigrationPlanLocked(m platform.Member, plan platform.MigrationPlan, at time.Time, evaluation ...time.Time) (MigrationReview, error) {
	captured := at.UTC()
	if len(evaluation) > 0 {
		captured = evaluation[0].UTC()
	}
	m = t.authorizationMember(m)
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/migrations/review", "read", []string{build.Builder, build.Publisher}, true)); err != nil {
		return MigrationReview{}, err
	}
	prepared, err := t.prepareMigrationLocked(plan, m, at, t.records)
	if err != nil {
		return MigrationReview{}, err
	}
	program, sources, targets, types := prepared.program, prepared.sources, prepared.targets, prepared.types
	review := MigrationReview{At: captured, From: plan.From, To: plan.To, Steps: len(plan.Steps), Types: []MigrationReviewType{}, Issues: []MigrationReviewIssue{}}
	witnesses := map[string]string{}
	extractions := map[string]bool{}
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
				result, err := program.Transform(index, typ, image, platform.ExprContext{Me: m.ID, Now: captured, Today: captured.Format(time.DateOnly)})
				if err != nil {
					review.Issues = append(review.Issues, MigrationReviewIssue{Type: typ, Record: id, Step: index + 1, Reason: err.Error()})
					break
				}
				if step.Kind == "extract" && result.Selected {
					var id string
					_ = json.Unmarshal(result.Extracted["id"], &id)
					key := step.Target + "/" + id
					existing := t.records.types[step.Target]
					if extractions[key] || existing != nil && existing.rows[id] != nil {
						review.Issues = append(review.Issues, MigrationReviewIssue{Type: typ, Record: id, Step: index + 1, Reason: "extraction identity is already used"})
					}
					extractions[key] = true
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
		At      time.Time
	}{plan, review.Types, witnesses, m.ID, review.At})
	return review, err
}

type preparedMigration struct {
	program       *platform.MigrationProgram
	sources       map[string]platform.EntityInfo
	targets       map[string]platform.EntityInfo
	types         []string
	trial         *Tenant
	installations []releaseInstallation
}

func (t *Tenant) prepareMigrationLocked(plan platform.MigrationPlan, m platform.Member, at time.Time, store *recordStore) (*preparedMigration, error) {
	if t.releases.active != plan.From {
		return nil, fmt.Errorf("migration source must be the active sealed release")
	}
	from, err := platform.ReadCandidate(plan.From, t.releases.raw(plan.From))
	if err != nil {
		return nil, err
	}
	to, err := platform.ReadCandidate(plan.To, t.releases.raw(plan.To))
	if err != nil {
		return nil, err
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
	records := store.forkRecords()
	records.readRelease = plan.To // This is the private target decoder context.
	sources := map[string]platform.EntityInfo{}
	for _, typ := range types {
		if !slices.ContainsFunc(plan.Steps, func(step platform.MigrateStep) bool { return slices.Contains(step.Types, typ) }) {
			continue
		}
		old := records.types[typ]
		if old == nil {
			continue
		}
		ref := platform.AssetRef{App: old.info.App, Kind: platform.AssetObject, Name: typ}
		if !slices.ContainsFunc(from.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref }) {
			return nil, fmt.Errorf("migration source type is outside the sealed release")
		}
		sources[typ] = old.info
		old.rows = map[string]*row{}
	}
	view, err := t.assetInstallationView(records)
	if err != nil {
		return nil, err
	}
	registry, err := view.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	context, err := view.assetReleaseContextLocked(registry, to.Assets, m, at, true)
	if err != nil {
		return nil, err
	}
	end, err := registry.BeginBootstrap(to.Assets)
	if err != nil {
		return nil, err
	}
	defer end()
	plans, err := registry.PrepareReleaseAssets(to.Assets, context)
	if err != nil {
		return nil, err
	}
	installations := []releaseInstallation{}
	for _, plan := range plans {
		before, err := view.installationPredecessor(plan.Record)
		if err != nil {
			return nil, err
		}
		ref := plan.Ref
		installation := releaseInstallation{Before: before, Ref: &ref, Record: plan.Record}
		installation.Schema, installation.Image = plan.Schema, plan.Image
		installations = append(installations, installation)
	}
	trial, err := view.stageReleaseInstallationLocked(installations, false, true)
	if err != nil {
		return nil, err
	}
	targets := map[string]platform.EntityInfo{}
	for _, typ := range types {
		if next := trial.records.types[typ]; next != nil {
			targets[typ] = next.info
		}
	}
	program, err := platform.CompileMigrationPlan(plan, sources, targets)
	if err != nil {
		return nil, err
	}
	for _, step := range plan.Steps {
		if step.Kind != "retire-action" {
			continue
		}
		action := slices.IndexFunc(from.Assets, func(asset platform.ReleaseAsset) bool {
			return asset.Ref.Kind == platform.AssetAction && asset.Ref.Name == step.Action
		})
		if action < 0 || slices.ContainsFunc(to.Assets, func(asset platform.ReleaseAsset) bool {
			return asset.Ref.Kind == platform.AssetAction && asset.Ref.Name == step.Action
		}) {
			return nil, fmt.Errorf("action retirement must remove its named sealed source action")
		}
		var source platform.Action
		if json.Unmarshal(from.Assets[action].Body, &source) != nil || !slices.Contains(step.Types, source.Target) {
			return nil, fmt.Errorf("retired action is outside its declared object scope")
		}
		if step.Redirect != "" {
			var target *platform.Action
			for _, asset := range to.Assets {
				if asset.Ref.Kind == platform.AssetAction && asset.Ref.Name == step.Redirect && asset.Ref.App == from.Assets[action].Ref.App {
					var descriptor struct {
						Action platform.Action `json:"action"`
					}
					if json.Unmarshal(asset.Body, &descriptor) == nil {
						target = &descriptor.Action
					}
				}
			}
			installed := slices.IndexFunc(trial.definitions, func(def platform.Definition) bool {
				return def.Ref.App == from.Assets[action].Ref.App && def.Ref.Kind == platform.AssetAction && def.Ref.Name == step.Redirect
			})
			if target == nil || target.Target != source.Target || target.Automation || installed < 0 || trial.definitions[installed].Action == nil || trial.definitions[installed].Action.Approval == nil {
				return nil, fmt.Errorf("approval redirect needs a sealed approval action of the same object and authority")
			}
		}
	}
	if err := t.validateAssetStorage(trial, true, plan); err != nil {
		return nil, err
	}
	return &preparedMigration{program: program, sources: sources, targets: targets, types: types, trial: trial, installations: installations}, nil
}
