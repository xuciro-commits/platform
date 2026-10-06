package build

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const FunctionCallType = "build.function-call"
const SchemaFunctionCall = FunctionCallType + ".start"
const SchemaFunctionAnswer = FunctionCallType + ".answer"

// FunctionRun keeps a suggestion separately from its business source. A
// person's ordinary business action remains the only way to adopt it.
type FunctionRun struct {
	platform.Record
	Question       string              `json:"question,omitempty" field:"readonly" type:"longtext"`
	History        []string            `json:"history,omitempty" field:"readonly"`
	Function       string              `json:"function" field:"readonly"`
	App            string              `json:"app,omitempty" field:"readonly"`
	Contract       platform.AIFunction `json:"contract" type:"json" field:"readonly"`
	Version        int                 `json:"version" field:"readonly"`
	Member         string              `json:"member" field:"readonly"`
	Source         string              `json:"source" field:"readonly"`
	State          string              `json:"state" field:"readonly" choices:"pending,ready,rejected"`
	Output         string              `json:"output,omitempty" field:"readonly" type:"longtext" title:"Typed answer"`
	Code           string              `json:"code,omitempty" field:"readonly" title:"Refusal code"`
	Reason         string              `json:"reason,omitempty" field:"readonly" type:"longtext" title:"Refusal reason"`
	Definition     string              `json:"definition" field:"readonly"`
	Dependencies   string              `json:"dependencies" field:"readonly"`
	Model          string              `json:"model" field:"readonly"`
	InputHash      string              `json:"inputHash" field:"readonly" title:"Input hash"`
	Release        string              `json:"release,omitempty" field:"readonly"`
	Metered        bool                `json:"metered,omitempty" field:"readonly" title:"Model call measured"`
	TokensReported bool                `json:"tokensReported,omitempty" field:"readonly" title:"Token counts reported"`
	InputTokens    int                 `json:"inputTokens,omitempty" field:"readonly" title:"Input tokens"`
	OutputTokens   int                 `json:"outputTokens,omitempty" field:"readonly" title:"Output tokens"`
	CostReported   bool                `json:"costReported,omitempty" field:"readonly" title:"USD cost reported"`
	CostUSD        float64             `json:"costUsd,omitempty" field:"readonly" title:"Reported USD cost"`
	LatencyMillis  int64               `json:"latencyMillis,omitempty" field:"readonly" title:"Model latency in milliseconds"`
	ServedModel    string              `json:"servedModel,omitempty" field:"readonly" title:"Served model"`
	Sources        []string            `json:"sources" field:"readonly"`
	Withheld       bool                `json:"withheld,omitempty" field:"readonly" title:"Some sources cannot be read"`
}

func (b *Build) functionCallEntity() platform.Entity {
	return platform.Entity{Type: FunctionCallType, Title: "AI function call", Plural: "AI function calls", Model: FunctionRun{}, Display: "function",
		Description: "A retained typed suggestion or refusal; it never changes its source record.",
		Scope:       platform.Scope{Default: platform.ScopeOwn, Owner: "member", Levels: map[string]string{Builder: platform.ScopeTenant}},
		Derived:     []platform.Derivation{{From: "sources", Fields: []string{"output", "reason", "question"}}}, Withheld: "withheld"}
}

func functionCallActions(roles []string) []platform.Action {
	return []platform.Action{{Schema: SchemaFunctionCall, Target: FunctionCallType, New: true, Capability: "functions", Title: "Call AI function",
		Description: "Request a published function over a readable source; retain the suggestion for human review.",
		Roles:       append(slices.Clone(roles), platform.AnyMember), Automation: true,
		Payload: []platform.Field{{Name: "question", Type: "string", Description: "Bounded member question for a conversation function"}, {Name: "history", Type: "json", Description: "Up to eight prior completed calls in this conversation"}, {Name: "app", Type: "string", Description: "Registered function owner"}, {Name: "name", Type: "string", Required: true, Description: "Published function name"},
			{Name: "source", Type: "string", Required: true, Description: "Source record ID"},
			{Name: "version", Type: "integer", Description: "Published version; zero selects the installed version"},
			{Name: "release", Type: "string", Description: "Retained release for a native automation; empty keeps a development run"},
			{Name: "onBehalf", Type: "string", Description: "Retained member for a native automation"}}},
		{Schema: SchemaFunctionAnswer, Target: FunctionCallType, Capability: "functions", Title: "Record AI function answer",
			Description: "Keep the validated suggestion or refusal on its call record.", Automation: true, Payload: platform.FunctionAnswerFields()}}
}

func (*Build) AcceptedActionSchemas() []string {
	return []string{SchemaFunctionCall, SchemaFunctionAnswer, SchemaEvaluationStart, SchemaEvaluationAnswer, SchemaCodeCompile, SchemaCodeCompiled, SchemaConnectionChecked, SchemaWritebackAnswered}
}

func (b *Build) submitFunctionCall(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Staging() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "AI functions require an accepted decision")
		}
		id := s.GetTarget().GetId()
		run, exists := platform.Get[FunctionRun](c, id)
		if s.GetSchema().GetName() == SchemaFunctionCall {
			if exists {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			var request platform.FunctionRequest
			if json.Unmarshal(s.GetPayload(), &request) != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			request.Reply = SchemaFunctionAnswer
			return func(r *pb.ChangeRecord) {
				call, err := c.RequestFunction(r, request)
				if err != nil {
					return
				}
				member := c.ID
				if c.Automation {
					member = request.OnBehalf
				}
				definition, _, ok := b.host.Function(cmp.Or(request.App, ID), request.Name, call.Version)
				if !ok {
					return
				}
				c.Put(r, FunctionRun{Question: request.Question, History: slices.Clone(request.History), App: cmp.Or(request.App, ID), Contract: definition, Record: platform.Record{ID: id}, Function: request.Name, Version: call.Version, Member: member,
					Source: call.Source, State: "pending", Definition: call.Definition, Dependencies: call.Dependencies,
					Model: call.Model, InputHash: call.InputHash, Release: call.Release, Sources: call.Sources})
			}, nil
		}
		var answer platform.Answer
		if !exists || run.State != "pending" || json.Unmarshal(s.GetPayload(), &answer) != nil || answer.Call != id ||
			answer.Outcome != "accepted" && answer.Outcome != "refused" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		run.State, run.Code, run.Reason = "rejected", answer.Code, answer.Reason
		run.Metered, run.TokensReported = answer.Metered, answer.TokensReported
		run.InputTokens, run.OutputTokens = answer.InputTokens, answer.OutputTokens
		run.CostReported, run.CostUSD = answer.CostReported, answer.CostUSD
		run.LatencyMillis, run.ServedModel = answer.LatencyMillis, answer.ServedModel
		if answer.Outcome == "accepted" {
			if run.Contract.ValidateOutput([]byte(answer.Text)) != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			run.Output, run.State, run.Code, run.Reason = answer.Text, "ready", "", ""
		}
		return func(r *pb.ChangeRecord) { c.Put(r, run) }, nil
	})
}
