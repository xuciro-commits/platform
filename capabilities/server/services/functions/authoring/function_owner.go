package authoring

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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

func (f Function) image() string {
	f.Published = ""
	f.Versions = nil
	return string(platform.Raw(f))
}

func (f Function) family() publication.Family {
	return publication.Family{ID: f.ID, Name: f.Name, State: f.State, Version: f.Version, Published: f.Published, Versions: f.Versions, Archived: f.Archived}
}

func setFamily(f *Function, family publication.Family) {
	f.ID, f.Name, f.State, f.Version, f.Published, f.Versions, f.Archived = family.ID, family.Name, family.State, family.Version, family.Published, family.Versions, family.Archived
}

func functionFromDeclaration(d platform.AIFunction) Function {
	return Function{Proposal: d.Proposal, Conversation: d.Conversation, Name: d.Name, Title: d.Title, Description: d.Description, Object: d.Object, Fields: d.Fields, Instructions: d.Instructions, Output: d.Output, OutputMode: d.OutputMode, Model: d.Model, ModelOptions: d.ModelOptions, MaxInputBytes: d.MaxInputBytes, MaxOutputBytes: d.MaxOutputBytes, MaxTokens: d.MaxTokens, Roles: d.Roles}
}

func strictDecode(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("descriptor has trailing data")
	}
	return nil
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

type Environment interface {
	Host() platform.BuildHost
	Ledger() *platform.Ledger
	App() string
	Version() string
	CallRoles() []string
	AllRoles() []string
	AddActions(...platform.Action)
	InstalledObject(string) (platform.Entity, bool)
	JointEntity(string) (platform.EntityInfo, bool)
	DeclaresRole(string) bool
	JointDeclaration(platform.AssetRef) (platform.ReleaseAsset, bool)
	AssetVersion(platform.ReleaseAsset) int
	AllReleaseAssets() ([]platform.ReleaseAsset, error)
}

type Owner struct {
	env     Environment
	records map[string]Function
}

func NewOwner(env Environment) *Owner {
	return &Owner{env: env, records: map[string]Function{}}
}

func (o *Owner) Fork(env Environment) *Owner {
	return &Owner{env: env, records: maps.Clone(o.records)}
}

func (o *Owner) Function(name string, version int) (platform.AIFunction, int, bool) {
	return publication.Retained(o.records, o.inventory, o.decode, name, version, Function.family, Function.Definition)
}

func (o *Owner) Declarations() []platform.AIFunction {
	out := []platform.AIFunction{}
	for _, name := range slices.Sorted(maps.Keys(o.records)) {
		out = append(out, o.records[name].Definition())
	}
	return out
}

func (o *Owner) Inventory() ([]Function, error) {
	return o.inventory()
}

func (o *Owner) Entity() platform.Entity {
	return platform.Entity{Type: FunctionType, Title: "AI function", Plural: "AI functions", Model: Function{}, Display: "title",
		Description: "A bounded typed inference over one published object; draft edits do not change installed calls.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder"}, Capability: "functions"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this typed function as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{"builder"}, Capability: "functions", Payload: []platform.Field{},
				Do: o.publish}}}}
}

func (o *Owner) CallEntity() platform.Entity {
	return platform.Entity{Type: FunctionCallType, Title: "AI function call", Plural: "AI function calls", Model: FunctionRun{}, Display: "function",
		Description: "A retained typed suggestion or refusal; it never changes its source record.",
		Scope: platform.Scope{Default: platform.ScopeOwn, Owner: "member", Levels: map[string]string{"builder": platform.ScopeTenant}, Participants: func(record any) []string {
			run := record.(FunctionRun)
			return append([]string{run.Member}, run.Reviewers...)
		}},
		Derived: []platform.Derivation{{From: "sources", Fields: []string{"output", "reason", "question", "sourceValues", "amendments"}}}, Withheld: "withheld"}
}

func (o *Owner) Actions() []platform.Action {
	actions := platform.EntityActions(o.Entity())
	actions = append(actions, platform.EntityActions(o.CallEntity())...)
	actions = append(actions, functionCallActions(append(o.env.CallRoles(), platform.AnyMember))...)
	return actions
}

