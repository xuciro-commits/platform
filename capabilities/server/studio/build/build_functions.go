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
	"time"
)

const SchemaFunction = FunctionType + ".publish"

var functionKind = resourceKind[Function, platform.AIFunction]{
	asset: platform.AssetFunction, typ: FunctionType, prefix: "function", noun: "function", title: "AI function", plural: "AI functions", capability: "functions",
	description:  "A bounded typed inference over one published object; draft edits do not change installed calls.",
	transition:   "Install this typed function as its next retained version.",
	immutable:    "A published function keeps its name and source object",
	limitMessage: "An AI function may retain at most 64 published versions",
	records:      func(b *Build) map[string]Function { return b.functions },
	validate: func(b *Build, f Function) (Function, error) {
		if !Named(f.Name) {
			return f, fmt.Errorf("The function name %s must be lower-case letters and digits", f.Name)
		}
		_, installed := b.installed[f.Object]
		shape, selected := b.jointEntities[f.Object]
		if !installed && (!selected || shape.App != ID) {
			return f, fmt.Errorf("Function %s needs a published object of this builder", f.Name)
		}
		if f.Proposal != nil && f.Proposal.HandoffRole != "" && !b.declaresRole(f.Proposal.HandoffRole) {
			return f, fmt.Errorf("The proposal reviewer must hold a declared role")
		}
		if selected {
			return f, f.Definition().Check()
		}
		return f, b.host.ValidateInstallFunction(f.Definition())
	},
	identity: func(a, b Function) bool { return a.Name == b.Name && a.Object == b.Object },
	fromDeclaration: func(d platform.AIFunction) Function {
		return Function{Proposal: d.Proposal, Conversation: d.Conversation, Name: d.Name, Title: d.Title, Description: d.Description, Object: d.Object, Fields: d.Fields, Instructions: d.Instructions, Output: d.Output, Model: d.Model, ModelOptions: d.ModelOptions, MaxInputBytes: d.MaxInputBytes, MaxOutputBytes: d.MaxOutputBytes, MaxTokens: d.MaxTokens, Roles: d.Roles}
	},
	beforeInstall: func(b *Build) {
		catalog := b.ledger.Catalog
		if b.installationCatalog != nil {
			catalog = b.installationCatalog
		}
		b.ledger.Catalog.Add(functionCallActions((platform.Manifest{Actions: catalog, Roles: b.manifestRoles()}).AllRoles())...)
	},
	install: func(h platform.BuildHost, c platform.Caller, d platform.AIFunction, version int) error {
		return h.InstallFunction(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.AIFunction) error { return h.ValidateInstallFunction(d) },
	candidate: func(asset platform.ReleaseAsset) error {
		var d platform.AIFunction
		if ValueDecodeIntegration(asset.Body, &d) != nil || asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil {
			return fmt.Errorf("function candidate dependencies differ")
		}
		return nil
	},
}

func (b *Build) FunctionDefinition(name string, version int) (platform.AIFunction, int, bool) {
	return functionKind.definition(b, name, version)
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

func (b *Build) FunctionReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return functionKind.resolve(b, name, sourceVersion)
}

func (b *Build) InstallFunctionAsset(asset platform.ReleaseAsset) error {
	return functionKind.installAsset(b, asset)
}

const FunctionType = "build.function"

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

func (f Function) Definition() platform.AIFunction {
	return platform.AIFunction{Proposal: f.Proposal, Conversation: f.Conversation, Name: f.Name, Title: f.Title, Description: f.Description, Object: f.Object,
		Fields: f.Fields, Instructions: f.Instructions, Output: f.Output, Model: f.Model,
		MaxInputBytes: f.MaxInputBytes, MaxOutputBytes: f.MaxOutputBytes, MaxTokens: f.MaxTokens, ModelOptions: f.ModelOptions, Roles: f.Roles}
}

const FunctionCallType = "build.function-call"

const SchemaFunctionCall = FunctionCallType + ".start"

const SchemaFunctionAnswer = FunctionCallType + ".answer"

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
