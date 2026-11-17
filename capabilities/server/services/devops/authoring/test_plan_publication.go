package authoring

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/devops/descriptor"
	"platformserver/services/resources/publication"
)

type testPlanOwner[O, P, F any] struct {
	env        descriptor.TestPlanEnvironment[F]
	evaluation descriptor.EvaluationPublication[O, P, F]
}

func TestPlanPublication[O, P, F any](env descriptor.TestPlanEnvironment[F], evaluation descriptor.EvaluationPublication[O, P, F]) descriptor.TestPlanPublication[O, P, F] {
	o := &testPlanOwner[O, P, F]{env: env, evaluation: evaluation}
	return descriptor.TestPlanPublication[O, P, F]{Asset: o.testPlanAsset, Draft: o.testPlanDraft,
		Decode: o.decode, Validate: o.validate, ReadFrozen: o.readFrozen,
		Assets: o.testPlanAssets, Resolve: o.testPlanPinned, FreezeBatch: o.prepareTestPlanPublications,
		VersionedDraft: o.testPlanDraftAssets, Submit: o.submitTestPlan}
}

func (o *testPlanOwner[O, P, F]) decode(image json.RawMessage) (platform.AssetRef, error) {
	_, asset, err := o.testPlanImage(image)
	return asset.Ref, err
}

func (o *testPlanOwner[O, P, F]) validate(image json.RawMessage) error {
	_, _, err := o.testPlanImage(image)
	return err
}

func (*testPlanOwner[O, P, F]) readFrozen(image json.RawMessage) (json.RawMessage, error) {
	var record struct{ Published string }
	if err := json.Unmarshal(image, &record); err != nil {
		return nil, err
	}
	return json.RawMessage(record.Published), nil
}

func (o *testPlanOwner[O, P, F]) testPlanAsset(plan descriptor.TestPlan[O, P, F], version int) (platform.ReleaseAsset, error) {
	if plan.Variant == "evaluation-set" {
		return o.evaluation.Asset(plan, version)
	}
	descriptor := platform.TestPlanDescriptor{Name: plan.Name, Title: plan.Title, Roots: plan.Roots, At: plan.At, Organization: plan.Organization, Samples: plan.Samples, Steps: plan.Steps}
	if err := descriptor.Check(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return o.env.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.App, Kind: platform.AssetTestPlan, Name: plan.Name}, ContractVersion: 1, SourceVersion: o.env.SourceVersion() + ".tests-" + strconv.Itoa(version), Body: body})
}

func testPlanVersionPrefix(kind platform.AssetKind) string {
	if kind == platform.AssetEvaluation {
		return ".evaluation-"
	}
	return ".tests-"
}

func (o *testPlanOwner[O, P, F]) testPlanAssetName(asset platform.ReleaseAsset) (string, error) {
	if asset.Ref.Kind == platform.AssetTestPlan {
		descriptor, err := platform.ReadTestPlan(asset.Body)
		return descriptor.Name, err
	}
	if asset.Ref.Kind == platform.AssetEvaluation {
		descriptor, err := platform.ReadEvaluationSet(asset.Body)
		return descriptor.Name, err
	}
	return "", fmt.Errorf("unsupported test plan asset kind")
}

func (o *testPlanOwner[O, P, F]) testPlanImage(raw []byte) (descriptor.TestPlan[O, P, F], platform.ReleaseAsset, error) {
	var plan descriptor.TestPlan[O, P, F]
	if o.env.Decode(raw, &plan) != nil || plan.ID == "" || plan.DefinitionVersion < 1 || plan.DefinitionVersion > 64 || len(plan.Versions) != plan.DefinitionVersion {
		return plan, platform.ReleaseAsset{}, fmt.Errorf("invalid evaluation set publication")
	}
	var asset platform.ReleaseAsset
	if o.env.Decode([]byte(plan.Published), &asset) != nil {
		return plan, asset, fmt.Errorf("evaluation set has no immutable descriptor")
	}
	for i, raw := range plan.Versions {
		var retained platform.ReleaseAsset
		if o.env.Decode([]byte(raw), &retained) != nil || o.checkTestPlanAsset(retained, plan.Name, i+1) != nil || retained.Ref.Kind != asset.Ref.Kind {
			return plan, asset, fmt.Errorf("evaluation set retained family differs")
		}
	}
	if o.checkTestPlanAsset(asset, plan.Name, plan.DefinitionVersion) != nil || plan.Versions[len(plan.Versions)-1] != plan.Published {
		return plan, asset, fmt.Errorf("evaluation set published pointer differs")
	}
	if asset.Ref.Kind != descriptor.TestPlanKind(plan.Variant) {
		return plan, asset, fmt.Errorf("test plan publication kind differs")
	}
	return plan, asset, nil
}

func (o *testPlanOwner[O, P, F]) checkTestPlanAsset(asset platform.ReleaseAsset, name string, version int) error {
	actual, err := o.testPlanAssetName(asset)
	if err != nil || actual != name || asset.Ref.App != o.env.App || asset.Ref.Name != name || asset.ContractVersion != 1 || asset.SourceVersion != o.env.SourceVersion()+testPlanVersionPrefix(asset.Ref.Kind)+strconv.Itoa(version) || o.env.CheckDependencies(asset) != nil {
		return fmt.Errorf("evaluation set publication differs from its descriptor")
	}
	return nil
}

