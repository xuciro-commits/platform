package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (*Build) evaluationEntity() platform.Entity {
	return platform.Entity{Type: EvaluationType, Title: "Function evaluation", Plural: "Function evaluations", Model: Evaluation{}, Display: "function",
		Description: "A retained candidate-bound report of repeated model calls over synthetic benchmark cases.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}}}
}

func evaluationActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaEvaluationStart, Target: EvaluationType, New: true, Capability: "functions", Title: "Start function evaluation",
			Description: "Run bounded synthetic cases against the candidate's pinned function and model.", Automation: true, Payload: []platform.Field{}},
		{Schema: SchemaEvaluationAnswer, Target: EvaluationType, Capability: "functions", Title: "Record function evaluation answer",
			Description: "Save one measured model answer in the candidate evaluation.", Automation: true, Payload: platform.FunctionAnswerFields()},
	}
}

func evalEqual(a, b []byte) bool {
	return (platform.EvaluationPolicy{}).Matches(a, b)
}

func (b *Build) submitEvaluation(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Automation || !c.Staging() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Function evaluations require the accepted model effect path")
		}
		id := s.GetTarget().GetId()
		run, exists := platform.Get[Evaluation](c, id)
		if s.GetSchema().GetName() == SchemaEvaluationStart {
			var start EvaluationStart
			if exists || json.Unmarshal(s.GetPayload(), &start) != nil || start.Candidate == "" || start.Plan == "" ||
				start.PlanRevision == 0 || start.PlanDigest == "" || start.Model == "" || start.ModelConfig == "" ||
				start.Version < 1 || start.Definition.Name != start.Function || start.Definition.Check() != nil ||
				len(start.Policy.Cases) < 1 || len(start.Policy.Cases) > 5 {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			installed, _, ok := b.FunctionDefinition(start.Function, start.Version)
			if start.Set == nil && (!ok || !evalEqual(platform.Raw(installed), platform.Raw(start.Definition))) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The evaluated function version changed")
			}
			plan, ok := platform.Get[TestPlan](c, start.Plan)
			if start.Definition.CheckEvaluationPolicy(start.Policy) != nil || start.Set == nil && (!ok || plan.Revision != start.PlanRevision || plan.Function == "" || len(plan.Evaluation) != 1 || !evalEqual(platform.Raw(plan.Evaluation[0]), platform.Raw(start.Policy))) || start.Set != nil && (start.Set.Ref.App != ID || start.Set.Ref.Kind != platform.AssetEvaluation || start.Set.Ref.Check() != nil || start.Set.SourceVersion == "") {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved evaluation plan changed")
			}
			run = Evaluation{Set: start.Set, Record: platform.Record{ID: id}, Candidate: start.Candidate, Function: start.Function,
				Version: start.Version, Definition: string(platform.Raw(start.Definition)), DefinitionDigest: start.DefinitionDigest,
				Plan: start.Plan, PlanRevision: start.PlanRevision, PlanDigest: start.PlanDigest,
				Model: start.Model, ModelConfig: start.ModelConfig, Policy: []platform.EvaluationPolicy{start.Policy}, State: "pending", CostComplete: true}
			for i, test := range start.Policy.Cases {
				if start.Definition.ValidateOutput(test.Expected) != nil || len(test.Input) > start.Definition.MaxInputBytes {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
				for repeat := 1; repeat <= EvaluationRepeats; repeat++ {
					run.Attempts = append(run.Attempts, EvaluationAttempt{Case: test.Name, Repeat: repeat,
						Call: fmt.Sprintf("%s:%d:%d", id, i+1, repeat)})
				}
			}
			return func(r *pb.ChangeRecord) {
				c.Put(r, run)
				for i, attempt := range run.Attempts {
					c.Request(r, platform.Request{Model: start.Model, Target: attempt.Call, Reply: SchemaEvaluationAnswer,
						Evaluation: true, EvaluationConfig: start.ModelConfig, EvaluationFunction: &start.Definition,
						Payload: platform.Prompt{System: start.Definition.SystemPrompt(), User: string(start.Policy.Cases[i/EvaluationRepeats].Input),
							MaxTokens: start.Definition.MaxTokens}})
				}
			}, nil
		}
		var answer platform.Answer
		if !exists || run.State != "pending" || json.Unmarshal(s.GetPayload(), &answer) != nil ||
			(answer.Outcome != "accepted" && answer.Outcome != "refused") || answer.Call == "" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		index := -1
		for i := range run.Attempts {
			if run.Attempts[i].Call == answer.Call && run.Attempts[i].Outcome == "" {
				index = i
				break
			}
		}
		if index < 0 || answer.InputTokens < 0 || answer.OutputTokens < 0 || answer.CostUSD < 0 || answer.LatencyMillis < 0 {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		a := &run.Attempts[index]
		var definition platform.AIFunction
		if json.Unmarshal([]byte(run.Definition), &definition) != nil || len(run.Policy) != 1 {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		a.Outcome, a.Reason = answer.Outcome, cmp.Or(answer.Reason, answer.Code)
		if len(a.Reason) > 1024 {
			a.Reason = a.Reason[:1024]
		}
		a.Metered, a.TokensReported, a.InputTokens, a.OutputTokens = answer.Metered, answer.TokensReported, answer.InputTokens, answer.OutputTokens
		a.CostReported, a.CostUSD, a.LatencyMillis, a.ServedModel = answer.CostReported, answer.CostUSD, answer.LatencyMillis, answer.ServedModel
		if answer.Outcome == "accepted" {
			if len(answer.Text) > definition.MaxOutputBytes {
				a.Reason = "The model answer exceeded the function byte budget"
			} else {
				a.Answer = answer.Text
				if definition.ValidateOutput([]byte(answer.Text)) == nil {
					a.Matched = run.Policy[0].Matches([]byte(answer.Text), run.Policy[0].Cases[index/EvaluationRepeats].Expected)
				} else {
					a.Reason = "The model answer failed the function output schema"
				}
			}
		}
		complete, valid, matched := true, true, 0
		run.CostUSD, run.PeakLatency, run.CostComplete = 0, 0, true
		for _, attempt := range run.Attempts {
			complete = complete && attempt.Outcome != ""
			valid = valid && attempt.Outcome == "accepted" && attempt.Reason == ""
			if attempt.Matched {
				matched++
			}
			run.CostUSD += attempt.CostUSD
			if attempt.LatencyMillis > run.PeakLatency {
				run.PeakLatency = attempt.LatencyMillis
			}
			run.CostComplete = run.CostComplete && attempt.Metered && attempt.CostReported
		}
		if complete {
			run.Quality = float64(matched) / float64(len(run.Attempts))
			run.State = "failed"
			if valid && run.Quality >= run.Policy[0].MinQuality && run.CostComplete && run.CostUSD <= run.Policy[0].MaxCostUSD &&
				run.PeakLatency <= run.Policy[0].MaxLatencyMillis {
				run.State = "passed"
			}
		}
		return func(r *pb.ChangeRecord) { c.Put(r, run) }, nil
	})
}

