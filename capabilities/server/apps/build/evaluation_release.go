package build

import (
	"encoding/json"
	"fmt"
	buildapi "platformserver/internal/host/buildapi"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaEvaluationSetPublish = TestPlanType + ".publish"

func (b *Build) evaluationSetAsset(plan TestPlan, version int) (platform.ReleaseAsset, error) {
	c := b.host.Automation(platform.Caller{}, ID)
	function, ok := platform.Get[Function](c, string(plan.Function))
	if !ok || function.Archived || len(plan.Evaluation) != 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("evaluation set needs its function and one saved policy")
	}
	definition, ordinal, ok := b.FunctionDefinition(function.Name, plan.FunctionVersion)
	if !ok || ordinal < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("evaluation function version is not published")
	}
	descriptor := platform.EvaluationSetDescriptor{Name: plan.Name, Title: plan.Title, Function: platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: function.Name}, SourceVersion: b.Manifest().Version + ".function-" + strconv.Itoa(ordinal)}, Model: plan.Model, Policy: plan.Evaluation[0]}
	if err := descriptor.Check(); err != nil {
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
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetEvaluation, Name: plan.Name}, ContractVersion: 1, SourceVersion: b.Manifest().Version + ".evaluation-" + strconv.Itoa(version), Body: body})
}

func (b *Build) evaluationSetImage(raw []byte) (TestPlan, platform.ReleaseAsset, error) {
	var plan TestPlan
	if buildapi.ValueDecodeIntegration(raw, &plan) != nil || plan.ID == "" || plan.DefinitionVersion < 1 || plan.DefinitionVersion > 64 || len(plan.Versions) != plan.DefinitionVersion {
		return plan, platform.ReleaseAsset{}, fmt.Errorf("invalid evaluation set publication")
	}
	var asset platform.ReleaseAsset
	if buildapi.ValueDecodeIntegration([]byte(plan.Published), &asset) != nil {
		return plan, asset, fmt.Errorf("evaluation set has no immutable descriptor")
	}
	for i, raw := range plan.Versions {
		var retained platform.ReleaseAsset
		if buildapi.ValueDecodeIntegration([]byte(raw), &retained) != nil || b.checkEvaluationAsset(retained, plan.Name, i+1) != nil {
			return plan, asset, fmt.Errorf("evaluation set retained family differs")
		}
	}
	if b.checkEvaluationAsset(asset, plan.Name, plan.DefinitionVersion) != nil || plan.Versions[len(plan.Versions)-1] != plan.Published {
		return plan, asset, fmt.Errorf("evaluation set published pointer differs")
	}
	return plan, asset, nil
}

func (b *Build) checkEvaluationAsset(asset platform.ReleaseAsset, name string, version int) error {
	descriptor, err := platform.ReadEvaluationSet(asset.Body)
	if err != nil || descriptor.Name != name || asset.Ref != (platform.AssetRef{App: ID, Kind: platform.AssetEvaluation, Name: name}) || asset.ContractVersion != 1 || asset.SourceVersion != b.Manifest().Version+".evaluation-"+strconv.Itoa(version) || platform.CheckAssetDependencies(asset) != nil {
		return fmt.Errorf("evaluation set publication differs from its descriptor")
	}
	return nil
}

func (b *Build) submitEvaluationSet(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Holds(ID, Builder) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		plan, ok := platform.Get[TestPlan](c, s.GetTarget().GetId())
		if !ok || plan.Archived || plan.DefinitionVersion >= 64 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a saved evaluation plan with an available version")
		}
		if plan.Published != "" {
			if _, _, err := b.evaluationSetImage(platform.Raw(plan)); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Evaluation set could not be published: {why}", err.Error())
			}
		}
		asset, err := b.evaluationSetAsset(plan, plan.DefinitionVersion+1)
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