func functionCallActions(roles []string) []platform.Action {
	return append([]platform.Action{{Schema: SchemaFunctionCall, Target: FunctionCallType, New: true, Capability: "functions", Title: "Call AI function",
		Description: "Request a published function over a readable source; retain the suggestion for human review.",
		Roles:       slices.Clone(roles), Automation: true,
		Payload: []platform.Field{{Name: "question", Type: "string", Description: "Bounded member question for a conversation function"}, {Name: "history", Type: "json", Description: "Up to eight prior completed calls in this conversation"}, {Name: "app", Type: "string", Description: "Registered function owner"}, {Name: "name", Type: "string", Required: true, Description: "Published function name"},
			{Name: "source", Type: "string", Required: true, Description: "Source record ID"},
			{Name: "version", Type: "integer", Description: "Published version; zero selects the installed version"},
			{Name: "release", Type: "string", Description: "Retained release for a native automation; empty keeps a development run"},
			{Name: "onBehalf", Type: "string", Description: "Retained member for a native automation"}}},
		{Schema: SchemaFunctionAnswer, Target: FunctionCallType, Capability: "functions", Title: "Record AI function answer",
			Description: "Keep the validated suggestion or refusal on its call record.", Automation: true, Payload: platform.FunctionAnswerFields()}}, platform.Action{Schema: SchemaProposalReview, Target: FunctionCallType, Capability: "functions", Title: "Review AI proposal", Description: "Reject or expire a retained proposal without changing its business source.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "decision", Type: "string", Required: true, Choices: []string{"rejected", "expired"}, Description: "Proposal decision"}}})
}

func (o *Owner) validate(f Function) (Function, error) {
	if !ontology.Named(f.Name) {
		return f, fmt.Errorf("The function name %s must be lower-case letters and digits", f.Name)
	}
	_, installed := o.env.InstalledObject(f.Object)
	shape, selected := o.env.JointEntity(f.Object)
	if !installed && (!selected || shape.App != o.env.App()) {
		return f, fmt.Errorf("Function %s needs a published object of this builder", f.Name)
	}
	if f.Proposal != nil && f.Proposal.HandoffRole != "" && !o.env.DeclaresRole(f.Proposal.HandoffRole) {
		return f, fmt.Errorf("The proposal reviewer must hold a declared role")
	}
	if selected {
		return f, f.Definition().Check()
	}
	return f, o.env.Host().ValidateInstallFunction(f.Definition())
}

func (o *Owner) check(f Function) (Function, error) {
	f, err := o.validate(f)
	if err != nil {
		return f, err
	}
	list, err := o.inventory()
	if err != nil {
		return f, err
	}
	for _, other := range list {
		installed, _ := publication.Read[Function](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return f, fmt.Errorf("AI function %s is already declared", f.Name)
		}
	}
	if old, ok := publication.Read[Function](f.Published); ok && (old.Name != f.Name || old.Object != f.Object) {
		return f, fmt.Errorf("a published function keeps its name and source object")
	}
	return f, nil
}

func (o *Owner) publish(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	f, ok := record.(*Function)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	bound, err := o.check(*f)
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	*f = bound
	return publication.PublishRetained(c, f, func(f *Function) publication.Family { return f.family() }, setFamily, "An AI function may retain at most 64 published versions", func(f *Function) string { return f.image() }, o.installRecord)
}

func (o *Owner) installRecord(c platform.Caller, f Function) error {
	o.env.AddActions(functionCallActions(o.env.AllRoles())...)
	if err := o.env.Host().InstallFunction(c, f.Definition(), f.Version); err != nil {
		return err
	}
	o.records[f.Name] = f
	return nil
}

func (o *Owner) InstallAsset(asset platform.ReleaseAsset) error {
	f, err := o.fromAsset(asset)
	if err != nil {
		return err
	}
	if _, exists := o.records[f.Name]; exists {
		return fmt.Errorf("function asset %s is already installed", asset.Ref)
	}
	return o.installRecord(platform.Caller{Replaying: true}, f)
}

func (o *Owner) Draft(asset platform.ReleaseAsset) (string, any, error) {
	var definition platform.AIFunction
	if err := strictDecode(asset.Body, &definition); err != nil {
		return "", nil, err
	}
	return FunctionType, functionFromDeclaration(definition), nil
}

func (o *Owner) fromAsset(asset platform.ReleaseAsset) (Function, error) {
	var zero Function
	version := o.env.AssetVersion(asset)
	var d platform.AIFunction
	if version == 0 || strictDecode(asset.Body, &d) != nil || asset.ContractVersion != 1 || d.Check() != nil || d.Name != asset.Ref.Name || platform.CheckAssetDependencies(asset) != nil {
		return zero, fmt.Errorf("invalid builder function asset %s", asset.Ref)
	}
	f := functionFromDeclaration(d)
	family := f.family()
	family.Version, family.State = version, "published"
	setFamily(&f, family)
	return f, nil
}

func (o *Owner) decode(raw []byte) (Function, error) {
	return publication.Decode(raw, "function", Function.family, func(f Function) error { return f.Definition().Check() }, func(old, next Function) bool {
		return old.Name == next.Name && old.Object == next.Object
	})
}

func (o *Owner) ValidateImage(raw []byte) error {
	selected, err := o.decode(raw)
	if err != nil {
		return err
	}
	return o.env.Host().ValidateInstallFunction(selected.Definition())
}

func (o *Owner) ApplyImage(raw []byte) error {
	selected, err := o.decode(raw)
	if err != nil {
		return err
	}
	var record Function
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	for _, version := range record.Versions {
		f, ok := publication.Read[Function](version)
		if !ok {
			return fmt.Errorf("accepted function has a malformed version")
		}
		if err := o.installRecord(platform.Caller{Replaying: true}, f); err != nil {
			return err
		}
	}
	return o.installRecord(platform.Caller{Replaying: true}, selected)
}

func (o *Owner) Restore() error {
	rows, err := o.inventory()
	if err != nil {
		return err
	}
	for _, record := range rows {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		if err := o.ApplyImage(raw); err != nil {
			return err
		}
	}
	return nil
}

func (o *Owner) Assets() ([]platform.ReleaseAsset, error) {
	return publication.InstalledAssets(o.records, Function.family, o.releaseAsset)
}

func (o *Owner) releaseAsset(f Function) (platform.ReleaseAsset, error) {
	ref := platform.AssetRef{App: o.env.App(), Kind: platform.AssetFunction, Name: f.Name}
	return publication.ReleaseAsset(ref, o.env.Version()+".function-"+fmt.Sprint(f.Version), f.Definition())
}

func (o *Owner) Resolve(name, sourceVersion string) (platform.ReleaseAsset, error) {
	return publication.RetainedAsset(o.env.App(), o.env.Version(), "function", "AI function", platform.AssetFunction, name, sourceVersion, o.Function)
}

func (o *Owner) Prepare(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return publication.Prepare(o.env.App(), o.env.Version(), assets, platform.AssetFunction, "function", SchemaFunction, o.inventory, Function.family, setFamily, o.Resolve,
		func(record Function, asset platform.ReleaseAsset, version int) (Function, error) {
			family := record.family()
			prior, retained := publication.Read[Function](family.Published)
			var d platform.AIFunction
			if version != family.Version+1 || strictDecode(asset.Body, &d) != nil || asset.ContractVersion != 1 || d.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
				return record, fmt.Errorf("invalid next function version")
			}
			frozen := functionFromDeclaration(d)
			if frozen.Name != record.Name || frozen.Object != record.Object || retained && prior.Object != frozen.Object {
				return record, fmt.Errorf("invalid next function version")
			}
			frozen.Record = record.Record
			kept := frozen.family()
			kept.State, kept.Version = "published", version
			setFamily(&frozen, kept)
			family.State, family.Version, family.Published = "published", version, frozen.image()
			family.Versions = append(slices.Clone(family.Versions), family.Published)
			setFamily(&record, family)
			return record, nil
		})
}

func (o *Owner) DraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = o.env.AllReleaseAssets()
	if err != nil {
		return
	}
	records, err := o.inventory()
	if err != nil {
		return
	}
	i := slices.IndexFunc(records, func(record Function) bool { return record.ID == id && !record.Archived })
	if i < 0 {
		err = fmt.Errorf("draft function %q not found", id)
		return
	}
	record := records[i]
	family := record.family()
	if old, ok := publication.Read[Function](family.Published); ok {
		prior = platform.AssetRef{App: o.env.App(), Kind: platform.AssetFunction, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: o.env.App(), Kind: platform.AssetFunction, Name: family.Name}
	record, err = o.check(record)
	if err != nil {
		return
	}
	if family.Version >= 64 {
		err = fmt.Errorf("function version family is full")
		return
	}
	family.Version++
	setFamily(&record, family)
	asset, err := o.releaseAsset(record)
	if err != nil {
		return
	}
	after = slices.Clone(before)
	if hadPrior {
		after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == prior })
	}
	after = append(after, asset)
	return
}

