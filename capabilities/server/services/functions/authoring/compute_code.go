package authoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
	"time"
)

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
func CodeEntity() platform.Entity {
	return platform.Entity{Type: CodeType, Title: "Code function", Plural: "Code functions", Model: Code{}, Display: "title", Description: "A typed Go/TinyGo algorithm built in isolation and activated with its application.", Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant}}, Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder"}, Capability: "compute"}, Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "compiling", Title: "Compiling", Tone: "warning"}, {Name: "compiled", Title: "Compiled", Tone: "success"}, {Name: "failed", Title: "Build failed", Tone: "danger"}, {Name: "published", Title: "Published", Tone: "success"}}, Actions: []platform.ActionType{{
		Name:        "compile",
		Title:       "Compile",
		Description: "Build the current source with an isolated pinned Go/TinyGo toolchain.",
		Roles:       []string{"builder"},
		Capability:  "compute",
		Parameters:  []platform.Field{},
		Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "compiled", "failed", "published"}}},
		Rules:       []platform.ActionRule{{Function: compileCode}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"compiling"}}}},
	}}}}
}
func compileCode(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	var code *Code
	switch value := record.(type) {
	case *Code:
		code = value
	default:
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Code requires Go/TinyGo source and bounded input/output schemas")
	}
	return checkCodeBuildable(code)
}

func checkCodeBuildable(code *Code) *platform.Error {
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
	if err := checkCodeBuildable(&code); err != nil {
		return platform.CodeBuildRequest{}, CodeArtifact{}, err
	}
	return platform.CodeBuildRequest{ABI: code.ABI, Language: code.Language, Source: code.Source, Input: code.Input, Output: code.Output}, CodeArtifact{SourceHash: code.SourceDigest(), BuildHash: code.BuildDigest()}, nil
}

func (o *Owner) DecideCodeBuild(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool) {
	switch s.GetSchema().GetName() {
	case SchemaCodeCompile:
		record, err := o.submitCodeCompile(c, s, now)
		return record, err, true
	case SchemaCodeCompiled:
		record, err := o.submitCodeCompiled(c, s, now)
		return record, err, true
	case SchemaCodeAttach:
		record, err := o.submitCodeAttach(c, s, now)
		return record, err, true
	}
	return nil, nil, false
}

func (o *Owner) submitCodeAttach(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var artifact CodeArtifact
		code, ok := platform.Get[Code](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &artifact) != nil || code.State == "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Code is missing or compiling")
		}
		if err := checkCodeBuildable(&code); err != nil {
			return nil, err
		}
		if len(artifact.Module) != 64 || strings.Trim(artifact.Module, "0123456789abcdef") != "" || artifact.Toolchain == "" || artifact.SourceHash != code.SourceDigest() || artifact.BuildHash != code.BuildDigest() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The attached build differs from the current source and schemas")
		}
		return func(r *pb.ChangeRecord) {
			code.Module, code.SourceHash, code.BuildHash, code.Toolchain = artifact.Module, artifact.SourceHash, artifact.BuildHash, artifact.Toolchain
			code.BuiltABI, code.BuiltSource, code.BuiltLanguage, code.BuiltInput, code.BuiltOutput = code.ABI, code.Source, code.Language, code.Input, code.Output
			code.State, code.Diagnostics = "compiled", ""
			c.Put(r, code)
		}, nil
	})
}

