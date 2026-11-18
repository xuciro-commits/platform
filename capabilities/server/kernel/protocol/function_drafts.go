package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
	"time"
)

const (
	FunctionType         = "build.function"
	SchemaFunction       = FunctionType + ".publish"
	FunctionCallType     = "build.function-call"
	SchemaFunctionCall   = FunctionCallType + ".start"
	SchemaFunctionAnswer = FunctionCallType + ".answer"
	SchemaProposalReview = FunctionCallType + ".review"
)

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
	OutputMode     string                   `json:"outputMode,omitempty" field:"aside" choices:"fields,edit-proposal" title:"Output mode"`
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

func (f Function) Definition() platform.AIFunction {
	return platform.AIFunction{Proposal: f.Proposal, Conversation: f.Conversation, Name: f.Name, Title: f.Title, Description: f.Description, Object: f.Object,
		Fields: f.Fields, Instructions: f.Instructions, Output: f.Output, OutputMode: f.OutputMode, Model: f.Model,
		MaxInputBytes: f.MaxInputBytes, MaxOutputBytes: f.MaxOutputBytes, MaxTokens: f.MaxTokens, ModelOptions: f.ModelOptions, Roles: f.Roles}
}

type FunctionRun struct {
	platform.Record
	TrainingAuthorization string                     `json:"trainingAuthorization,omitempty" field:"readonly"`
	TrainingSources       map[string]uint32          `json:"trainingSources,omitempty" field:"readonly" type:"json"`
	SourceValues          map[string]json.RawMessage `json:"sourceValues,omitempty" field:"readonly" type:"json" title:"Accepted AI inputs"`
	SourceRevision        uint32                     `json:"sourceRevision,omitempty" field:"readonly" title:"Source revision"`
	Decision              string                     `json:"decision,omitempty" field:"readonly" choices:"pending,adopted,rejected,expired" title:"Proposal decision"`
	ExpiresAt             time.Time                  `json:"expiresAt,omitzero" field:"readonly" title:"Proposal expires at"`
	Reviewers             []string                   `json:"reviewers,omitempty" field:"readonly" title:"Proposal reviewers"`
	DecidedBy             string                     `json:"decidedBy,omitempty" field:"readonly" title:"Decided by"`
	DecidedAt             time.Time                  `json:"decidedAt,omitzero" field:"readonly" title:"Decided at"`
	AdoptedAction         string                     `json:"adoptedAction,omitempty" field:"readonly" title:"Adopted action"`
	AdoptedChange         string                     `json:"adoptedChange,omitempty" field:"readonly" title:"Adoption change"`
	Amendments            json.RawMessage            `json:"amendments,omitempty" field:"readonly" type:"json" title:"Human amendments"`
	Applications          []platform.AssetBinding    `json:"applications,omitempty" field:"readonly" type:"json"`
	Question              string                     `json:"question,omitempty" field:"readonly" type:"longtext"`
	History               []string                   `json:"history,omitempty" field:"readonly"`
	Function              string                     `json:"function" field:"readonly"`
	App                   string                     `json:"app,omitempty" field:"readonly"`
	Contract              platform.AIFunction        `json:"contract" type:"json" field:"readonly"`
	Version               int                        `json:"version" field:"readonly"`
	Member                string                     `json:"member" field:"readonly"`
	Source                string                     `json:"source" field:"readonly"`
	State                 string                     `json:"state" field:"readonly" choices:"pending,ready,rejected"`
	Output                string                     `json:"output,omitempty" field:"readonly" type:"longtext" title:"Typed answer"`
	Code                  string                     `json:"code,omitempty" field:"readonly" title:"Refusal code"`
	Reason                string                     `json:"reason,omitempty" field:"readonly" type:"longtext" title:"Refusal reason"`
	Definition            string                     `json:"definition" field:"readonly"`
	Dependencies          string                     `json:"dependencies" field:"readonly"`
	Model                 string                     `json:"model" field:"readonly"`
	InputHash             string                     `json:"inputHash" field:"readonly" title:"Input hash"`
	Release               string                     `json:"release,omitempty" field:"readonly"`
	Metered               bool                       `json:"metered,omitempty" field:"readonly" title:"Model call measured"`
	TokensReported        bool                       `json:"tokensReported,omitempty" field:"readonly" title:"Token counts reported"`
	InputTokens           int                        `json:"inputTokens,omitempty" field:"readonly" title:"Input tokens"`
	OutputTokens          int                        `json:"outputTokens,omitempty" field:"readonly" title:"Output tokens"`
	CostReported          bool                       `json:"costReported,omitempty" field:"readonly" title:"USD cost reported"`
	CostUSD               float64                    `json:"costUsd,omitempty" field:"readonly" title:"Reported USD cost"`
	LatencyMillis         int64                      `json:"latencyMillis,omitempty" field:"readonly" title:"Model latency in milliseconds"`
	ServedModel           string                     `json:"servedModel,omitempty" field:"readonly" title:"Served model"`
	Sources               []string                   `json:"sources" field:"readonly"`
	Withheld              bool                       `json:"withheld,omitempty" field:"readonly" title:"Some sources cannot be read"`
}

