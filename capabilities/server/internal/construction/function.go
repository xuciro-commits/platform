package construction

import (
	"platformserver/platform"
)

const FunctionType = "build.function"

// Function owns one bounded declaration and its retained publications. Draft
// edits never replace what callers use until the publication result commits.
type Function struct {
	platform.Record
	Proposal       *platform.ProposalPolicy `json:"proposal,omitempty" type:"json" field:"aside" title:"Business proposal review"`
	Conversation   bool                     `json:"conversation,omitempty" field:"aside" title:"Conversation input"`
	Name           string                   `json:"name" field:"required,search"`
	Title          string                   `json:"title" field:"required,search"`
	Description    string                   `json:"description" field:"required" type:"longtext"`
	Object         string                   `json:"object" field:"required" title:"Source object"`
	Fields         []string                 `json:"fields" field:"aside" title:"Input fields"`
	Instructions   string                   `json:"instructions" field:"required" type:"longtext"`
	Output         []platform.Field         `json:"output" field:"aside" title:"Output fields"`
	Model          string                   `json:"model,omitempty"`
	ModelOptions   platform.ModelOptions    `json:"modelOptions,omitzero" type:"json"`
	MaxInputBytes  int                      `json:"maxInputBytes" field:"required" title:"Maximum input bytes"`
	MaxOutputBytes int                      `json:"maxOutputBytes" field:"required" title:"Maximum output bytes"`
	MaxTokens      int                      `json:"maxTokens" field:"required" title:"Maximum tokens"`
	Roles          []string                 `json:"roles" field:"aside" title:"Callable by"`
	State          string                   `json:"state" field:"readonly" choices:"draft,published"`
	Version        int                      `json:"version,omitempty" field:"readonly"`
	Published      string                   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions       []string                 `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

// definition projects the stored fields into the shared runtime declaration;
// validation and execution remain owned by platform.AIFunction.
func (f Function) Definition() platform.AIFunction {
	return platform.AIFunction{Proposal: f.Proposal, Conversation: f.Conversation, Name: f.Name, Title: f.Title, Description: f.Description, Object: f.Object,
		Fields: f.Fields, Instructions: f.Instructions, Output: f.Output, Model: f.Model,
		MaxInputBytes: f.MaxInputBytes, MaxOutputBytes: f.MaxOutputBytes, MaxTokens: f.MaxTokens, ModelOptions: f.ModelOptions, Roles: f.Roles}
}
