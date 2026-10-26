package build

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/construction"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaEvaluationSetPublish = construction.TestPlanType + ".publish"

func (b *Build) testPlanAsset(plan construction.TestPlan, version int) (platform.ReleaseAsset, error) {
	if len(plan.Evaluation) > 0 {
		return b.evaluationSetAsset(plan, version)
	}
	descriptor := platform.TestPlanDescriptor{Name: plan.Name, Title: plan.Title, Roots: plan.Roots, At: plan.At, Organization: plan.Organization, Samples: plan.Samples, Steps: plan.Steps}
	if err := descriptor.Check(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetTestPlan, Name: plan.Name}, ContractVersion: 1, SourceVersion: b.Manifest().Version + ".tests-" + strconv.Itoa(version), Body: body})
}

func testPlanKind(plan construction.TestPlan) platform.AssetKind {
	if len(plan.Evaluation) > 0 {
		return platform.AssetEvaluation
	}
	return platform.AssetTestPlan
}
func testPlanVersionPrefix(kind platform.AssetKind) string {
	if kind == platform.AssetEvaluation {
		return ".evaluation-"
	}
	return ".tests-"
}

func (b *Build) testPlanAssetName(asset platform.ReleaseAsset) (string, error) {
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

func (b *Build) evaluationSetAsset(plan construction.TestPlan, version int) (platform.ReleaseAsset, error) {
	c := b.host.Automation(platform.Caller{}, construction.ID)
	function, ok := platform.Get[construction.Function](c, string(plan.Function))
	if !ok || function.Archived || len(plan.Evaluation) != 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("evaluation set needs its function and one saved policy")
	}
	definition, ordinal, ok := b.FunctionDefinition(function.Name, plan.FunctionVersion)
	if selected, found := b.jointDeclarations[platform.AssetRef{App: construction.ID, Kind: platform.AssetFunction, Name: function.Name}]; found {
		var frozen construction.Function
		text, valid := strings.CutPrefix(selected.SourceVersion, b.Manifest().Version+".function-")
		version, err := strconv.Atoi(text)
		if valid && err == nil && version > 0 && (plan.FunctionVersion == 0 || plan.FunctionVersion == version) && construction.ValueDecodeIntegration(selected.Body, &frozen) == nil {
			definition, ordinal, ok = frozen.Definition(), version, true
		}
	}
	if !ok || ordinal < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("evaluation function version is not published")
	}
	descriptor := platform.EvaluationSetDescriptor{Name: plan.Name, Title: plan.Title, Function: platform.AssetBinding{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetFunction, Name: function.Name}, SourceVersion: b.Manifest().Version + ".function-" + strconv.Itoa(ordinal)}, Model: plan.Model, Policy: plan.Evaluation[0]}
	if err := descriptor.Check(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	if err := definition.CheckEvaluationPolicy(descriptor.Policy); err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, test := range descriptor.Policy.Cases {
		if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
			return platform.ReleaseAsset{}, fmt.Errorf("evaluation cases do not fit the pinned function")
		}
	}
	body, err := json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetEvaluation, Name: plan.Name}, ContractVersion: 1, SourceVersion: b.Manifest().Version + ".evaluation-" + strconv.Itoa(version), Body: body})
}

func (b *Build) testPlanImage(raw []byte) (construction.TestPlan, platform.ReleaseAsset, error) {
	var plan construction.TestPlan
	if construction.ValueDecodeIntegration(raw, &plan) != nil || plan.ID == "" || plan.DefinitionVersion < 1 || plan.DefinitionVersion > 64 || len(plan.Versions) != plan.DefinitionVersion {
		return plan, platform.ReleaseAsset{}, fmt.Errorf("invalid evaluation set publication")
	}
	var asset platform.ReleaseAsset
	if construction.ValueDecodeIntegration([]byte(plan.Published), &asset) != nil {
		return plan, asset, fmt.Errorf("evaluation set has no immutable descriptor")
	}
	for i, raw := range plan.Versions {
		var retained platform.ReleaseAsset
		if construction.ValueDecodeIntegration([]byte(raw), &retained) != nil || b.checkTestPlanAsset(retained, plan.Name, i+1) != nil || retained.Ref.Kind != asset.Ref.Kind {
			return plan, asset, fmt.Errorf("evaluation set retained family differs")
		}
	}
	if b.checkTestPlanAsset(asset, plan.Name, plan.DefinitionVersion) != nil || plan.Versions[len(plan.Versions)-1] != plan.Published {
		return plan, asset, fmt.Errorf("evaluation set published pointer differs")
	}
	if asset.Ref.Kind != testPlanKind(plan) {
		return plan, asset, fmt.Errorf("test plan publication kind differs")
	}
	return plan, asset, nil
}

func (b *Build) checkTestPlanAsset(asset platform.ReleaseAsset, name string, version int) error {
	actual, err := b.testPlanAssetName(asset)
	if err != nil || actual != name || asset.Ref.App != construction.ID || asset.Ref.Name != name || asset.ContractVersion != 1 || asset.SourceVersion != b.Manifest().Version+testPlanVersionPrefix(asset.Ref.Kind)+strconv.Itoa(version) || platform.CheckAssetDependencies(asset) != nil {
		return fmt.Errorf("evaluation set publication differs from its descriptor")
	}
	return nil
}

func (b *Build) submitTestPlan(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Holds(construction.ID, construction.Builder) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		plan, ok := platform.Get[construction.TestPlan](c, s.GetTarget().GetId())
		if !ok || plan.Archived || plan.DefinitionVersion >= 64 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a saved evaluation plan with an available version")
		}
		if plan.Published != "" {
			if _, _, err := b.testPlanImage(platform.Raw(plan)); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Evaluation set could not be published: {why}", err.Error())
			}
		}
		asset, err := b.testPlanAsset(plan, plan.DefinitionVersion+1)
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