const CodeType = "build.code"

const SchemaCodeCompile = CodeType + ".compile"
const SchemaCodePublish = CodeType + ".publish"
const SchemaCodeCompiled = CodeType + ".compiled"
const SchemaCodeAttach = CodeType + ".attach"

type Code struct {
	platform.Record
	Name          string                   `json:"name" field:"required,search" source:"record"`
	Title         string                   `json:"title" field:"required,search" source:"record"`
	Description   string                   `json:"description" type:"longtext" source:"record"`
	ABI           string                   `json:"abi,omitempty" field:"aside" title:"Compute ABI" source:"record"`
	BuiltABI      string                   `json:"builtABI,omitempty" field:"readonly,aside" title:"Built ABI" source:"record"`
	Language      string                   `json:"language" field:"required" choices:"go,tinygo"`
	Source        string                   `json:"source" field:"required" type:"longtext" source:"record"`
	Input         platform.ValueSchema     `json:"input" field:"aside" type:"json"`
	Output        platform.ValueSchema     `json:"output" field:"aside" type:"json"`
	Roles         []string                 `json:"roles" field:"aside"`
	Limits        platform.OperationLimits `json:"limits" field:"aside" type:"json"`
	State         string                   `json:"state" field:"readonly" choices:"draft,compiling,compiled,failed,published"`
	Call          string                   `json:"call,omitempty" field:"readonly" source:"record"`
	Module        string                   `json:"module,omitempty" field:"readonly" source:"record"`
	SourceHash    string                   `json:"sourceHash,omitempty" field:"readonly" source:"record"`
	BuildHash     string                   `json:"buildHash,omitempty" field:"readonly" source:"record"`
	BuiltSource   string                   `json:"builtSource,omitempty" field:"readonly" type:"longtext" source:"record"`
	BuiltLanguage string                   `json:"builtLanguage,omitempty" field:"readonly" source:"record"`
	BuiltInput    platform.ValueSchema     `json:"builtInput" field:"readonly,aside" type:"json"`
	BuiltOutput   platform.ValueSchema     `json:"builtOutput" field:"readonly,aside" type:"json"`
	Toolchain     string                   `json:"toolchain,omitempty" field:"readonly"`
	Diagnostics   string                   `json:"diagnostics,omitempty" field:"readonly" type:"longtext"`
	Version       int                      `json:"version,omitempty" field:"readonly"`
	Published     string                   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions      []string                 `json:"versions,omitempty" field:"readonly"`
}

func (c Code) Definition() platform.Operation {
	abi := c.ABI
	if abi == "" {
		abi = platform.WasmCommandABI
	}
	return platform.Operation{Name: c.Name, Title: c.Title, Description: c.Description, Input: c.Input, Output: c.Output, Roles: c.Roles, Binding: platform.OperationBinding{Kind: "wasm", Module: c.Module, ABI: abi}, Limits: c.Limits}
}
func (c Code) SourceDigest() string {
	h := sha256.Sum256([]byte(c.Source))
	return hex.EncodeToString(h[:])
}

func (c Code) BuildDigest() string {
	raw, _ := json.Marshal(platform.CodeBuildRequest{ABI: c.ABI, Language: c.Language, Source: c.Source, Input: c.Input, Output: c.Output})
	value, err := platform.DecodeValue(raw, 1<<20)
	if err != nil {
		return ""
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:])
}
func CheckCodeBuildable(code *Code) *platform.Error {
	if code.Language != "go" && code.Language != "tinygo" || code.Input.Check() != nil || code.Output.Check() != nil || len(code.Source) == 0 || len(code.Source) > 256<<10 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Code requires Go/TinyGo source and bounded input/output schemas")
	}
	declaration := code.Definition()
	declaration.Binding.Module = strings.Repeat("0", 64)
	if err := declaration.Check(); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

type CodeArtifact struct {
	Module     string `json:"module"`
	SourceHash string `json:"sourceHash"`
	BuildHash  string `json:"buildHash"`
	Toolchain  string `json:"toolchain"`
}

func (a CodeArtifact) Matches(spec json.RawMessage) bool {
	_, expected, err := CodeBuildOf(spec)
	return err == nil && a.Module != "" && a.Toolchain != "" && a.SourceHash == expected.SourceHash && a.BuildHash == expected.BuildHash
}

func CodeBuildOf(spec json.RawMessage) (platform.CodeBuildRequest, CodeArtifact, *platform.Error) {
	var code Code
	if json.Unmarshal(spec, &code) != nil {
		return platform.CodeBuildRequest{}, CodeArtifact{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The code spec is unreadable")
	}
	if err := CheckCodeBuildable(&code); err != nil {
		return platform.CodeBuildRequest{}, CodeArtifact{}, err
	}
	return platform.CodeBuildRequest{ABI: code.ABI, Language: code.Language, Source: code.Source, Input: code.Input, Output: code.Output}, CodeArtifact{SourceHash: code.SourceDigest(), BuildHash: code.BuildDigest()}, nil
}

const TableType = "build.table"