func (o *Owner) Definition(name string, version int) (platform.AIFunction, int, bool) {
	if asset, selected := o.env.JointDeclaration(platform.AssetRef{App: o.env.App(), Kind: platform.AssetFunction, Name: name}); selected {
		ordinal := o.env.AssetVersion(asset)
		if ordinal == version && version > 0 {
			var d platform.AIFunction
			valid := json.Unmarshal(asset.Body, &d) == nil && asset.ContractVersion == 1 && functionFromDeclaration(d).Name == name && d.Check() == nil
			return d, ordinal, valid
		}
	}
	return o.Function(name, version)
}

func (o *Owner) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	switch s.GetSchema().GetName() {
	case SchemaFunctionCall, SchemaFunctionAnswer:
		return o.submitCall(c, s, now)
	case SchemaProposalReview:
		return o.submitProposalReview(c, s, now)
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (o *Owner) submitCall(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Staging() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, platform.FunctionNeedsDecision)
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
				definition, _, ok := o.env.Host().Function(cmp.Or(request.App, o.env.App()), request.Name, call.Version)
				if !ok {
					return
				}
				authorization := ""
				if !c.Automation {
					authorization, _ = platform.CanonicalDigest([]any{c.ID, c.Tenant, c.Roles, c.Grants, c.Scopes, c.Status})
				}
				c.Put(r, FunctionRun{TrainingAuthorization: authorization, Question: request.Question, History: slices.Clone(request.History), App: cmp.Or(request.App, o.env.App()), Contract: definition, Record: platform.Record{ID: id}, Function: request.Name, Version: call.Version, Member: member,
					SourceValues: call.SourceValues, SourceRevision: call.SourceRevision, Applications: call.Applications, Source: call.Source, State: "pending", Definition: call.Definition, Dependencies: call.Dependencies,
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
		return func(r *pb.ChangeRecord) {
			if run.State == "ready" && run.Contract.Proposal != nil {
				run.Decision = "pending"
				run.ExpiresAt = now.Add(time.Duration(run.Contract.Proposal.ExpiresAfterSeconds) * time.Second)
				if NeedsHandover(run) {
					recipient := platform.Recipient{AppRole: run.Contract.Proposal.HandoffRole}
					key := "proposal:" + run.ID
					if err := c.Assign(r, platform.Assignment{Title: "Review AI proposal", Ref: FunctionCallType + "/" + run.ID, To: []platform.Recipient{recipient}, Due: run.ExpiresAt, Key: key}); err != nil {
						return
					}
					if task, ok := platform.Get[platform.WorkTask](o.env.Host().Automation(c, platform.WorkApp), o.env.App()+":"+key); ok {
						run.Reviewers = slices.Clone(task.Candidates)
					}
				}
			}
			c.Put(r, run)
		}, nil
	})
}

