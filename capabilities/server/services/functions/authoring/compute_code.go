package authoring

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"strings"
	"time"
)

func (o *Owner) CodeEntity() platform.Entity {
	return platform.Entity{Type: protocol.CodeType, Title: "Code function", Plural: "Code functions", Model: protocol.Code{}, Display: "title", Description: "A typed Go/TinyGo algorithm built in isolation and activated with its application.", Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant}}, Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder"}, Capability: "compute"}, Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "compiling", Title: "Compiling", Tone: "warning"}, {Name: "compiled", Title: "Compiled", Tone: "success"}, {Name: "failed", Title: "Build failed", Tone: "danger"}, {Name: "published", Title: "Published", Tone: "success"}}, Actions: []platform.ActionType{{
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
	var code *protocol.Code
	switch value := record.(type) {
	case *protocol.Code:
		code = value
	default:
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Code requires Go/TinyGo source and bounded input/output schemas")
	}
	return protocol.CheckCodeBuildable(code)
}

func (o *Owner) DecideCodeBuild(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool) {
	switch s.GetSchema().GetName() {
	case protocol.SchemaCodeCompile:
		record, err := o.submitCodeCompile(c, s, now)
		return record, err, true
	case protocol.SchemaCodeCompiled:
		record, err := o.submitCodeCompiled(c, s, now)
		return record, err, true
	case protocol.SchemaCodeAttach:
		record, err := o.submitCodeAttach(c, s, now)
		return record, err, true
	}
	return nil, nil, false
}

func (o *Owner) submitCodeAttach(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var artifact protocol.CodeArtifact
		code, ok := platform.Get[protocol.Code](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &artifact) != nil || code.State == "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Code is missing or compiling")
		}
		if err := protocol.CheckCodeBuildable(&code); err != nil {
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
		code, ok := platform.Get[protocol.Code](c, s.GetTarget().GetId())
		if !ok || code.State == "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Code is missing or already compiling")
		}
		if err := compileCode(c, &code, nil, now); err != nil {
			return nil, err
		}
		return func(r *pb.ChangeRecord) {
			call, err := c.RequestCompilation(r, r.GetChangeId(), protocol.CodeType+"/"+code.ID, platform.CodeBuildRequest{ABI: code.ABI, Language: code.Language, Source: code.Source, Input: code.Input, Output: code.Output})
			if err != nil {
				return
			}
			code.State, code.Call, code.Diagnostics = "compiling", call.ID, ""
			c.Put(r, code)
		}, nil
	})
}
func (o *Owner) CodeActions() []platform.Action {
	return []platform.Action{{Schema: protocol.SchemaCodeAttach, Target: protocol.CodeType, Capability: "compute", Title: "Attach code build", Description: "Retain an isolated build made on a branch on the code asset it was built from.", Automation: true, Payload: []platform.Field{{Name: "module", Type: "string", Required: true, Description: "The content digest of the built module"}, {Name: "sourceHash", Type: "string", Required: true, Description: "The digest of the built source"}, {Name: "buildHash", Type: "string", Required: true, Description: "The digest of the source and schema snapshot"}, {Name: "toolchain", Type: "string", Required: true, Description: "The pinned toolchain that built it"}}}, {Schema: protocol.SchemaCodeCompiled, Target: protocol.CodeType, Capability: "compute", Title: "Keep code build result", Description: "Retain the accepted compiler output on its source asset.", Automation: true, Payload: []platform.Field{{Name: "call", Type: "string", Required: true, Description: "The accepted compilation call"}, {Name: "result", Type: "json", Required: true, Description: "Validated compiler result"}, {Name: "request", Type: "json", Required: true, Description: "The accepted source and SDK schema snapshot"}}}}
}
func (o *Owner) submitCodeCompiled(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var payload struct {
			Call    string                    `json:"call"`
			Result  platform.CodeBuildResult  `json:"result"`
			Request platform.CodeBuildRequest `json:"request"`
		}
		code, ok := platform.Get[protocol.Code](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || code.Call != payload.Call || code.State != "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compiler result has no matching current build")
		}
		return func(r *pb.ChangeRecord) {
			code.Diagnostics = payload.Result.Diagnostics
			code.State = "failed"
			built := protocol.Code{ABI: payload.Request.ABI, Language: payload.Request.Language, Source: payload.Request.Source, Input: payload.Request.Input, Output: payload.Request.Output}
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
	if effect.Endpoint != "operation" || !strings.HasPrefix(effect.Target, protocol.CodeType+"/") {
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
	_, err := platform.Decide(c, app, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: o.env.App(), IdempotencyKey: "compiled:" + effect.ID, Target: &pb.EntityRef{Type: protocol.CodeType, Id: strings.TrimPrefix(effect.Target, protocol.CodeType+"/")}, Schema: &pb.SchemaRef{Name: protocol.SchemaCodeCompiled, Version: 1}, Payload: payload}, now)
	return err
}
