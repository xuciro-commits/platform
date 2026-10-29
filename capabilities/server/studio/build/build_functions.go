package build

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"strings"

	"platformserver/platform"
	"slices"
	"strconv"
	"time"
)

const SchemaFunction = FunctionType + ".publish"

func (b *Build) functionEntity() platform.Entity {
	return platform.Entity{Type: FunctionType, Title: "AI function", Plural: "AI functions", Model: Function{}, Display: "title",
		Description: "A bounded typed inference over one published object; draft edits do not change installed calls.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "functions"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this typed function as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "functions", Payload: []platform.Field{}, Do: b.publishFunction}}}}
}

func (b *Build) checkFunction(f Function) error {
	if !Named(f.Name) {
		return fmt.Errorf("The function name %s must be lower-case letters and digits", f.Name)
	}
	_, installed := b.installed[f.Object]
	shape, selected := b.jointEntities[f.Object]
	if !installed && (!selected || shape.App != ID) {
		return fmt.Errorf("Function %s needs a published object of this builder", f.Name)
	}
	if f.Proposal != nil && f.Proposal.HandoffRole != "" && !b.declaresRole(f.Proposal.HandoffRole) {
		return fmt.Errorf("The proposal reviewer must hold a declared role")
	}
	// The complete candidate installs and checks its source object before the
	// original host validates this function. Draft review must not require an
	// independent live publication of that same selected object.
	validate := f.Definition().Check
	if !selected {
		validate = func() error { return b.host.ValidateInstallFunction(f.Definition()) }
	}
	if err := validate(); err != nil {
		return err
	}
	list, err := b.functionInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		installed, _ := ReadPublication[Function](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Function %s is already declared", f.Name)
		}
	}
	if old, ok := ReadPublication[Function](f.Published); ok && (old.Name != f.Name || old.Object != f.Object) {
		return fmt.Errorf("A published function keeps its name and source object")
	}
	return nil
}

func (b *Build) publishFunction(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	f, ok := record.(*Function)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkFunction(*f); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return publishRetainedDefinition(c, f, "An AI function may retain at most 64 published versions", b.installFunction)
}

func functionImage(image []byte) (Function, error) {
	return definitionImage[Function](image, "function", func(before, after Function) bool {
		return before.Name == after.Name && before.Object == after.Object
	})
}

func (b *Build) functionInventory() ([]Function, error) {
	return readDefinitionInventory[Function](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installFunction(c platform.Caller, f Function) error {
	b.ledger.Catalog.Add(functionCallActions(b.Manifest().AllRoles())...)
	if err := b.host.InstallFunction(c, f.Definition(), f.Version); err != nil {
		return err
	}
	b.functions[f.Name] = f
	return nil
}

func (b *Build) FunctionDefinition(name string, version int) (platform.AIFunction, int, bool) {
	return retainedDefinition(b.functions, b.functionInventory, functionImage, name, version)
}

func (b *Build) functionDeclarations() []platform.AIFunction {
	return definitionDeclarations(b.functions)
}

func (b *Build) functionCallEntity() platform.Entity {
	return platform.Entity{Type: FunctionCallType, Title: "AI function call", Plural: "AI function calls", Model: FunctionRun{}, Display: "function",
		Description: "A retained typed suggestion or refusal; it never changes its source record.",
		Scope: platform.Scope{Default: platform.ScopeOwn, Owner: "member", Levels: map[string]string{Builder: platform.ScopeTenant}, Participants: func(record any) []string {
			run := record.(FunctionRun)
			return append([]string{run.Member}, run.Reviewers...)
		}},
		Derived: []platform.Derivation{{From: "sources", Fields: []string{"output", "reason", "question", "sourceValues", "amendments"}}}, Withheld: "withheld"}
}

func functionCallActions(roles []string) []platform.Action {
	return append([]platform.Action{{Schema: SchemaFunctionCall, Target: FunctionCallType, New: true, Capability: "functions", Title: "Call AI function",
		Description: "Request a published function over a readable source; retain the suggestion for human review.",
		Roles:       append(slices.Clone(roles), platform.AnyMember), Automation: true,
		Payload: []platform.Field{{Name: "question", Type: "string", Description: "Bounded member question for a conversation function"}, {Name: "history", Type: "json", Description: "Up to eight prior completed calls in this conversation"}, {Name: "app", Type: "string", Description: "Registered function owner"}, {Name: "name", Type: "string", Required: true, Description: "Published function name"},
			{Name: "source", Type: "string", Required: true, Description: "Source record ID"},
			{Name: "version", Type: "integer", Description: "Published version; zero selects the installed version"},
			{Name: "release", Type: "string", Description: "Retained release for a native automation; empty keeps a development run"},
			{Name: "onBehalf", Type: "string", Description: "Retained member for a native automation"}}},
		{Schema: SchemaFunctionAnswer, Target: FunctionCallType, Capability: "functions", Title: "Record AI function answer",
			Description: "Keep the validated suggestion or refusal on its call record.", Automation: true, Payload: platform.FunctionAnswerFields()}}, platform.Action{Schema: SchemaProposalReview, Target: FunctionCallType, Capability: "functions", Title: "Review AI proposal", Description: "Reject or expire a retained proposal without changing its business source.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "decision", Type: "string", Required: true, Choices: []string{"rejected", "expired"}, Description: "Proposal decision"}}})
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
				authorization := ""
				if !c.Automation {
					authorization, _ = platform.CanonicalDigest([]any{c.ID, c.Tenant, c.Roles, c.Grants, c.Scopes, c.Status})
				}
				c.Put(r, FunctionRun{TrainingAuthorization: authorization, Question: request.Question, History: slices.Clone(request.History), App: cmp.Or(request.App, ID), Contract: definition, Record: platform.Record{ID: id}, Function: request.Name, Version: call.Version, Member: member,
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
				if proposalNeedsHandover(run) {
					recipient := platform.Recipient{AppRole: run.Contract.Proposal.HandoffRole}
					key := "proposal:" + run.ID
					if err := c.Assign(r, platform.Assignment{Title: "Review AI proposal", Ref: FunctionCallType + "/" + run.ID, To: []platform.Recipient{recipient}, Due: run.ExpiresAt, Key: key}); err != nil {
						return
					}
					if task, ok := platform.Get[platform.WorkTask](b.host.Automation(c, platform.WorkApp), ID+":"+key); ok {
						run.Reviewers = slices.Clone(task.Candidates)
					}
				}
			}
			c.Put(r, run)
		}, nil
	})
}

func functionReleaseAsset(f Function, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: f.Name}, sourceVersion+".function-"+strconv.Itoa(f.Version), f.Definition())
}