func (b *Build) testPlanAssets() ([]platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[construction.TestPlan](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return nil, err
	}
	out := []platform.ReleaseAsset{}
	for _, plan := range rows {
		if plan.Published == "" || plan.Archived {
			continue
		}
		_, asset, err := b.testPlanImage(platform.Raw(plan))
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (b *Build) testPlanPinned(ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[construction.TestPlan](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, plan := range rows {
		if plan.Published == "" {
			continue
		}
		if _, _, err := b.testPlanImage(platform.Raw(plan)); err != nil {
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

func (b *Build) testPlanDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	plan, found := platform.Get[construction.TestPlan](b.host.Automation(platform.Caller{}, construction.ID), id)
	if !found || plan.Archived || plan.DefinitionVersion >= 64 {
		err = fmt.Errorf("saved evaluation plan is unavailable")
		return
	}
	if plan.Published != "" {
		_, old, problem := b.testPlanImage(platform.Raw(plan))
		if problem != nil {
			err = problem
			return
		}
		prior, hadPrior = old.Ref, true
	}
	asset, problem := b.testPlanAsset(plan, plan.DefinitionVersion+1)
	if problem != nil {
		err = problem
		return
	}
	next = asset.Ref
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareTestPlanPublications(assets []platform.ReleaseAsset) ([]construction.ReleasePublication, error) {
	rows, err := readDefinitionInventory[construction.TestPlan](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return nil, err
	}
	out := []construction.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetEvaluation && asset.Ref.Kind != platform.AssetTestPlan {
			continue
		}
		name, err := b.testPlanAssetName(asset)
		if err != nil {
			return nil, err
		}
		ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+testPlanVersionPrefix(asset.Ref.Kind))
		version, err := strconv.Atoi(ordinal)
		if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal {
			return nil, fmt.Errorf("invalid evaluation set version")
		}
		if err := b.checkTestPlanAsset(asset, name, version); err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(plan construction.TestPlan) bool { return !plan.Archived && plan.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("evaluation set has no target plan")
		}
		plan := rows[i]
		if version <= plan.DefinitionVersion {
			prior, err := b.testPlanPinned(asset.Ref, asset.SourceVersion)
			if err != nil || !equalIntegrationAsset(prior, asset) {
				return nil, fmt.Errorf("evaluation set retained bytes differ")
			}
			continue
		}
		if version != plan.DefinitionVersion+1 {
			return nil, fmt.Errorf("evaluation set needs its retained predecessor")
		}
		frozen, err := b.testPlanDraft(asset)
		if err != nil {
			return nil, err
		}
		plan.Title, plan.Model, plan.Evaluation, plan.Function, plan.FunctionVersion = frozen.Title, frozen.Model, frozen.Evaluation, frozen.Function, frozen.FunctionVersion
		if asset.Ref.Kind == platform.AssetTestPlan {
			plan.Roots, plan.At, plan.Organization, plan.Samples, plan.Steps = frozen.Roots, frozen.At, frozen.Organization, frozen.Samples, frozen.Steps
			plan.Application, plan.CandidateID, plan.Object, plan.Process, plan.As, plan.Actors, plan.Sample = "", "", "", "", "", nil, ""
		}
		if _, err := b.testPlanPinned(asset.Ref, asset.SourceVersion); err == nil {
			return nil, fmt.Errorf("evaluation set version already exists")
		}
		plan.DefinitionVersion, plan.Published = version, string(platform.Raw(asset))
		plan.Versions = append(slices.Clone(plan.Versions), plan.Published)
		out = append(out, construction.ReleasePublication{Schema: SchemaEvaluationSetPublish, Image: platform.Raw(plan)})
	}
	return out, nil
}

func (b *Build) testPlanDraft(asset platform.ReleaseAsset) (construction.TestPlan, error) {
	if asset.Ref.Kind == platform.AssetTestPlan {
		descriptor, err := platform.ReadTestPlan(asset.Body)
		if err != nil {
			return construction.TestPlan{}, err
		}
		return construction.TestPlan{Name: descriptor.Name, Title: descriptor.Title, Roots: descriptor.Roots, At: descriptor.At, Organization: descriptor.Organization, Samples: descriptor.Samples, Steps: descriptor.Steps}, nil
	}
	descriptor, err := platform.ReadEvaluationSet(asset.Body)
	if err != nil {
		return construction.TestPlan{}, err
	}
	rows, err := b.functionInventory()
	if err != nil {
		return construction.TestPlan{}, err
	}
	functionID := descriptor.Function.Ref.Name
	for _, function := range rows {
		if !function.Archived && function.Name == descriptor.Function.Ref.Name {
			functionID = function.ID
			break
		}
	}
	if id := b.releaseDraftIDs[descriptor.Function.Ref]; id != "" {
		functionID = id
	}
	ordinal, ok := strings.CutPrefix(descriptor.Function.SourceVersion, b.Manifest().Version+".function-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 {
		return construction.TestPlan{}, fmt.Errorf("evaluation needs a supported function owner")
	}
	return construction.TestPlan{Name: descriptor.Name, Title: descriptor.Title, Function: platform.Ref[construction.Function](functionID), FunctionVersion: version, Model: descriptor.Model, At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Steps: []construction.TestStep{{Type: construction.FunctionCallType, ID: "evaluation", Action: construction.SchemaFunctionCall, Payload: "{}", Expect: "accepted"}}, Evaluation: []construction.EvaluationPolicy{descriptor.Policy}}, nil
}