func (b *Build) evaluationAssets() ([]platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[TestPlan](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	out := []platform.ReleaseAsset{}
	for _, plan := range rows {
		if plan.Published == "" || plan.Archived {
			continue
		}
		_, asset, err := b.evaluationSetImage(platform.Raw(plan))
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (b *Build) evaluationSetPinned(ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[TestPlan](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, plan := range rows {
		if plan.Published == "" {
			continue
		}
		if _, _, err := b.evaluationSetImage(platform.Raw(plan)); err != nil {
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

func (b *Build) evaluationDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	plan, found := platform.Get[TestPlan](b.host.Automation(platform.Caller{}, ID), id)
	if !found || plan.Archived || plan.DefinitionVersion >= 64 {
		err = fmt.Errorf("saved evaluation plan is unavailable")
		return
	}
	if plan.Published != "" {
		_, old, problem := b.evaluationSetImage(platform.Raw(plan))
		if problem != nil {
			err = problem
			return
		}
		prior, hadPrior = old.Ref, true
	}
	asset, problem := b.evaluationSetAsset(plan, plan.DefinitionVersion+1)
	if problem != nil {
		err = problem
		return
	}
	next = asset.Ref
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareEvaluationPublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	rows, err := readDefinitionInventory[TestPlan](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	out := []ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetEvaluation {
			continue
		}
		descriptor, err := platform.ReadEvaluationSet(asset.Body)
		if err != nil {
			return nil, err
		}
		ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".evaluation-")
		version, err := strconv.Atoi(ordinal)
		if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal {
			return nil, fmt.Errorf("invalid evaluation set version")
		}
		if err := b.checkEvaluationAsset(asset, descriptor.Name, version); err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(plan TestPlan) bool { return !plan.Archived && plan.Name == descriptor.Name })
		if i < 0 {
			return nil, fmt.Errorf("evaluation set has no target plan")
		}
		plan := rows[i]
		if version <= plan.DefinitionVersion {
			prior, err := b.evaluationSetPinned(asset.Ref, asset.SourceVersion)
			if err != nil || !equalIntegrationAsset(prior, asset) {
				return nil, fmt.Errorf("evaluation set retained bytes differ")
			}
			continue
		}
		if version != plan.DefinitionVersion+1 {
			return nil, fmt.Errorf("evaluation set needs its retained predecessor")
		}
		frozen, err := b.evaluationSetDraft(asset)
		if err != nil {
			return nil, err
		}
		plan.Title, plan.Model, plan.Evaluation, plan.Function, plan.FunctionVersion = frozen.Title, frozen.Model, frozen.Evaluation, frozen.Function, frozen.FunctionVersion
		if _, err := b.evaluationSetPinned(asset.Ref, asset.SourceVersion); err == nil {
			return nil, fmt.Errorf("evaluation set version already exists")
		}
		plan.DefinitionVersion, plan.Published = version, string(platform.Raw(asset))
		plan.Versions = append(slices.Clone(plan.Versions), plan.Published)
		out = append(out, ReleasePublication{Schema: SchemaEvaluationSetPublish, Image: platform.Raw(plan)})
	}
	return out, nil
}

func (b *Build) evaluationSetDraft(asset platform.ReleaseAsset) (TestPlan, error) {
	descriptor, err := platform.ReadEvaluationSet(asset.Body)
	if err != nil {
		return TestPlan{}, err
	}
	rows, err := b.functionInventory()
	if err != nil {
		return TestPlan{}, err
	}
	functionID := descriptor.Function.Ref.Name
	for _, function := range rows {
		if !function.Archived && function.Name == descriptor.Function.Ref.Name {
			functionID = function.ID
			break
		}
	}
	ordinal, ok := strings.CutPrefix(descriptor.Function.SourceVersion, b.Manifest().Version+".function-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 {
		return TestPlan{}, fmt.Errorf("evaluation needs a supported function owner")
	}
	return TestPlan{Name: descriptor.Name, Title: descriptor.Title, Function: platform.Ref[Function](functionID), FunctionVersion: version, Model: descriptor.Model, At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Steps: []TestStep{{Type: FunctionCallType, ID: "evaluation", Action: SchemaFunctionCall, Payload: "{}", Expect: "accepted"}}, Evaluation: []EvaluationPolicy{descriptor.Policy}}, nil
}
