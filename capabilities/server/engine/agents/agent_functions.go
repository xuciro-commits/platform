package agents

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"strings"
	"time"
	"unicode/utf8"
)

type FunctionBinding struct {
	Owner      string                `json:"owner,omitempty"`
	Definition platform.AIFunction   `json:"definition"`
	Call       platform.FunctionCall `json:"call"`
	Member     string                `json:"member"`
}
type FunctionSource struct {
	Revision       uint32
	Values         map[string]json.RawMessage
	Raw            []byte
	HistorySources []string
}
type FunctionDispatch struct {
	Model   string
	Binding platform.AIModelBinding
	Effect  platform.Effect
}
type FunctionContext struct {
	Tenant           string
	App              func(string) platform.App
	Member           func(string) (platform.Member, bool)
	AuthorityOf      func(string) string
	Authorize        func(platform.Caller, string, platform.AIFunction, []string, string, time.Time) *kernel.Error
	Source           func(platform.Member, platform.FunctionRequest, platform.AIFunction, string, string, string, int) (FunctionSource, *kernel.Error)
	Readable         func(platform.Member, string, time.Time) bool
	Closure          func(string, platform.AIFunction, int, *string) (platform.ReleaseCandidate, string, error)
	BindApplications func(platform.AssetRef, string, string) ([]platform.AssetBinding, error)
	Model            func(platform.Caller, *pb.ChangeRecord, platform.Request, []platform.Effect, platform.ModelOptions) (FunctionDispatch, *kernel.Error)
	Bind             func(platform.Effect, FunctionBinding, platform.AIModelBinding) platform.Effect
}
type FunctionHistory struct {
	Archived                                                   bool
	State, App, Function, Definition, Source, Question, Output string
	Version                                                    int
}

func PlanFunction(ctx FunctionContext, c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest, pending []platform.Effect) (platform.FunctionCall, platform.Effect, *kernel.Error) {
	refuse := func(code pb.ErrorCode, message string) (platform.FunctionCall, platform.Effect, *kernel.Error) {
		return platform.FunctionCall{}, platform.Effect{}, platform.Refuse(code, message)
	}
	if c.Tenant != ctx.Tenant || r.GetSubmission().GetTenantId() != ctx.Tenant || r.GetSubmission().GetAuthority() != c.App ||
		request.Version < 0 || c.Automation && request.OnBehalf == "" || !c.Automation && (request.Release != nil || request.OnBehalf != "" && request.OnBehalf != c.ID) {
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI functions need an explicit authorised member")
	}
	member := c.Member
	if c.Automation {
		var found bool
		member, found = ctx.Member(request.OnBehalf)
		if !found || member.Tenant != ctx.Tenant || member.Agent {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI functions need an explicit authorised member")
		}
	}
	owner := request.App
	if owner == "" {
		owner = c.App
	}
	app := ctx.App(owner)
	if app == nil {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function is not declared")
	}
	f, version, found := FunctionDefinition(app, request.Name, request.Version)
	if !found {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function is not declared")
	}
	target := r.GetSubmission().GetTarget()
	replyOwner := ctx.App(c.App)
	if replyOwner == nil {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function reply owner is not installed")
	}
	action, ok := replyOwner.Manifest().Actions.Action(request.Reply)
	if !ok || action.Target != target.GetType() || ctx.AuthorityOf(target.GetType()) != c.App || action.Approval != nil || !action.Automation {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The AI function needs an automatic reply on its owner's target object")
	}
	source := request.Source
	if source == "" && target.GetType() == f.Object {
		source = target.GetId()
	}
	if source == "" {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an AI function source record")
	}
	sourceRef := f.Object + "/" + source
	definition, err := platform.CanonicalDigest([]any{owner, f})
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function definition cannot be bound")
	}
	at := r.GetRecordedTime().AsTime()
	sources := []string{}
	for _, field := range f.Fields {
		sources = append(sources, sourceRef+"#"+field)
	}
	member.AIContext = true
	applicant := platform.ActingCaller(c, member, owner, false)
	if refusal := ctx.Authorize(applicant, owner, f, sources, definition, at); refusal != nil {
		return platform.FunctionCall{}, platform.Effect{}, refusal
	}
	prepared, problem := ctx.Source(member, request, f, owner, sourceRef, definition, version)
	if problem != nil {
		return platform.FunctionCall{}, platform.Effect{}, problem
	}
	raw, sourceRevision, sourceValues, historySources := prepared.Raw, prepared.Revision, prepared.Values, prepared.HistorySources
	for _, ref := range historySources {
		if !ctx.Readable(member, ref, at) {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The conversation history is not readable")
		}
	}
	sources = append(sources, historySources...)
	if len(raw) > f.MaxInputBytes {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The AI function input exceeds its byte budget")
	}
	candidate, release, err := ctx.Closure(owner, f, version, request.Release)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function dependencies cannot be bound")
	}
	hash, _ := platform.CanonicalDigest(string(raw))
	q := platform.Request{Model: f.Model, Target: target.GetId(), Reply: request.Reply,
		Payload: platform.Prompt{System: f.SystemPrompt(), User: string(raw), MaxTokens: f.MaxTokens}}
	dispatch, problem := ctx.Model(c, r, q, pending, f.ModelOptions.Override(platform.ModelOptions{MaxTokens: f.MaxTokens}))
	if problem != nil {
		return platform.FunctionCall{}, platform.Effect{}, problem
	}
	applications, err := ctx.BindApplications(platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: f.Name}, release, candidate.ID)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function dependencies cannot be bound")
	}
	call := platform.FunctionCall{SourceValues: sourceValues, SourceRevision: sourceRevision, Applications: applications, Definition: definition, Dependencies: candidate.ID, Model: dispatch.Model, InputHash: hash, Sources: sources, Source: sourceRef, Version: version, Release: release}
	if dispatch.Model == "" {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function needs a bound model")
	}
	effect := ctx.Bind(dispatch.Effect, FunctionBinding{Owner: owner, Definition: f, Call: call, Member: member.ID}, dispatch.Binding)
	effect.Applications = applications
	return call, effect, nil
}