func (o *Owner) submitProposalReview(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		raw, err := c.ReadRecord(FunctionCallType, s.GetTarget().GetId(), now)
		var visible FunctionRun
		run, ok := platform.Get[FunctionRun](c, s.GetTarget().GetId())
		if err != nil || json.Unmarshal(raw, &visible) != nil || visible.Withheld || !ok || run.Contract.Proposal == nil || run.State != "ready" || run.Decision != "pending" || c.ID != run.Member && !c.Holds(o.env.App(), run.Contract.Proposal.HandoffRole) && !c.Holds(o.env.App(), "builder") {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The pending proposal is not available for review")
		}
		var input struct {
			Decision string `json:"decision"`
		}
		if json.Unmarshal(s.GetPayload(), &input) != nil || input.Decision != "rejected" && input.Decision != "expired" || input.Decision == "expired" && (run.ExpiresAt.IsZero() || now.Before(run.ExpiresAt)) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose rejection, or expiry after the proposal deadline")
		}
		return func(r *pb.ChangeRecord) {
			if input.Decision == "rejected" {
				fact := platform.BusinessRefusalFact{Level: "rejected", Code: "AI_PROPOSAL_REJECTED", Message: "The reviewer rejected the AI proposal", Translations: map[string]string{"zh-CN": "审核人驳回了 AI 提议"}, Subject: FunctionCallType + "/" + run.ID, Action: s.GetSchema().GetName(), Member: c.ID, At: now.UTC(), Original: raw, Inputs: map[string]json.RawMessage{"decision": platform.Raw(input.Decision)}, Record: FunctionCallType + "/" + run.ID}
				if err := c.CaptureBusinessRefusal(r, fact); err != nil {
					c.FailDecision(err)
					return
				}
			}
			run.Decision = input.Decision
			run.DecidedBy = c.ID
			run.DecidedAt = now
			if NeedsHandover(run) {
				if err := c.Assign(r, platform.Assignment{Key: "proposal:" + run.ID, Close: true}); err != nil {
					return
				}
			}
			c.Put(r, run)
		}, nil
	})
}

