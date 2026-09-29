package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	EvaluationType         = "build.evaluation"
	SchemaEvaluationStart  = EvaluationType + ".start"
	SchemaEvaluationAnswer = EvaluationType + ".answer"
	EvaluationRepeats      = 3
)

// Evaluation is one retained observation of an immutable release candidate.
// Its samples are synthetic and builder-only; a new plan or configuration
// requires a new report rather than mutating this one.
type Evaluation struct {
	platform.Record
	Candidate        string              `json:"candidate" field:"readonly"`
	Function         string              `json:"function" field:"readonly"`
	Version          int                 `json:"version" field:"readonly"`
	Definition       string              `json:"definition" field:"readonly" type:"longtext"`
	DefinitionDigest string              `json:"definitionDigest" field:"readonly"`
	Plan             string              `json:"plan" field:"readonly"`
	PlanRevision     uint32              `json:"planRevision" field:"readonly"`
	PlanDigest       string              `json:"planDigest" field:"readonly"`
	Model            string              `json:"model" field:"readonly"`
	ModelConfig      string              `json:"modelConfig" field:"readonly"`
	Policy           []EvaluationPolicy  `json:"policy" field:"readonly"`
	State            string              `json:"state" field:"readonly" choices:"pending,passed,failed"`
	Quality          float64             `json:"quality" field:"readonly"`
	CostUSD          float64             `json:"costUsd" field:"readonly"`
	CostComplete     bool                `json:"costComplete" field:"readonly"`
	PeakLatency      int64               `json:"peakLatencyMillis" field:"readonly"`
	Attempts         []EvaluationAttempt `json:"attempts" field:"readonly"`
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

// EvaluationStart is formed by the host after it has checked the saved
// candidate, current plan revision and model configuration. Only the host's
// Build automation identity may submit it.
type EvaluationStart struct {
	Candidate        string              `json:"candidate"`
	Function         string              `json:"function"`
	Version          int                 `json:"version"`
	Definition       platform.AIFunction `json:"definition"`
	DefinitionDigest string              `json:"definitionDigest"`
	Plan             string              `json:"plan"`
	PlanRevision     uint32              `json:"planRevision"`
	PlanDigest       string              `json:"planDigest"`
	Model            string              `json:"model"`
	ModelConfig      string              `json:"modelConfig"`
	Policy           EvaluationPolicy    `json:"policy"`
}

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
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
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
			if !ok || !evalEqual(platform.Raw(installed), platform.Raw(start.Definition)) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The evaluated function version changed")
			}
			plan, ok := platform.Get[TestPlan](c, start.Plan)
			if !ok || plan.Revision != start.PlanRevision || plan.Function == "" || len(plan.Evaluation) != 1 ||
				!evalEqual(platform.Raw(plan.Evaluation[0]), platform.Raw(start.Policy)) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved evaluation plan changed")
			}
			run = Evaluation{Record: platform.Record{ID: id}, Candidate: start.Candidate, Function: start.Function,
				Version: start.Version, Definition: string(platform.Raw(start.Definition)), DefinitionDigest: start.DefinitionDigest,
				Plan: start.Plan, PlanRevision: start.PlanRevision, PlanDigest: start.PlanDigest,
				Model: start.Model, ModelConfig: start.ModelConfig, Policy: []EvaluationPolicy{start.Policy}, State: "pending", CostComplete: true}
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
					a.Matched = evalEqual([]byte(answer.Text), run.Policy[0].Cases[index/EvaluationRepeats].Expected)
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
