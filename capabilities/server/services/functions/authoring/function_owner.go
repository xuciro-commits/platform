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
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
	"platformserver/services/resources/publication"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func functionImage(f protocol.Function) string {
	f.Published = ""
	f.Versions = nil
	return string(platform.Raw(f))
}

func functionFamily(f protocol.Function) publication.Family {
	return publication.Family{ID: f.ID, Name: f.Name, State: f.State, Version: f.Version, Published: f.Published, Versions: f.Versions, Archived: f.Archived}
}

func setFamily(f *protocol.Function, family publication.Family) {
	f.ID, f.Name, f.State, f.Version, f.Published, f.Versions, f.Archived = family.ID, family.Name, family.State, family.Version, family.Published, family.Versions, family.Archived
}

func functionFromDeclaration(d platform.AIFunction) protocol.Function {
	return protocol.Function{Proposal: d.Proposal, Conversation: d.Conversation, Name: d.Name, Title: d.Title, Description: d.Description, Object: d.Object, Fields: d.Fields, Instructions: d.Instructions, Output: d.Output, OutputMode: d.OutputMode, Model: d.Model, ModelOptions: d.ModelOptions, MaxInputBytes: d.MaxInputBytes, MaxOutputBytes: d.MaxOutputBytes, MaxTokens: d.MaxTokens, Roles: d.Roles}
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

func RunValue(value any) (protocol.FunctionRun, bool) {
	return records.As[protocol.FunctionRun](value)
}

type Owner struct {
	resolver publication.Resolver
	env      publication.FunctionEnvironment
	records  map[string]protocol.Function
	codes    map[string]protocol.Code
	tables   map[string]Table
}

func NewOwner(env publication.FunctionEnvironment) publication.FunctionAuthoring {
	return &Owner{env: env, resolver: env.Resolver(), records: map[string]protocol.Function{}, codes: map[string]protocol.Code{}, tables: map[string]Table{}}
}

func (o *Owner) Fork(env publication.FunctionEnvironment) publication.FunctionAuthoring {
	return &Owner{env: env, resolver: o.resolver, records: maps.Clone(o.records), codes: maps.Clone(o.codes), tables: maps.Clone(o.tables)}
}

func (o *Owner) Function(name string, version int) (platform.AIFunction, int, bool) {
	return publication.Retained(o.records, o.inventory, o.decode, name, version, functionFamily, protocol.Function.Definition)
}

func (o *Owner) Declarations() []platform.AIFunction {
	out := []platform.AIFunction{}
	for _, name := range slices.Sorted(maps.Keys(o.records)) {
		out = append(out, o.records[name].Definition())
	}
	return out
}

func (o *Owner) Inventory() ([]protocol.Function, error) {
	return o.inventory()
}

func (o *Owner) Entity() platform.Entity {
	return platform.Entity{Type: protocol.FunctionType, Title: "AI function", Plural: "AI functions", Model: protocol.Function{}, Display: "title",
		Description: "A bounded typed inference over one published object; draft edits do not change installed calls.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder"}, Capability: "functions"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Description: "Install this typed function as its next retained version.",
				Roles:       []string{"builder"},
				Capability:  "functions",
				Parameters:  []platform.Field{},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules:       []platform.ActionRule{{Function: o.publish}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (o *Owner) CallEntity() platform.Entity {
	return platform.Entity{Type: protocol.FunctionCallType, Title: "AI function call", Plural: "AI function calls", Model: protocol.FunctionRun{}, Display: "function",
		Description: "A retained typed suggestion or refusal; it never changes its source record.",
		Scope: platform.Scope{Default: platform.ScopeOwn, Owner: "member", Levels: map[string]string{"builder": platform.ScopeTenant}, Participants: func(record any) []string {
			run := record.(protocol.FunctionRun)
			return append([]string{run.Member}, run.Reviewers...)
		}},
		Derived: []platform.Derivation{{From: "sources", Fields: []string{"output", "reason", "question", "sourceValues", "amendments"}}}, Withheld: "withheld"}
}

func (o *Owner) Actions() []platform.Action {
	actions := actioncatalog.EntityActions(o.Entity())
	actions = append(actions, actioncatalog.EntityActions(o.CallEntity())...)
	actions = append(actions, functionCallActions(append(o.env.CallRoles(), platform.AnyMember))...)
	return actions
}

func functionCallActions(roles []string) []platform.Action {
	return append([]platform.Action{{Schema: protocol.SchemaFunctionCall, Target: protocol.FunctionCallType, New: true, Capability: "functions", Title: "Call AI function",
		Description: "Request a published function over a readable source; retain the suggestion for human review.",
		Roles:       slices.Clone(roles), Automation: true,
		Payload: []platform.Field{{Name: "question", Type: "string", Description: "Bounded member question for a conversation function"}, {Name: "history", Type: "json", Description: "Up to eight prior completed calls in this conversation"}, {Name: "app", Type: "string", Description: "Registered function owner"}, {Name: "name", Type: "string", Required: true, Description: "Published function name"},
			{Name: "source", Type: "string", Required: true, Description: "Source record ID"},
			{Name: "version", Type: "integer", Description: "Published version; zero selects the installed version"},
			{Name: "release", Type: "string", Description: "Retained release for a native automation; empty keeps a development run"},
			{Name: "onBehalf", Type: "string", Description: "Retained member for a native automation"}}},
		{Schema: protocol.SchemaFunctionAnswer, Target: protocol.FunctionCallType, Capability: "functions", Title: "Record AI function answer",
			Description: "Keep the validated suggestion or refusal on its call record.", Automation: true, Payload: platform.FunctionAnswerFields()}}, platform.Action{Schema: protocol.SchemaProposalReview, Target: protocol.FunctionCallType, Capability: "functions", Title: "Review AI proposal", Description: "Reject or expire a retained proposal without changing its business source.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "decision", Type: "string", Required: true, Choices: []string{"rejected", "expired"}, Description: "Proposal decision"}}})
}

func (o *Owner) validate(f protocol.Function) (protocol.Function, error) {
	if !publication.ValidName(f.Name) {
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

func (o *Owner) check(f protocol.Function) (protocol.Function, error) {
	f, err := o.validate(f)
	if err != nil {
		return f, err
	}
	list, err := o.inventory()
	if err != nil {
		return f, err
	}
	for _, other := range list {
		installed, _ := publication.Read[protocol.Function](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return f, fmt.Errorf("AI function %s is already declared", f.Name)
		}
	}
	if old, ok := publication.Read[protocol.Function](f.Published); ok && (old.Name != f.Name || old.Object != f.Object) {
		return f, fmt.Errorf("a published function keeps its name and source object")
	}
	return f, nil
}

func (o *Owner) publish(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	var f *protocol.Function
	switch value := record.(type) {
	case *protocol.Function:
		f = value
	default:
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	bound, err := o.check(*f)
	if err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	*f = bound
	return publication.PublishRetained(c, f, func(f *protocol.Function) publication.Family { return functionFamily(*f) }, setFamily, "An AI function may retain at most 64 published versions", func(f *protocol.Function) string { return functionImage(*f) }, o.installRecord)
}

func (o *Owner) installRecord(c platform.Caller, f protocol.Function) error {
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
	return protocol.FunctionType, functionFromDeclaration(definition), nil
}

func (o *Owner) assetVersion(asset platform.ReleaseAsset) int {
	return o.resolver.DefinitionAssetVersion(asset, o.env.App(), o.env.Version(), platform.AssetFunction, "function")
}

func (o *Owner) fromAsset(asset platform.ReleaseAsset) (protocol.Function, error) {
	var zero protocol.Function
	version := o.assetVersion(asset)
	var d platform.AIFunction
	if version == 0 || strictDecode(asset.Body, &d) != nil || asset.ContractVersion != 1 || d.Check() != nil || d.Name != asset.Ref.Name {
		return zero, fmt.Errorf("invalid builder function asset %s", asset.Ref)
	}
	f := functionFromDeclaration(d)
	family := functionFamily(f)
	family.Version, family.State = version, "published"
	setFamily(&f, family)
	return f, nil
}

func (o *Owner) decode(raw []byte) (protocol.Function, error) {
	return publication.Decode(raw, "function", functionFamily, func(f protocol.Function) error { return f.Definition().Check() }, func(old, next protocol.Function) bool {
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
	var record protocol.Function
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	for _, version := range record.Versions {
		f, ok := publication.Read[protocol.Function](version)
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
	return publication.InstalledAssets(o.records, functionFamily, o.releaseAsset)
}

func (o *Owner) releaseAsset(f protocol.Function) (platform.ReleaseAsset, error) {
	ref := platform.AssetRef{App: o.env.App(), Kind: platform.AssetFunction, Name: f.Name}
	return o.resolver.ReleaseAsset(ref, o.env.Version()+".function-"+fmt.Sprint(f.Version), f.Definition())
}

func (o *Owner) Resolve(name, sourceVersion string) (platform.ReleaseAsset, error) {
	return publication.RetainedAsset(o.resolver, o.env.App(), o.env.Version(), "function", "AI function", platform.AssetFunction, name, sourceVersion, o.Function)
}

func (o *Owner) Prepare(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return publication.Prepare(o.env.App(), o.env.Version(), assets, platform.AssetFunction, "function", protocol.SchemaFunction, o.inventory, functionFamily, setFamily, o.Resolve,
		func(record protocol.Function, asset platform.ReleaseAsset, version int) (protocol.Function, error) {
			family := functionFamily(record)
			prior, retained := publication.Read[protocol.Function](family.Published)
			var d platform.AIFunction
			if version != family.Version+1 || strictDecode(asset.Body, &d) != nil || asset.ContractVersion != 1 || d.Check() != nil || o.resolver.CheckAssetDependencies(asset) != nil {
				return record, fmt.Errorf("invalid next function version")
			}
			frozen := functionFromDeclaration(d)
			if frozen.Name != record.Name || frozen.Object != record.Object || retained && prior.Object != frozen.Object {
				return record, fmt.Errorf("invalid next function version")
			}
			frozen.Record = record.Record
			kept := functionFamily(frozen)
			kept.State, kept.Version = "published", version
			setFamily(&frozen, kept)
			family.State, family.Version, family.Published = "published", version, functionImage(frozen)
			family.Versions = append(slices.Clone(family.Versions), family.Published)
			setFamily(&record, family)
			return record, nil
		})
}

func (o *Owner) DraftAssets(id string, releaseAssets func() ([]platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = releaseAssets()
	if err != nil {
		return
	}
	records, err := o.inventory()
	if err != nil {
		return
	}
	i := slices.IndexFunc(records, func(record protocol.Function) bool { return record.ID == id && !record.Archived })
	if i < 0 {
		err = fmt.Errorf("draft function %q not found", id)
		return
	}
	record := records[i]
	family := functionFamily(record)
	if old, ok := publication.Read[protocol.Function](family.Published); ok {
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
	if asset, ok := o.env.JointDeclaration(platform.AssetRef{App: o.env.App(), Kind: platform.AssetFunction, Name: name}); ok {
		return o.definitionFromAsset(name, version, asset)
	}
	return o.Function(name, version)
}

func (o *Owner) definitionFromAsset(name string, version int, asset platform.ReleaseAsset) (platform.AIFunction, int, bool) {
	ordinal := o.assetVersion(asset)
	if ordinal == version && version > 0 {
		var d platform.AIFunction
		valid := json.Unmarshal(asset.Body, &d) == nil && asset.ContractVersion == 1 && functionFromDeclaration(d).Name == name && d.Check() == nil
		return d, ordinal, valid
	}
	return o.Function(name, version)
}

func (o *Owner) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	switch s.GetSchema().GetName() {
	case protocol.SchemaFunctionCall, protocol.SchemaFunctionAnswer:
		return o.submitCall(c, s, now)
	case protocol.SchemaProposalReview:
		return o.submitProposalReview(c, s, now)
	}
	return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (o *Owner) submitCall(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		if !c.Staging() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, platform.FunctionNeedsDecision)
		}
		id := s.GetTarget().GetId()
		run, exists := platform.Get[protocol.FunctionRun](c, id)
		if s.GetSchema().GetName() == protocol.SchemaFunctionCall {
			if exists {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			var request platform.FunctionRequest
			if json.Unmarshal(s.GetPayload(), &request) != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			request.Reply = protocol.SchemaFunctionAnswer
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
				c.Put(r, protocol.FunctionRun{TrainingAuthorization: authorization, Question: request.Question, History: slices.Clone(request.History), App: cmp.Or(request.App, o.env.App()), Contract: definition, Record: platform.Record{ID: id}, Function: request.Name, Version: call.Version, Member: member,
					SourceValues: call.SourceValues, SourceRevision: call.SourceRevision, Applications: call.Applications, Source: call.Source, State: "pending", Definition: call.Definition, Dependencies: call.Dependencies,
					Model: call.Model, InputHash: call.InputHash, Release: call.Release, Sources: call.Sources})
			}, nil
		}
		var answer platform.Answer
		if !exists || run.State != "pending" || json.Unmarshal(s.GetPayload(), &answer) != nil || answer.Call != id ||
			answer.Outcome != "accepted" && answer.Outcome != "refused" {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		run.State, run.Code, run.Reason = "rejected", answer.Code, answer.Reason
		run.Metered, run.TokensReported = answer.Metered, answer.TokensReported
		run.InputTokens, run.OutputTokens = answer.InputTokens, answer.OutputTokens
		run.CostReported, run.CostUSD = answer.CostReported, answer.CostUSD
		run.LatencyMillis, run.ServedModel = answer.LatencyMillis, answer.ServedModel
		if answer.Outcome == "accepted" {
			if run.Contract.ValidateOutput([]byte(answer.Text)) != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
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
					if err := c.Assign(r, platform.Assignment{Title: "Review AI proposal", Ref: protocol.FunctionCallType + "/" + run.ID, To: []platform.Recipient{recipient}, Due: run.ExpiresAt, Key: key}); err != nil {
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

func (o *Owner) submitProposalReview(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return o.env.Ledger().Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		raw, err := c.ReadRecord(protocol.FunctionCallType, s.GetTarget().GetId(), now)
		var visible protocol.FunctionRun
		run, ok := platform.Get[protocol.FunctionRun](c, s.GetTarget().GetId())
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
				fact := platform.BusinessRefusalFact{Level: "rejected", Code: "AI_PROPOSAL_REJECTED", Message: "The reviewer rejected the AI proposal", Translations: map[string]string{"zh-CN": "审核人驳回了 AI 提议"}, Subject: protocol.FunctionCallType + "/" + run.ID, Action: s.GetSchema().GetName(), Member: c.ID, At: now.UTC(), Original: raw, Inputs: map[string]json.RawMessage{"decision": platform.Raw(input.Decision)}, Record: protocol.FunctionCallType + "/" + run.ID}
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

func NeedsHandover(run protocol.FunctionRun) bool {
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
		prefix := protocol.FunctionCallType + "/"
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		var run protocol.FunctionRun
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

func (o *Owner) inventory() ([]protocol.Function, error) {
	return publication.Inventory[protocol.Function](o.env.Host().Automation(platform.Caller{}, o.env.App()))
}