func NeedsHandover(run FunctionRun) bool {
	p := run.Contract.Proposal
	if p == nil || p.HandoffRole == "" {
		return false
	}
	if p.ConfidenceField == "" {
		return true
	}
	var values map[string]json.RawMessage
	var confidence float64
	return json.Unmarshal([]byte(run.Output), &values) != nil || json.Unmarshal(values[p.ConfidenceField], &confidence) != nil || confidence < p.MinimumConfidence
}

func (o *Owner) TrainingCandidates(c platform.Caller, event platform.Event, images map[string]json.RawMessage) []platform.AITrainingCandidate {
	out := []platform.AITrainingCandidate{}
	at := event.Record.GetRecordedTime().AsTime()
	for _, ref := range event.Changed {
		prefix := FunctionCallType + "/"
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		var run FunctionRun
		if json.Unmarshal(images[ref], &run) != nil || (run.Decision != "adopted" && run.Decision != "rejected") || run.State != "ready" {
			continue
		}
		candidate := platform.AITrainingCandidate{Tenant: c.Tenant, Intent: run.ID, Run: ref, App: o.env.App(), Action: run.AdoptedAction, Definition: run.Definition, Sources: map[string]uint32{run.Source: run.SourceRevision}, SourceFields: map[string][]string{run.Source: slices.Clone(run.Contract.Fields)}, ExpectedSources: run.TrainingSources, Authorization: run.TrainingAuthorization, Model: run.Model, Evidence: event.App + "/" + event.Record.GetChangeId(), At: at, Outcome: run.Decision, Target: run.Source}
		if run.Decision == "adopted" {
			candidate.Outcome = "accepted"
			_ = json.Unmarshal([]byte(run.Output), &candidate.Chosen)
			var amendments map[string]json.RawMessage
			_ = json.Unmarshal(run.Amendments, &amendments)
			if len(amendments) > 0 {
				_ = json.Unmarshal([]byte(run.Output), &candidate.Rejected)
				for name, value := range amendments {
					if _, ok := candidate.Chosen[name]; !ok {
						candidate.Chosen, candidate.Rejected = nil, nil
						break
					}
					candidate.Chosen[name] = value
				}
				candidate.Critique = "The reviewer corrected the proposed fields"
			}
		} else {
			_ = json.Unmarshal([]byte(run.Output), &candidate.Rejected)
			candidate.Critique = "The reviewer rejected the AI proposal"
		}
		if candidate.Authorization == "" {
			candidate.Rejected, candidate.Critique = nil, ""
		}
		identity := sha256.Sum256(platform.Raw([]any{c.Tenant, ref, run.Decision, run.DecidedAt, run.Definition, run.InputHash}))
		candidate.ID = hex.EncodeToString(identity[:])
		out = append(out, candidate)
	}
	return out
}

func (o *Owner) inventory() ([]Function, error) {
	return publication.Inventory[Function](o.env.Host().Automation(platform.Caller{}, o.env.App()))
}
