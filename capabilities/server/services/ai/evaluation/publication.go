package agents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
	"platformserver/services/devops/descriptor"
	functionauthoring "platformserver/services/functions/authoring"
)

func EvaluationPublication[O, P any](env descriptor.TestPlanEnvironment[functionauthoring.Function]) descriptor.EvaluationPublication[O, P, functionauthoring.Function] {
	return descriptor.EvaluationPublication[O, P, functionauthoring.Function]{
		Asset: func(plan descriptor.TestPlan[O, P, functionauthoring.Function], version int) (platform.ReleaseAsset, error) {
			function, ok := platform.Get[functionauthoring.Function](env.Caller(), string(plan.Function))
			if !ok || function.Archived || len(plan.Evaluation) != 1 {
				return platform.ReleaseAsset{}, fmt.Errorf("evaluation set needs its function and one saved policy")
			}
			definition, ordinal, ok := env.FunctionDefinition(function.Name, plan.FunctionVersion)
			if selected, found := env.JointDeclarations()[platform.AssetRef{App: env.App, Kind: platform.AssetFunction, Name: function.Name}]; found {
				var frozen functionauthoring.Function
				text, valid := strings.CutPrefix(selected.SourceVersion, env.SourceVersion()+".function-")
				version, err := strconv.Atoi(text)
				if valid && err == nil && version > 0 && (plan.FunctionVersion == 0 || plan.FunctionVersion == version) && env.Decode(selected.Body, &frozen) == nil {
					definition, ordinal, ok = frozen.Definition(), version, true
				}
			}
			if !ok || ordinal < 1 {
				return platform.ReleaseAsset{}, fmt.Errorf("evaluation function version is not published")
			}
			d := platform.EvaluationSetDescriptor{Name: plan.Name, Title: plan.Title, Function: platform.AssetBinding{Ref: platform.AssetRef{App: env.App, Kind: platform.AssetFunction, Name: function.Name}, SourceVersion: env.SourceVersion() + ".function-" + strconv.Itoa(ordinal)}, Model: plan.Model, Policy: plan.Evaluation[0]}
			if err := d.Check(); err != nil {
				return platform.ReleaseAsset{}, err
			}
			if err := definition.CheckEvaluationPolicy(d.Policy); err != nil {
				return platform.ReleaseAsset{}, err
			}
			for _, test := range d.Policy.Cases {
				if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
					return platform.ReleaseAsset{}, fmt.Errorf("evaluation cases do not fit the pinned function")
				}
			}
			body, err := json.Marshal(d)
			if err != nil {
				return platform.ReleaseAsset{}, err
			}
			return env.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: env.App, Kind: platform.AssetEvaluation, Name: plan.Name}, ContractVersion: 1, SourceVersion: env.SourceVersion() + ".evaluation-" + strconv.Itoa(version), Body: body})
		},
		Draft: func(asset platform.ReleaseAsset) (descriptor.TestPlan[O, P, functionauthoring.Function], error) {
			d, err := platform.ReadEvaluationSet(asset.Body)
			if err != nil {
				return descriptor.TestPlan[O, P, functionauthoring.Function]{}, err
			}
			rows, err := env.Functions()
			if err != nil {
				return descriptor.TestPlan[O, P, functionauthoring.Function]{}, err
			}
			functionID := d.Function.Ref.Name
			for _, function := range rows {
				if !function.Archived && function.Name == d.Function.Ref.Name {
					functionID = function.ID
					break
				}
			}
			if id := env.DraftID(d.Function.Ref); id != "" {
				functionID = id
			}
			ordinal, ok := strings.CutPrefix(d.Function.SourceVersion, env.SourceVersion()+".function-")
			version, err := strconv.Atoi(ordinal)
			if !ok || err != nil || version < 1 {
				return descriptor.TestPlan[O, P, functionauthoring.Function]{}, fmt.Errorf("evaluation needs a supported function owner")
			}
			return descriptor.TestPlan[O, P, functionauthoring.Function]{Variant: "evaluation-set", Name: d.Name, Title: d.Title, Function: platform.Ref[functionauthoring.Function](functionID), FunctionVersion: version, Model: d.Model, At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Steps: []platform.TestStep{{Type: functionauthoring.FunctionCallType, ID: "evaluation", Action: functionauthoring.SchemaFunctionCall, Payload: "{}", Expect: "accepted"}}, Evaluation: []platform.EvaluationPolicy{d.Policy}}, nil
		},
	}
}
