package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

// CompleteBlueprintAssets uses exactly the ordinary object publisher's
// generated actions/pages. Explicit owner-produced pages retain precedence.
func (b *Build) CompleteBlueprintAssets(input []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	var objects []Object
	for _, asset := range input {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetObject {
			var object Object
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				return nil, fmt.Errorf("blueprint object %s: %w", asset.Ref, err)
			}
			if TypeOf(object.Name) != asset.Ref.Name {
				return nil, fmt.Errorf("invalid blueprint object %s", asset.Ref)
			}
			object.ID = object.Name
			object.Published = published(object)
			objects = append(objects, object)
		}
	}
	generated, err := releaseAssets(objects, nil, nil, nil, b.Manifest().Version)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(input)
	seen := map[platform.AssetRef]bool{}
	for _, asset := range input {
		seen[asset.Ref] = true
	}
	for _, asset := range generated {
		if !seen[asset.Ref] {
			out = append(out, asset)
		}
	}
	return out, nil
}

// RootDrafts resolves portable logical roots to the same saved drafts the
// existing joint review and simulation already use.
func (b *Build) RootDrafts(roots []platform.AssetRef) ([]JointDraftRef, error) {
	objects, _, _, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	var out []JointDraftRef
	seen := map[JointDraftRef]bool{}
	for _, root := range roots {
		id := ""
		if root.App != ID {
			return nil, fmt.Errorf("test root %s has no Build draft", root)
		}
		switch root.Kind {
		case platform.AssetObject:
			for _, o := range objects {
				if !o.Archived && TypeOf(o.Name) == root.Name {
					if id != "" {
						return nil, fmt.Errorf("test root %s is ambiguous", root)
					}
					id = o.ID
				}
			}
		case platform.AssetFlow:
			for _, p := range processes {
				if !p.Archived && TypeOf(p.Name) == root.Name {
					if id != "" {
						return nil, fmt.Errorf("test root %s is ambiguous", root)
					}
					id = p.ID
				}
			}
		default:
			return nil, fmt.Errorf("test roots use objects or workflows")
		}
		if id == "" {
			return nil, fmt.Errorf("test root %s has no saved draft", root)
		}
		selected := []JointDraftRef{{Kind: root.Kind, ID: id}}
		dependencies, err := b.ReferencedDrafts(root.Kind, id)
		if err != nil {
			return nil, err
		}
		selected = append(selected, dependencies...)
		for _, draft := range selected {
			if !seen[draft] {
				seen[draft] = true
				out = append(out, draft)
			}
		}
	}
	if len(out) == 0 || len(out) > JointDraftLimit {
		return nil, fmt.Errorf("test roots need one to %d saved drafts", JointDraftLimit)
	}
	return out, nil
}

func portableConnectionDraft(asset platform.ReleaseAsset) (Connection, error) {
	var definition ConnectionDefinition
	if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
		return Connection{}, err
	}
	// Site addresses, credentials and concrete protocol settings are filled in
	// this environment before use, never copied from a package.
	return Connection{Name: definition.Name, Title: definition.Title, Kind: definition.Kind, Marking: definition.Marking, State: "draft"}, nil
}

const SchemaProposalReview = FunctionCallType + ".review"

type proposalConsumption struct {
	run        FunctionRun
	binding    platform.ProposalAdoption
	amendments json.RawMessage
}

func checkActionAdoption(a Action) error {
	if a.Adoption == nil {
		return nil
	}
	if a.Transaction == nil {
		return fmt.Errorf("Proposal adoption needs the original business transaction")
	}
	if err := a.Adoption.Check(); err != nil {
		return err
	}
	if a.Adoption.Function.App != "" && a.Adoption.Function.App != ID {
		return fmt.Errorf("Proposal adoption cannot cross record authorities")
	}
	input := slices.IndexFunc(a.Inputs, func(in Input) bool { return in.Name == a.Adoption.Input })
	if input < 0 || a.Inputs[input].Type != "reference" || a.Inputs[input].Ref != FunctionCallType || !a.Inputs[input].Required {
		return fmt.Errorf("Adoption needs a required reference input to the retained function call")
	}
	for _, mapping := range a.Adoption.Mapping {
		if !slices.ContainsFunc(a.Inputs, func(in Input) bool { return in.Name == mapping.Input }) {
			return fmt.Errorf("Proposal mapping names an undeclared action input")
		}
	}
	return nil
}