func (o *Owner) submitCodeCompile(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		code, ok := platform.Get[Code](c, s.GetTarget().GetId())
		if !ok || code.State == "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Code is missing or already compiling")
		}
		if err := compileCode(c, &code, nil, now); err != nil {
			return nil, err
		}
		return func(r *pb.ChangeRecord) {
			call, err := c.RequestCompilation(r, r.GetChangeId(), CodeType+"/"+code.ID, platform.CodeBuildRequest{ABI: code.ABI, Language: code.Language, Source: code.Source, Input: code.Input, Output: code.Output})
			if err != nil {
				return
			}
			code.State, code.Call, code.Diagnostics = "compiling", call.ID, ""
			c.Put(r, code)
		}, nil
	})
}
func CodeActions() []platform.Action {
	return []platform.Action{{Schema: SchemaCodeAttach, Target: CodeType, Capability: "compute", Title: "Attach code build", Description: "Retain an isolated build made on a branch on the code asset it was built from.", Automation: true, Payload: []platform.Field{{Name: "module", Type: "string", Required: true, Description: "The content digest of the built module"}, {Name: "sourceHash", Type: "string", Required: true, Description: "The digest of the built source"}, {Name: "buildHash", Type: "string", Required: true, Description: "The digest of the source and schema snapshot"}, {Name: "toolchain", Type: "string", Required: true, Description: "The pinned toolchain that built it"}}}, {Schema: SchemaCodeCompiled, Target: CodeType, Capability: "compute", Title: "Keep code build result", Description: "Retain the accepted compiler output on its source asset.", Automation: true, Payload: []platform.Field{{Name: "call", Type: "string", Required: true, Description: "The accepted compilation call"}, {Name: "result", Type: "json", Required: true, Description: "Validated compiler result"}, {Name: "request", Type: "json", Required: true, Description: "The accepted source and SDK schema snapshot"}}}}
}
func (o *Owner) submitCodeCompiled(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var payload struct {
			Call    string                    `json:"call"`
			Result  platform.CodeBuildResult  `json:"result"`
			Request platform.CodeBuildRequest `json:"request"`
		}
		code, ok := platform.Get[Code](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || code.Call != payload.Call || code.State != "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compiler result has no matching current build")
		}
		return func(r *pb.ChangeRecord) {
			code.Diagnostics = payload.Result.Diagnostics
			code.State = "failed"
			built := Code{ABI: payload.Request.ABI, Language: payload.Request.Language, Source: payload.Request.Source, Input: payload.Request.Input, Output: payload.Request.Output}
			if payload.Result.Module != "" && payload.Result.SourceHash == built.SourceDigest() && payload.Result.BuildHash == built.BuildDigest() {
				code.Module, code.SourceHash, code.Toolchain, code.BuildHash = payload.Result.Module, payload.Result.SourceHash, payload.Result.Toolchain, payload.Result.BuildHash
				code.BuiltABI = payload.Request.ABI
				code.BuiltSource, code.BuiltLanguage, code.BuiltInput, code.BuiltOutput = payload.Request.Source, payload.Request.Language, payload.Request.Input, payload.Request.Output
				code.State = "compiled"
			} else if code.Diagnostics == "" {
				code.Diagnostics = "Source or schema changed while this build was running"
			}
			c.Put(r, code)
		}, nil
	})
}

func (o *Owner) AnswerCompilation(c platform.Caller, app platform.App, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error {
	if effect.Endpoint != "operation" || !strings.HasPrefix(effect.Target, CodeType+"/") {
		return nil
	}
	var result platform.CodeBuildResult
	var binding struct {
		Build *platform.CodeBuildRequest `json:"build"`
	}
	if json.Unmarshal([]byte(effect.Body), &binding) != nil || binding.Build == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compiler has no frozen source")
	}
	if out.Result == "delivered" {
		if err := json.Unmarshal(out.Answer, &result); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Compiler returned an invalid result")
		}
	} else {
		result.Diagnostics = out.Detail
	}
	payload, _ := json.Marshal(struct {
		Call    string                    `json:"call"`
		Result  platform.CodeBuildResult  `json:"result"`
		Request platform.CodeBuildRequest `json:"request"`
	}{effect.ID, result, *binding.Build})
	_, err := platform.Decide(c, app, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: o.env.App(), IdempotencyKey: "compiled:" + effect.ID, Target: &pb.EntityRef{Type: CodeType, Id: strings.TrimPrefix(effect.Target, CodeType+"/")}, Schema: &pb.SchemaRef{Name: SchemaCodeCompiled, Version: 1}, Payload: payload}, now)
	return err
}