func FunctionDefinition(app platform.App, name string, version int) (platform.AIFunction, int, bool) {
	if app == nil {
		return platform.AIFunction{}, 0, false
	}
	if owner, ok := app.(platform.FunctionDefinitions); ok {
		return owner.FunctionDefinition(name, version)
	}
	if version != 0 {
		return platform.AIFunction{}, 0, false
	}
	for _, f := range app.Manifest().Functions {
		if f.Name == name {
			return f, 0, true
		}
	}
	return platform.AIFunction{}, 0, false
}

func FunctionConversation(request platform.FunctionRequest, f platform.AIFunction, owner, source, definition string, version int, input map[string]any, read func(string) (FunctionHistory, error)) (any, []string, error) {
	if !f.Conversation {
		if request.Question != "" || len(request.History) > 0 {
			return nil, nil, fmt.Errorf("this function has no conversation input")
		}
		return input, nil, nil
	}
	if strings.TrimSpace(request.Question) == "" || len(request.Question) > 4096 || !utf8.ValidString(request.Question) || len(request.History) > 8 {
		return nil, nil, fmt.Errorf("conversation needs a bounded question and history")
	}
	if request.Reply != "build.function-call.answer" {
		return nil, nil, fmt.Errorf("conversation history needs the original function call owner")
	}
	type turn struct {
		Question string          `json:"question"`
		Answer   json.RawMessage `json:"answer"`
	}
	history := []turn{}
	refs := []string{}
	seen := map[string]bool{}
	for _, id := range request.History {
		if id == "" || len(id) > 1024 || seen[id] {
			return nil, nil, fmt.Errorf("conversation history is unavailable")
		}
		seen[id] = true
		run, err := read(id)
		if err != nil {
			return nil, nil, err
		}
		if run.Archived || run.State != "ready" || run.App != owner || run.Function != f.Name || run.Version != version || run.Definition != definition || run.Source != source || f.ValidateOutput([]byte(run.Output)) != nil {
			return nil, nil, fmt.Errorf("conversation history belongs to another context")
		}
		history = append(history, turn{Question: run.Question, Answer: json.RawMessage(run.Output)})
		refs = append(refs, "build.function-call"+"/"+id+"#question", "build.function-call"+"/"+id+"#output")
	}
	return struct {
		Record   map[string]any `json:"record"`
		Question string         `json:"question"`
		History  []turn         `json:"history"`
	}{input, request.Question, history}, refs, nil
}

// FunctionAccess keeps current grants and records with their existing owners.
type FunctionAccess struct {
	Tenant      string
	Member      func(string) (platform.Member, bool)
	AuthorityOf func(string) string
	Suspended   func(string) bool
	Authorize   func(platform.Member, string, platform.AIFunction, []string, string, time.Time) bool
	Source      func(string, string) (platform.EntityInfo, bool)
}

func FunctionAllowed(ctx FunctionAccess, app string, binding *FunctionBinding, model, record, call string, prompt platform.Prompt, at time.Time) bool {
	f := binding.Definition
	owner := binding.Owner
	if owner == "" {
		owner = app
	}
	definition, err := platform.CanonicalDigest([]any{owner, f})
	hash, hashErr := platform.CanonicalDigest(prompt.User)
	member, ok := ctx.Member(binding.Member)
	sourceType, sourceID, sourceOK := strings.Cut(binding.Call.Source, "/")
	replyType, replyID, replyOK := strings.Cut(record, "/")
	if !ok || member.Tenant != ctx.Tenant || f.Check() != nil || err != nil || hashErr != nil ||
		definition != binding.Call.Definition || hash != binding.Call.InputHash || model != binding.Call.Model ||
		prompt.System != f.SystemPrompt() || prompt.MaxTokens != f.MaxTokens || len(prompt.User) > f.MaxInputBytes ||
		!sourceOK || sourceID == "" || sourceType != f.Object || !replyOK || replyID != call || ctx.AuthorityOf(replyType) != app ||
		member.Agent && ctx.Suspended(member.ID) || len(binding.Call.Sources) < len(f.Fields) || !f.Conversation && len(binding.Call.Sources) != len(f.Fields) || f.Conversation && len(binding.Call.Sources) > len(f.Fields)+16 {
		return false
	}
	if !ctx.Authorize(member, owner, f, binding.Call.Sources, definition, at) {
		return false
	}
	sourceInfo, present := ctx.Source(sourceType, sourceID)
	if !present {
		return false
	}
	for i, field := range f.Fields {
		if current, ok := sourceInfo.Field(field); !ok || current.AIInput != nil && !*current.AIInput {
			return false
		}
		ref := binding.Call.Source + "#" + field
		if binding.Call.Sources[i] != ref {
			return false
		}
	}
	return true
}