func (b *Build) prepareProposal(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) (*proposalConsumption, *kernel.Error) {
	entity := b.installed[txn.Subject]
	if entity.Lifecycle == nil {
		return nil, nil
	}
	name := strings.TrimPrefix(r.GetSubmission().GetSchema().GetName(), txn.Subject+".")
	var action *platform.Transition
	for i := range entity.Lifecycle.Transitions {
		if entity.Lifecycle.Transitions[i].Name == name {
			action = &entity.Lifecycle.Transitions[i]
			break
		}
	}
	if action == nil || action.Adoption == nil {
		return nil, nil
	}
	fail := func(message string) (*proposalConsumption, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, message)
	}
	if c.Simulating() {
		return fail("Proposal adoption does not execute in isolated trials")
	}
	if c.Agent && action.Approval != nil {
		return fail("A checkpointed proposal must be adopted through its human approval path")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(r.GetSubmission().GetPayload(), &values) != nil {
		return fail("Adoption inputs are invalid")
	}
	binding := *action.Adoption
	var id string
	if json.Unmarshal(values[binding.Input], &id) != nil || id == "" {
		return fail("Choose a retained proposal for this action")
	}
	visible, problem := c.ReadRecord(FunctionCallType, id, now)
	var readable FunctionRun
	if problem != nil || json.Unmarshal(visible, &readable) != nil || readable.Withheld || readable.Output == "" {
		return fail("The proposal and its current sources must be readable")
	}
	run, ok := platform.Get[FunctionRun](c, id)
	if !ok || run.State != "ready" || run.Contract.Proposal == nil || run.Decision != "pending" || run.Source != txn.Subject+"/"+r.GetSubmission().GetTarget().GetId() || run.Function != binding.Function.Name || cmp.Or(run.App, ID) != cmp.Or(binding.Function.App, ID) || binding.Function.Version != 0 && run.Version != binding.Function.Version {
		return fail("The proposal is not pending for this action and source")
	}
	if run.ExpiresAt.IsZero() || !now.Before(run.ExpiresAt) {
		return fail("The proposal has expired")
	}
	if len(run.SourceValues) != len(run.Contract.Fields) {
		return fail("The proposal has no accepted input evidence")
	}
	for _, field := range run.Contract.Fields {
		current, _, problem := c.ReadRecordPath(txn.Subject, r.GetSubmission().GetTarget().GetId(), []string{field}, now)
		if problem != nil || string(current) != string(run.SourceValues[field]) {
			return fail("The proposal source changed; request a new proposal")
		}
	}

	var output map[string]json.RawMessage
	if run.Contract.ValidateOutput([]byte(run.Output)) != nil || json.Unmarshal([]byte(run.Output), &output) != nil {
		return fail("The retained proposal output is invalid")
	}
	policy := run.Contract.Proposal
	if proposalNeedsHandover(run) && !c.Holds(ID, policy.HandoffRole) {
		checkpoint := action.Approval != nil && slices.ContainsFunc(action.Approval.Levels, func(level platform.ApprovalLevel) bool { return level.AppRole == policy.HandoffRole })
		if !checkpoint {
			return fail("This proposal needs its designated reviewer or approval checkpoint")
		}
	}
	if err := binding.CheckMapping(run.Contract, action.Payload); err != nil {
		return fail(err.Error())
	}
	amendments := map[string]json.RawMessage{}
	for _, mapping := range binding.Mapping {
		if string(values[mapping.Input]) != string(output[mapping.Output]) {
			amendments[mapping.Input] = values[mapping.Input]
		}
	}
	return &proposalConsumption{run: run, binding: binding, amendments: platform.Raw(amendments)}, nil
}
func proposalNeedsHandover(run FunctionRun) bool {
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
func (b *Build) finishProposal(c platform.Caller, r *pb.ChangeRecord, p *proposalConsumption, now time.Time) *kernel.Error {
	if p == nil {
		return nil
	}
	run := p.run
	run.Decision = "adopted"
	run.DecidedBy = c.ID
	run.DecidedAt = now
	run.AdoptedAction = r.GetSubmission().GetSchema().GetName()
	run.AdoptedChange = r.GetChangeId()
	authorization, _ := platform.CanonicalDigest([]any{c.ID, c.Tenant, c.Roles, c.Grants, c.Scopes, c.Status})
	if run.TrainingAuthorization != authorization {
		run.TrainingAuthorization = ""
	}
	run.TrainingSources = map[string]uint32{}
	for _, ref := range run.Sources {
		typ, id, ok := strings.Cut(strings.Split(ref, "#")[0], "/")
		if !ok {
			continue
		}
		raw, problem := c.ReadRecord(typ, id, now)
		if problem != nil {
			continue
		}
		var header struct {
			Revision uint32 `json:"revision"`
		}
		_ = json.Unmarshal(raw, &header)
		run.TrainingSources[strings.Split(ref, "#")[0]] = header.Revision
	}

	run.Amendments = p.amendments
	if proposalNeedsHandover(run) {
		if err := c.Assign(r, platform.Assignment{Key: "proposal:" + run.ID, Close: true}); err != nil {
			return err
		}
	}
	return c.Put(r, run)
}

// Read provenance uses the original entity compute and derivation paths.
// It is accepted with each business write, before its original validation.
func proposalEntity(e platform.Entity, info platform.EntityInfo, changed map[string]any, p *proposalConsumption, extra []string) (platform.Entity, map[string]any) {
	if p == nil {
		return e, changed
	}
	payload := maps.Clone(changed)
	generated := map[string]any{}
	for name, value := range changed {
		if field, ok := info.Field(name); ok && field.ReadOnly && field.SourceType == "generated" {
			generated[name] = value
			delete(payload, name)
		}
	}
	compute := e.ComputeAt
	e.ComputeAt = func(c platform.Caller, record any, at time.Time) *kernel.Error {
		rv := reflect.ValueOf(record).Elem()
		for name, value := range generated {
			field, _ := info.Field(name)
			if err := assign(rv.FieldByIndex(field.Index), value); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
		if compute != nil {
			if err := compute(c, record, at); err != nil {
				return err
			}
		}
		sources := append(slices.Clone(p.run.Sources), extra...)
		sources = append(sources, FunctionCallType+"/"+p.run.ID+"#output")
		slices.Sort(sources)
		sources = slices.Compact(sources)
		for field := range changed {
			destination := rv.FieldByName("AISources" + GoName(field))
			if destination.IsValid() && destination.CanSet() {
				destination.Set(reflect.ValueOf(sources))
			}
		}
		return nil
	}
	return e, payload
}

func (b *Build) submitProposalReview(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		raw, err := c.ReadRecord(FunctionCallType, s.GetTarget().GetId(), now)
		var visible FunctionRun
		run, ok := platform.Get[FunctionRun](c, s.GetTarget().GetId())
		if err != nil || json.Unmarshal(raw, &visible) != nil || visible.Withheld || !ok || run.Contract.Proposal == nil || run.State != "ready" || run.Decision != "pending" || c.ID != run.Member && !c.Holds(ID, run.Contract.Proposal.HandoffRole) && !c.Holds(ID, Builder) {
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
			if proposalNeedsHandover(run) {
				if err := c.Assign(r, platform.Assignment{Key: "proposal:" + run.ID, Close: true}); err != nil {
					return
				}
			}
			c.Put(r, run)
		}, nil
	})
}