// FunctionReleaseAsset resolves only a retained version from this owner.
func (b *Build) FunctionReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "function", "function", platform.AssetFunction, name, sourceVersion, b.FunctionDefinition)
}

func (b *Build) functionAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.functions, func(record Function) (platform.ReleaseAsset, error) {
		return functionReleaseAsset(record, b.Manifest().Version)
	})
}

// InstallFunctionAsset compiles one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor repeats this compilation before snapshot restore.
func (b *Build) InstallFunctionAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetFunction, "function")
	var f Function
	if version == 0 || asset.ContractVersion != 1 ||
		json.Unmarshal(asset.Body, &f) != nil || f.Name != asset.Ref.Name ||
		f.Definition().Check() != nil {
		return fmt.Errorf("invalid builder function asset %s", asset.Ref)
	}
	if _, exists := b.functions[f.Name]; exists {
		return fmt.Errorf("function asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installFunction(platform.Caller{Replaying: true}, f)
}

func (b *Build) functionDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetFunction, "function", b.functionInventory, func(record Function) (Function, error) {
		return record, b.checkFunction(record)
	}, func(record Function) (platform.ReleaseAsset, error) {
		record.Version++
		return functionReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) prepareFunctionReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.functionInventory, platform.AssetFunction, "function", SchemaFunction, "function", b.FunctionReleaseAsset, func(record Function, asset platform.ReleaseAsset, version int) (Function, error) {
		var frozen Function
		if version != record.Version+1 || ValueDecodeIntegration(asset.Body, &frozen) != nil || frozen.Definition().Check() != nil || frozen.Name != record.Name || frozen.Object != record.Object {
			return record, fmt.Errorf("function version requires its retained predecessors and matching target definition")
		}
		if asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil {
			return record, fmt.Errorf("function candidate dependencies differ")
		}
		frozen.Record, frozen.Version, frozen.State = record.Record, version, "published"
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}

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

const FunctionCallType = "build.function-call"

const SchemaFunctionCall = FunctionCallType + ".start"

const SchemaFunctionAnswer = FunctionCallType + ".answer"

// FunctionRun keeps a suggestion separately from its business source. A
// person's ordinary business action remains the only way to adopt it.
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

// TrainingCandidates keeps function evidence with the function owner. Prompt,
// question, history, credentials and protocol continuation are not projected.
func (b *Build) TrainingCandidates(c platform.Caller, event platform.Event, images map[string]json.RawMessage) []platform.AITrainingCandidate {
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
		candidate := platform.AITrainingCandidate{Tenant: c.Tenant, Intent: run.ID, Run: ref, App: ID, Action: run.AdoptedAction, Definition: run.Definition, Sources: map[string]uint32{run.Source: run.SourceRevision}, SourceFields: map[string][]string{run.Source: slices.Clone(run.Contract.Fields)}, ExpectedSources: run.TrainingSources, Authorization: run.TrainingAuthorization, Model: run.Model, Evidence: event.App + "/" + event.Record.GetChangeId(), At: at, Outcome: run.Decision, Target: run.Source}
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