const SchemaEvaluationSetPublish = TestPlanType + ".publish"

func (b *Build) testPlanAsset(plan TestPlan, version int) (platform.ReleaseAsset, error) {
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
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetTestPlan, Name: plan.Name}, ContractVersion: 1, SourceVersion: b.Manifest().Version + ".tests-" + strconv.Itoa(version), Body: body})
}

func testPlanKind(plan TestPlan) platform.AssetKind {
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

func (b *Build) evaluationSetAsset(plan TestPlan, version int) (platform.ReleaseAsset, error) {
	c := b.host.Automation(platform.Caller{}, ID)
	function, ok := platform.Get[Function](c, string(plan.Function))
	if !ok || function.Archived || len(plan.Evaluation) != 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("evaluation set needs its function and one saved policy")
	}
	definition, ordinal, ok := b.FunctionDefinition(function.Name, plan.FunctionVersion)
	if selected, found := b.jointDeclarations[platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: function.Name}]; found {
		var frozen Function
		text, valid := strings.CutPrefix(selected.SourceVersion, b.Manifest().Version+".function-")
		version, err := strconv.Atoi(text)
		if valid && err == nil && version > 0 && (plan.FunctionVersion == 0 || plan.FunctionVersion == version) && ValueDecodeIntegration(selected.Body, &frozen) == nil {
			definition, ordinal, ok = frozen.Definition(), version, true
		}
	}
	if !ok || ordinal < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("evaluation function version is not published")
	}
	descriptor := platform.EvaluationSetDescriptor{Name: plan.Name, Title: plan.Title, Function: platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: function.Name}, SourceVersion: b.Manifest().Version + ".function-" + strconv.Itoa(ordinal)}, Model: plan.Model, Policy: plan.Evaluation[0]}
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
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetEvaluation, Name: plan.Name}, ContractVersion: 1, SourceVersion: b.Manifest().Version + ".evaluation-" + strconv.Itoa(version), Body: body})
}

