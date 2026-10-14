package buildapi

import (
	"platformserver/platform"
)

const FunctionCallType = "build.function-call"

const SchemaFunctionCall = FunctionCallType + ".start"

const SchemaFunctionAnswer = FunctionCallType + ".answer"

// FunctionRun keeps a suggestion separately from its business source. A
// person's ordinary business action remains the only way to adopt it.
type FunctionRun struct {
	platform.Record
	Applications   []platform.AssetBinding `json:"applications,omitempty" field:"readonly" type:"json"`
	Question       string                  `json:"question,omitempty" field:"readonly" type:"longtext"`
	History        []string                `json:"history,omitempty" field:"readonly"`
	Function       string                  `json:"function" field:"readonly"`
	App            string                  `json:"app,omitempty" field:"readonly"`
	Contract       platform.AIFunction     `json:"contract" type:"json" field:"readonly"`
	Version        int                     `json:"version" field:"readonly"`
	Member         string                  `json:"member" field:"readonly"`
	Source         string                  `json:"source" field:"readonly"`
	State          string                  `json:"state" field:"readonly" choices:"pending,ready,rejected"`
	Output         string                  `json:"output,omitempty" field:"readonly" type:"longtext" title:"Typed answer"`
	Code           string                  `json:"code,omitempty" field:"readonly" title:"Refusal code"`
	Reason         string                  `json:"reason,omitempty" field:"readonly" type:"longtext" title:"Refusal reason"`
	Definition     string                  `json:"definition" field:"readonly"`
	Dependencies   string                  `json:"dependencies" field:"readonly"`
	Model          string                  `json:"model" field:"readonly"`
	InputHash      string                  `json:"inputHash" field:"readonly" title:"Input hash"`
	Release        string                  `json:"release,omitempty" field:"readonly"`
	Metered        bool                    `json:"metered,omitempty" field:"readonly" title:"Model call measured"`
	TokensReported bool                    `json:"tokensReported,omitempty" field:"readonly" title:"Token counts reported"`
	InputTokens    int                     `json:"inputTokens,omitempty" field:"readonly" title:"Input tokens"`
	OutputTokens   int                     `json:"outputTokens,omitempty" field:"readonly" title:"Output tokens"`
	CostReported   bool                    `json:"costReported,omitempty" field:"readonly" title:"USD cost reported"`
	CostUSD        float64                 `json:"costUsd,omitempty" field:"readonly" title:"Reported USD cost"`
	LatencyMillis  int64                   `json:"latencyMillis,omitempty" field:"readonly" title:"Model latency in milliseconds"`
	ServedModel    string                  `json:"servedModel,omitempty" field:"readonly" title:"Served model"`
	Sources        []string                `json:"sources" field:"readonly"`
	Withheld       bool                    `json:"withheld,omitempty" field:"readonly" title:"Some sources cannot be read"`
}