func (o *testPlanOwner[O, P, F]) submitTestPlan(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Authority().Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		if !c.Holds(o.env.App, o.env.Builder) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		plan, ok := platform.Get[descriptor.TestPlan[O, P, F]](c, s.GetTarget().GetId())
		if !ok || plan.Archived || plan.DefinitionVersion >= 64 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a saved evaluation plan with an available version")
		}
		if plan.Published != "" {
			if _, _, err := o.testPlanImage(platform.Raw(plan)); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Evaluation set could not be published: {why}", err.Error())
			}
		}
		asset, err := o.testPlanAsset(plan, plan.DefinitionVersion+1)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Evaluation set could not be published: {why}", err.Error())
		}
		return func(r *pb.ChangeRecord) {
			plan.DefinitionVersion++
			plan.Published = string(platform.Raw(asset))
			plan.Versions = append(slices.Clone(plan.Versions), plan.Published)
			c.Put(r, plan)
		}, nil
	})
}

func (o *testPlanOwner[O, P, F]) testPlanAssets() ([]platform.ReleaseAsset, error) {
	rows, err := publication.Inventory[descriptor.TestPlan[O, P, F]](o.env.Caller())
	if err != nil {
		return nil, err
	}
	out := []platform.ReleaseAsset{}
	for _, plan := range rows {
		if plan.Published == "" || plan.Archived {
			continue
		}
		_, asset, err := o.testPlanImage(platform.Raw(plan))
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (o *testPlanOwner[O, P, F]) testPlanPinned(ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
	rows, err := publication.Inventory[descriptor.TestPlan[O, P, F]](o.env.Caller())
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, plan := range rows {
		if plan.Published == "" {
			continue
		}
		if _, _, err := o.testPlanImage(platform.Raw(plan)); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range plan.Versions {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == source {
				return asset, nil
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("evaluation set version is not retained")
}

func (o *testPlanOwner[O, P, F]) testPlanDraftAssets(id string, releaseAssets func() ([]platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = releaseAssets()
	if err != nil {
		return
	}
	plan, found := platform.Get[descriptor.TestPlan[O, P, F]](o.env.Caller(), id)
	if !found || plan.Archived || plan.DefinitionVersion >= 64 {
		err = fmt.Errorf("saved evaluation plan is unavailable")
		return
	}
	if plan.Published != "" {
		_, old, problem := o.testPlanImage(platform.Raw(plan))
		if problem != nil {
			err = problem
			return
		}
		prior, hadPrior = old.Ref, true
	}
	asset, problem := o.testPlanAsset(plan, plan.DefinitionVersion+1)
	if problem != nil {
		err = problem
		return
	}
	next = asset.Ref
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (o *testPlanOwner[O, P, F]) prepareTestPlanPublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := publication.Inventory[descriptor.TestPlan[O, P, F]](o.env.Caller())
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != o.env.App || asset.Ref.Kind != platform.AssetEvaluation && asset.Ref.Kind != platform.AssetTestPlan {
			continue
		}
		name, err := o.testPlanAssetName(asset)
		if err != nil {
			return nil, err
		}
		ordinal, ok := strings.CutPrefix(asset.SourceVersion, o.env.SourceVersion()+testPlanVersionPrefix(asset.Ref.Kind))
		version, err := strconv.Atoi(ordinal)
		if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal {
			return nil, fmt.Errorf("invalid evaluation set version")
		}
		if err := o.checkTestPlanAsset(asset, name, version); err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(plan descriptor.TestPlan[O, P, F]) bool { return !plan.Archived && plan.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("evaluation set has no target plan")
		}
		plan := rows[i]
		if version <= plan.DefinitionVersion {
			prior, err := o.testPlanPinned(asset.Ref, asset.SourceVersion)
			if err != nil || !o.env.EqualAsset(prior, asset) {
				return nil, fmt.Errorf("evaluation set retained bytes differ")
			}
			continue
		}
		if version != plan.DefinitionVersion+1 {
			return nil, fmt.Errorf("evaluation set needs its retained predecessor")
		}
		frozen, err := o.testPlanDraft(asset)
		if err != nil {
			return nil, err
		}
		plan.Title, plan.Model, plan.Evaluation, plan.Function, plan.FunctionVersion = frozen.Title, frozen.Model, frozen.Evaluation, frozen.Function, frozen.FunctionVersion
		if asset.Ref.Kind == platform.AssetTestPlan {
			plan.Roots, plan.At, plan.Organization, plan.Samples, plan.Steps = frozen.Roots, frozen.At, frozen.Organization, frozen.Samples, frozen.Steps
			plan.Application, plan.CandidateID, plan.Object, plan.Process, plan.As, plan.Actors, plan.Sample = "", "", "", "", "", nil, ""
		}
		if _, err := o.testPlanPinned(asset.Ref, asset.SourceVersion); err == nil {
			return nil, fmt.Errorf("evaluation set version already exists")
		}
		plan.DefinitionVersion, plan.Published = version, string(platform.Raw(asset))
		plan.Versions = append(slices.Clone(plan.Versions), plan.Published)
		out = append(out, platform.ReleasePublication{Schema: o.env.Type + ".publish", Image: platform.Raw(plan)})
	}
	return out, nil
}

func (o *testPlanOwner[O, P, F]) testPlanDraft(asset platform.ReleaseAsset) (descriptor.TestPlan[O, P, F], error) {
	if asset.Ref.Kind == platform.AssetTestPlan {
		d, err := platform.ReadTestPlan(asset.Body)
		if err != nil {
			return descriptor.TestPlan[O, P, F]{}, err
		}
		return descriptor.TestPlan[O, P, F]{Variant: "test-plan", Name: d.Name, Title: d.Title, Roots: d.Roots, At: d.At, Organization: d.Organization, Samples: d.Samples, Steps: d.Steps}, nil
	}
	return o.evaluation.Draft(asset)
}