func checkAdoptionSources(o Object, a Action, lookup func(string) (platform.EntityInfo, bool)) error {
	if a.Adoption == nil || a.Transaction == nil {
		return nil
	}
	mapped := map[string]bool{}
	for _, m := range a.Adoption.Mapping {
		mapped[m.Input] = true
	}
	taintedSteps := map[string]bool{}
	consumesRef := func(ref string) bool {
		if strings.HasPrefix(ref, "input.") {
			return mapped[strings.TrimPrefix(ref, "input.")]
		}
		if step, ok := strings.CutPrefix(ref, "step."); ok {
			name, _, _ := strings.Cut(step, ".")
			return taintedSteps[name]
		}
		return mapped[ref]
	}
	var consumes func(platform.Expr) bool
	consumes = func(e platform.Expr) bool {
		if consumesRef(e.Ref) {
			return true
		}
		for _, arg := range e.Args {
			if consumes(arg) {
				return true
			}
		}
		return e.In != nil && consumes(*e.In)
	}
	var problem error
	platform.WalkBusinessTxn(a.Transaction, func(step platform.TxnStep) {
		for _, change := range step.Changes {
			if !consumesRef(change.From) && (change.Expr == nil || !consumes(*change.Expr)) {
				continue
			}
			taintedSteps[step.Name] = true
			source := ""
			if step.Target.Type == TypeOf(o.Name) {
				for _, field := range o.Fields {
					if field.Name == change.Field {
						source = field.SourceType
						break
					}
				}
			} else if info, ok := lookup(step.Target.Type); ok {
				if field, ok := info.Field(change.Field); ok {
					source = field.SourceType
				}
			}
			if source != "generated" {
				problem = fmt.Errorf("AI proposal destinations must declare generated provenance")
			}
		}
	})
	return problem
}