func (b *Build) testPlanImage(raw []byte) (TestPlan, platform.ReleaseAsset, error) {
	var plan TestPlan
	if ValueDecodeIntegration(raw, &plan) != nil || plan.ID == "" || plan.DefinitionVersion < 1 || plan.DefinitionVersion > 64 || len(plan.Versions) != plan.DefinitionVersion {
		return plan, platform.ReleaseAsset{}, fmt.Errorf("invalid evaluation set publication")
	}
	var asset platform.ReleaseAsset
	if ValueDecodeIntegration([]byte(plan.Published), &asset) != nil {
		return plan, asset, fmt.Errorf("evaluation set has no immutable descriptor")
	}
	for i, raw := range plan.Versions {
		var retained platform.ReleaseAsset
		if ValueDecodeIntegration([]byte(raw), &retained) != nil || b.checkTestPlanAsset(retained, plan.Name, i+1) != nil || retained.Ref.Kind != asset.Ref.Kind {
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
	if err != nil || actual != name || asset.Ref.App != ID || asset.Ref.Name != name || asset.ContractVersion != 1 || asset.SourceVersion != b.Manifest().Version+testPlanVersionPrefix(asset.Ref.Kind)+strconv.Itoa(version) || platform.CheckAssetDependencies(asset) != nil {
		return fmt.Errorf("evaluation set publication differs from its descriptor")
	}
	return nil
}

func (b *Build) submitTestPlan(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Holds(ID, Builder) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		plan, ok := platform.Get[TestPlan](c, s.GetTarget().GetId())
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
	rows, err := readDefinitionInventory[TestPlan](b.host.Automation(platform.Caller{}, ID))
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
	rows, err := readDefinitionInventory[TestPlan](b.host.Automation(platform.Caller{}, ID))
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
	plan, found := platform.Get[TestPlan](b.host.Automation(platform.Caller{}, ID), id)
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

func (b *Build) prepareTestPlanPublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := readDefinitionInventory[TestPlan](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetEvaluation && asset.Ref.Kind != platform.AssetTestPlan {
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
		i := slices.IndexFunc(rows, func(plan TestPlan) bool { return !plan.Archived && plan.Name == name })
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
		out = append(out, platform.ReleasePublication{Schema: SchemaEvaluationSetPublish, Image: platform.Raw(plan)})
	}
	return out, nil
}

func (b *Build) testPlanDraft(asset platform.ReleaseAsset) (TestPlan, error) {
	if asset.Ref.Kind == platform.AssetTestPlan {
		descriptor, err := platform.ReadTestPlan(asset.Body)
		if err != nil {
			return TestPlan{}, err
		}
		return TestPlan{Name: descriptor.Name, Title: descriptor.Title, Roots: descriptor.Roots, At: descriptor.At, Organization: descriptor.Organization, Samples: descriptor.Samples, Steps: descriptor.Steps}, nil
	}
	descriptor, err := platform.ReadEvaluationSet(asset.Body)
	if err != nil {
		return TestPlan{}, err
	}
	rows, err := functionKind.inventory(b)
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
	if id := b.releaseDraftIDs[descriptor.Function.Ref]; id != "" {
		functionID = id
	}
	ordinal, ok := strings.CutPrefix(descriptor.Function.SourceVersion, b.Manifest().Version+".function-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 {
		return TestPlan{}, fmt.Errorf("evaluation needs a supported function owner")
	}
	return TestPlan{Name: descriptor.Name, Title: descriptor.Title, Function: platform.Ref[Function](functionID), FunctionVersion: version, Model: descriptor.Model, At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Steps: []platform.TestStep{{Type: FunctionCallType, ID: "evaluation", Action: SchemaFunctionCall, Payload: "{}", Expect: "accepted"}}, Evaluation: []platform.EvaluationPolicy{descriptor.Policy}}, nil
}

const (
	EvaluationType         = "build.evaluation"
	SchemaEvaluationStart  = EvaluationType + ".start"
	SchemaEvaluationAnswer = EvaluationType + ".answer"
	EvaluationRepeats      = 3
)

type Evaluation struct {
	platform.Record
	Set              *platform.AssetBinding      `json:"evaluationSet,omitempty" field:"readonly" type:"json"`
	Candidate        string                      `json:"candidate" field:"readonly"`
	Function         string                      `json:"function" field:"readonly"`
	Version          int                         `json:"version" field:"readonly"`
	Definition       string                      `json:"definition" field:"readonly" type:"longtext"`
	DefinitionDigest string                      `json:"definitionDigest" field:"readonly"`
	Plan             string                      `json:"plan" field:"readonly"`
	PlanRevision     uint32                      `json:"planRevision" field:"readonly"`
	PlanDigest       string                      `json:"planDigest" field:"readonly"`
	Model            string                      `json:"model" field:"readonly"`
	ModelConfig      string                      `json:"modelConfig" field:"readonly"`
	Policy           []platform.EvaluationPolicy `json:"policy" field:"readonly"`
	State            string                      `json:"state" field:"readonly" choices:"pending,passed,failed"`
	Quality          float64                     `json:"quality" field:"readonly"`
	CostUSD          float64                     `json:"costUsd" field:"readonly"`
	CostComplete     bool                        `json:"costComplete" field:"readonly"`
	PeakLatency      int64                       `json:"peakLatencyMillis" field:"readonly"`
	Attempts         []EvaluationAttempt         `json:"attempts" field:"readonly"`
}

type EvaluationAttempt struct {
	Case           string  `json:"case"`
	Repeat         int     `json:"repeat"`
	Call           string  `json:"call"`
	Outcome        string  `json:"outcome,omitempty"`
	Answer         string  `json:"answer,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	Matched        bool    `json:"matched,omitempty"`
	Metered        bool    `json:"metered,omitempty"`
	TokensReported bool    `json:"tokensReported,omitempty"`
	InputTokens    int     `json:"inputTokens,omitempty"`
	OutputTokens   int     `json:"outputTokens,omitempty"`
	CostReported   bool    `json:"costReported,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	LatencyMillis  int64   `json:"latencyMillis,omitempty"`
	ServedModel    string  `json:"servedModel,omitempty"`
}

type EvaluationStart struct {
	Set              *platform.AssetBinding    `json:"evaluationSet,omitempty"`
	Candidate        string                    `json:"candidate"`
	Function         string                    `json:"function"`
	Version          int                       `json:"version"`
	Definition       platform.AIFunction       `json:"definition"`
	DefinitionDigest string                    `json:"definitionDigest"`
	Plan             string                    `json:"plan"`
	PlanRevision     uint32                    `json:"planRevision"`
	PlanDigest       string                    `json:"planDigest"`
	Model            string                    `json:"model"`
	ModelConfig      string                    `json:"modelConfig"`
	Policy           platform.EvaluationPolicy `json:"policy"`
}