// CodeDeclarations checks references between installed declarations. Every
// input is a value supplied by its existing owner; this checker cannot read
// tenant state, install definitions, execute callbacks or grant permissions.
type CodeDeclarations struct {
	Manifests       []platform.Manifest
	Entities        []platform.EntityInfo
	DataClasses     map[string]bool
	NarrowedFields  map[string][]string
	NativeExecutors map[string]bool
}

func (c CodeDeclarations) Check() error {
	objects := map[string]platform.AssetRef{}
	infos := map[string]platform.EntityInfo{}
	for _, info := range c.Entities {
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
		infos[info.Type] = info
	}
	lookup := func(typ string) (platform.EntityInfo, bool) { info, ok := infos[typ]; return info, ok }
	for _, info := range c.Entities {
		for _, field := range info.Fields {
			if field.Ref == "" {
				continue
			}
			if field.Type != "reference" && field.Type != "references" {
				return fmt.Errorf("asset %s field %s has reference %s with incompatible type %s", objects[info.Type], field.Name, field.Ref, field.Type)
			}
			// Enterprise identities belong to the directory. An optional reference
			// remains declarable before that owner is mounted.
			if field.Ref == "enterprise.element" {
				continue
			}
			if _, ok := objects[field.Ref]; !ok {
				return fmt.Errorf("asset %s field %s requires missing object %s", objects[info.Type], field.Name, field.Ref)
			}
		}
	}
	actions := map[platform.AssetRef]platform.Action{}
	for _, m := range c.Manifests {
		for _, action := range m.Actions.All() {
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetAction, Name: action.Schema}
			if _, ok := objects[action.Target]; !ok && !c.DataClasses[action.Target] {
				return fmt.Errorf("asset %s targets missing data class %s", ref, action.Target)
			}
			for _, field := range action.Payload {
				if field.Ref == "" {
					continue
				}
				if field.Type != "string" {
					return fmt.Errorf("asset %s payload %s has reference %s with incompatible type %s", ref, field.Name, field.Ref, field.Type)
				}
				if field.Ref == "enterprise.element" {
					continue
				}
				if _, ok := objects[field.Ref]; !ok {
					return fmt.Errorf("asset %s payload %s requires missing object %s", ref, field.Name, field.Ref)
				}
			}
			actions[ref] = action
		}
	}
	for _, m := range c.Manifests {
		for _, original := range m.Pages {
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetPage, Name: original.Name}
			if original.Format == 0 {
				if original.Layout != "list-detail" && original.Layout != "composed" {
					return fmt.Errorf("asset %s has unsupported layout %q", ref, original.Layout)
				}
				if original.Document != nil && original.Layout != "composed" {
					return fmt.Errorf("asset %s: a structured layout needs composed sections", ref)
				}
				if len(original.Selections) != 0 && len(original.Sections) == 0 {
					return fmt.Errorf("asset %s: selections need composed sections", ref)
				}
				if len(original.Sections) == 0 {
					for _, fields := range [][]string{original.ListFields, original.DetailFields} {
						if len(fields) == 0 {
							return fmt.Errorf("asset %s needs list and detail fields", ref)
						}
						seen := map[string]bool{}
						for _, field := range fields {
							if seen[field] {
								return fmt.Errorf("asset %s repeats field %s", ref, field)
							}
							seen[field] = true
							if _, ok := infos[original.Object.Name].Field(field); !ok {
								return fmt.Errorf("asset %s requires missing field %s on %s", ref, field, original.Object)
							}
						}
					}
				}
			}
			page, err := original.Safety()
			if err != nil {
				return fmt.Errorf("asset %s: %w", ref, err)
			}
			if err = page.Document.Check(page.Sections); err != nil {
				return fmt.Errorf("asset %s: %w", ref, err)
			}
			if page.Object.Kind != platform.AssetObject || objects[page.Object.Name] != page.Object {
				return fmt.Errorf("asset %s requires missing object %s", ref, page.Object)
			}
			boundObjects := map[string]string{"page": page.Object.Name}
			for _, binding := range page.DataBindings {
				object := binding.Object.Name
				if binding.Model != nil {
					object = binding.Model.Type
				}
				if object != "" {
					if _, ok := objects[object]; !ok {
						return fmt.Errorf("asset %s requires missing object %s", ref, object)
					}
				}
				boundObjects[binding.ID] = object
			}
			for _, trigger := range page.ActionTriggers {
				seen := map[platform.AssetRef]bool{}
				for _, actionRef := range trigger.Actions {
					action, ok := actions[actionRef]
					if !ok {
						return fmt.Errorf("asset %s requires missing action %s", ref, actionRef)
					}
					if object := boundObjects[trigger.Binding]; object != "" && action.Target != object {
						return fmt.Errorf("asset %s action %s targets %s, not %s", ref, actionRef, action.Target, object)
					}
					if seen[actionRef] {
						return fmt.Errorf("asset %s repeats an action", ref)
					}
					seen[actionRef] = true
				}
			}
		}
	}
	for _, m := range c.Manifests {
		for _, p := range m.PropertyTypes {
			if err := p.Check(); err != nil {
				return err
			}
		}
		for _, l := range m.LinkTypes {
			parent, ok := infos[l.Parent.Name]
			if !ok {
				return fmt.Errorf("link parent missing")
			}
			child, ok := infos[l.Child.Name]
			if !ok || l.Child.App != m.ID {
				return fmt.Errorf("link child owner differs")
			}
			if err := l.CheckSchema(parent, child); err != nil {
				return err
			}
		}
		for _, q := range m.Queries {
			if q.Interface != "" {
				continue
			} // its original owner binds retained implementers
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetQuery, Name: q.Name}
			if _, ok := objects[q.Object]; !ok {
				return fmt.Errorf("asset %s reads missing object %s", ref, q.Object)
			}
			if q.By != "" {
				if f, ok := infos[q.Object].Field(q.By); !ok || f.Type != "reference" {
					return fmt.Errorf("asset %s is run for %s, not a reference of %s", ref, q.By, q.Object)
				}
			}
			var terms []any
			if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &terms) != nil {
				return fmt.Errorf("asset %s has a domain that is not a list of conditions", ref)
			}
		}
		for _, f := range m.Functions {
			ref := platform.AssetRef{App: m.ID, Kind: platform.AssetFunction, Name: f.Name}
			if err := f.Check(); err != nil {
				return err
			}
			if object, ok := objects[f.Object]; !ok || object.App != m.ID {
				return fmt.Errorf("asset %s needs its owner's source object", ref)
			}
			for _, role := range f.Roles {
				if !slices.Contains(m.AllRoles(), role) {
					return fmt.Errorf("asset %s names an undeclared role", ref)
				}
			}
			for _, name := range f.Fields {
				field, ok := infos[f.Object].Field(name)
				if !ok || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) || slices.Contains(c.NarrowedFields[f.Object], name) {
					return fmt.Errorf("asset %s needs direct scalar source fields", ref)
				}
			}
		}
		for _, o := range m.Operations {
			if err := o.Check(); err != nil {
				return fmt.Errorf("operation %s: %w", o.Name, err)
			}
			for _, role := range o.Roles {
				if !slices.Contains(m.AllRoles(), role) {
					return fmt.Errorf("operation %s names an undeclared role", o.Name)
				}
			}
			if o.Binding.Kind == "native" && !c.NativeExecutors[m.ID] {
				return fmt.Errorf("operation %s has no native executor", o.Name)
			}
		}
	}
	for _, m := range c.Manifests {
		for _, observation := range m.Observations {
			if err := platform.CheckObservationDefinition(observation, lookup); err != nil {
				return err
			}
		}
	}
	return nil
}

// AgentDescriptor seals declaration data without invoking a guard, test or
// handoff callback. The original agent owner remains responsible for running it.
func AgentDescriptor(agent platform.Agent) (platform.AgentReleaseDescriptor, error) {
	cases := []map[string]any{}
	for _, test := range agent.Cases {
		cases = append(cases, map[string]any{"name": test.Name, "goal": test.Goal, "ref": test.Ref, "check": test.Check != nil})
	}
	raw, err := json.Marshal(cases)
	if err != nil {
		return platform.AgentReleaseDescriptor{}, err
	}
	descriptor := platform.AgentReleaseDescriptor{Model: agent.Model, ModelOptions: agent.ModelOptions, Name: agent.Name, Title: agent.Title, Description: agent.Description, Instructions: agent.Instructions, Tools: slices.Clone(agent.Tools), Budget: agent.Budget, Checkpoints: slices.Clone(agent.Checkpoints), Native: true, Guard: agent.Guard != nil, Handoff: agent.To != nil, Cases: raw}
	return descriptor, descriptor.Check()
}

// ProtocolDescriptor binds the protocol's public actions to their original
// provider declarations. No protocol execution or cross-owner call occurs here.
func ProtocolDescriptor(manifest platform.Manifest, provision platform.Provision) (platform.ProtocolReleaseDescriptor, error) {
	protocol := platform.ProtocolReleaseDescriptor{Name: provision.Protocol.Name, Version: provision.Protocol.Version, Actions: slices.Clone(provision.Protocol.Actions), Reads: slices.Clone(provision.Protocol.Reads), ActionBindings: map[string]platform.AssetRef{}, ActionRoles: map[string][]string{}}
	for i, action := range protocol.Actions {
		name, ok := provision.Actions[action.Schema]
		declared, exists := manifest.Actions.Action(name)
		if !ok || !exists {
			return protocol, fmt.Errorf("protocol %s action %s has no provider declaration", protocol.Name, action.Schema)
		}
		protocol.ActionBindings[action.Schema] = platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: name}
		protocol.Actions[i].Roles = slices.Clone(declared.Roles)
		protocol.ActionRoles[action.Schema] = slices.Clone(declared.Roles)
	}
	return protocol, nil
}
