package agents

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type Agent struct {
	platform.Record
	Name         string                `json:"name" field:"required,search" help:"Lower-case letters and digits; the agent is build.<name>"`
	Title        string                `json:"title" field:"required,search"`
	Description  string                `json:"description" field:"required" type:"longtext" help:"What it does for others; people and other agents see this"`
	Instructions string                `json:"instructions" field:"required" type:"longtext" help:"Its private instructions: the job, the order of work, when to ask"`
	Model        string                `json:"model,omitempty" field:"aside"`
	ModelOptions platform.ModelOptions `json:"modelOptions,omitzero" field:"aside" type:"json"`
	Tools        []string              `json:"tools" field:"aside" help:"Beyond what its objects, actions and functions derive for it: extra queries (query:<name>)"`
	Steps        int                   `json:"steps,omitempty" title:"Model turns per run" help:"Default 10"`
	Tokens       int                   `json:"tokens,omitempty" title:"Tokens per run" help:"Default 40000"`
	Actions      int                   `json:"actions,omitempty" title:"Actions per run" help:"Default 3"`
	Cost         float64               `json:"cost,omitempty" title:"USD per run" help:"0: no cap"`
	Checkpoints  []string              `json:"checkpoints,omitempty" field:"aside" help:"Tools that always wait for a person to confirm"`
	HandoffRole  string                `json:"handoffRole,omitempty" title:"Hands over to" help:"The builder-app role that takes the goal when a run stops; empty: the builder"`
	Cases        []AgentCase           `json:"cases,omitempty" type:"json" title:"Evaluation cases"`
	State        string                `json:"state" field:"readonly" choices:"draft,published"`
	Version      int                   `json:"version,omitempty" field:"readonly"`
	Published    string                `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions     []string              `json:"versions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type AgentCase struct {
	Name   string   `json:"name"`
	Goal   string   `json:"goal"`
	Ref    string   `json:"ref,omitempty" help:"The record it is about, <type>/<id>"`
	Expect string   `json:"expect,omitempty" help:"Text the result must contain"`
	Asks   bool     `json:"asks,omitempty" help:"It must ask a person"`
	Takes  []string `json:"takes,omitempty" help:"Actions it must take"`
	Avoids []string `json:"avoids,omitempty" help:"Actions it must not take"`
}

type builderAgentOwner struct {
	env    platform.AgentAuthoringEnvironment
	agents map[string]Agent
}

func BuilderPublication(env platform.AgentAuthoringEnvironment) platform.AgentAuthoringBinding {
	return (&builderAgentOwner{env: env, agents: map[string]Agent{}}).binding()
}

func (o *builderAgentOwner) binding() platform.AgentAuthoringBinding {
	return platform.AgentAuthoringBinding{
		Fork: func(env platform.AgentAuthoringEnvironment) platform.AgentAuthoringBinding {
			return (&builderAgentOwner{env: env, agents: maps.Clone(o.agents)}).binding()
		},
		Entity: o.entity, DraftEntity: o.draftEntity, Stamps: o.stamps, Declarations: o.declarations,
		Version: o.version, Published: o.published, DraftAsset: o.draftAsset, DraftFromAsset: o.draftFromAsset,
		Assets: o.assets, Resolve: o.resolve, InstallAsset: o.installAsset, Restore: o.restore,
		ValidateImage: o.validateImage, InstallImage: o.installImage, ReadFrozen: o.readFrozen,
		FreezeBatch: o.preparePublications, VersionedDraft: o.draftAssets, LoadExecution: o.loadExecution,
	}
}

func (o *builderAgentOwner) inventory() ([]Agent, error) {
	rows, err := o.env.Inventory(reflect.TypeFor[Agent]())
	if err != nil {
		return nil, err
	}
	var all []Agent
	for _, row := range rows {
		all = append(all, row.(Agent))
	}
	return all, nil
}

func (o *builderAgentOwner) read(raw string) (Agent, bool) {
	var record Agent
	ok := o.env.Read(raw, &record)
	return record, ok
}

func (o *builderAgentOwner) definition(a Agent) platform.Agent {
	checkpoints := slices.Clone(a.Checkpoints)
	role := a.HandoffRole
	builder := o.env.Builder
	d := platform.Agent{Model: a.Model, ModelOptions: a.ModelOptions, Name: a.Name, Title: a.Title, Description: a.Description, Instructions: a.Instructions, Tools: slices.Clone(a.Tools), Checkpoints: checkpoints,
		Budget: platform.Budget{Steps: a.Steps, Tokens: a.Tokens, Actions: a.Actions, Cost: a.Cost},
		Guard: func(c platform.Caller, r platform.AgentRun, action, target string, payload json.RawMessage) *platform.Error {
			if slices.Contains(checkpoints, action) && r.OnBehalf == "" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This step waits for a person: ask, or finish with what you propose.")
			}
			return nil
		},
		To: func(platform.Caller, platform.AgentRun) []platform.Recipient {
			if role == "" {
				return []platform.Recipient{{AppRole: builder}}
			}
			return []platform.Recipient{{AppRole: role}}
		}}
	for _, c := range a.Cases {
		c := c
		d.Cases = append(d.Cases, platform.Case{Name: c.Name, Goal: c.Goal, Ref: c.Ref, Check: func(run platform.CaseRun) string {
			if c.Expect != "" && !strings.Contains(strings.ToLower(run.Result), strings.ToLower(c.Expect)) {
				return fmt.Sprintf("the result does not say %q", c.Expect)
			}
			if c.Asks && !run.Asked {
				return "it did not ask a person"
			}
			taken := func(action string) bool {
				return slices.ContainsFunc(run.Actions, func(x string) bool { return strings.HasPrefix(x, action+" ") || x == action })
			}
			for _, x := range c.Takes {
				if !taken(x) {
					return "it did not take " + x
				}
			}
			for _, x := range c.Avoids {
				if taken(x) {
					return "it took " + x
				}
			}
			return ""
		}})
	}
	return d
}

func (o *builderAgentOwner) entity() platform.Entity {
	return platform.Entity{Type: o.env.Type, Title: "Agent", Plural: "Agents", Model: Agent{}, Display: "title",
		Description: "An AI agent over the ontology: bounded tools, a budget, checkpoints and cases; publishing installs it.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{o.env.Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{o.env.Builder}, Capability: "functions"},
		Validate: func(c platform.Caller, record any) *platform.Error {
			if err := o.checkAgent(*record.(*Agent)); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Actions: []platform.ActionType{{Name: "publish", Title: "Publish", Description: "Install this agent as its next version; a changed definition stops existing runs.", Roles: []string{o.env.Builder}, Capability: "functions", Parameters: []platform.Field{},
				Criteria: platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules:    []platform.ActionRule{{Function: o.publish}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (o *builderAgentOwner) draftEntity() platform.Entity {
	entity := o.entity()
	entity.Validate = func(c platform.Caller, record any) *platform.Error {
		if c.Replaying {
			return nil
		}
		tools, roles, err := o.env.DraftTools(c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		if err := o.checkAgentWithDrafts(*record.(*Agent), tools, roles); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		return nil
	}
	return entity
}

func (o *builderAgentOwner) checkAgent(a Agent) error {
	var roles []string
	tools := map[string]bool{}
	for _, model := range o.env.JointEntities() {
		for role := range model.Scope.Levels {
			roles = append(roles, role)
		}
		for _, standard := range model.Standard {
			tools[standard] = true
		}
		if model.Lifecycle != nil {
			for _, transition := range model.Lifecycle.Transitions {
				tools[transition.Schema] = true
			}
		}
	}
	for ref := range o.env.JointDeclarations() {
		if ref.App == o.env.App && ref.Kind == platform.AssetQuery {
			tools["query:"+ref.Name] = true
		}
	}
	return o.checkAgentWithDrafts(a, tools, roles)
}

func (o *builderAgentOwner) checkAgentWithDrafts(a Agent, tools map[string]bool, roles []string) error {
	if !o.env.ValidName(a.Name) {
		return fmt.Errorf("The agent name %s must be lower-case letters and digits", a.Name)
	}
	if strings.TrimSpace(a.Instructions) == "" || strings.TrimSpace(a.Description) == "" {
		return fmt.Errorf("The agent %s needs a description and instructions", a.Name)
	}
	known := func(tool string) error {
		if tools[tool] {
			return nil
		}
		if query, ok := strings.CutPrefix(tool, "query:"); ok {
			if o.env.JointAssets()[platform.AssetRef{App: o.env.App, Kind: platform.AssetQuery, Name: query}] {
				return nil
			}
			if !o.env.QueryExists(query) {
				return fmt.Errorf("The agent %s uses query %s, which is not published", a.Name, query)
			}
			return nil
		}
		if o.env.JointAssets()[platform.AssetRef{App: o.env.App, Kind: platform.AssetAction, Name: tool}] {
			return nil
		}
		if _, ok := o.env.Authority().Catalog().Action(tool); !ok || !strings.HasPrefix(tool, o.env.App+".") || o.env.InstalledAction(tool) == "" {
			return fmt.Errorf("The agent %s uses %s, which is not an action of a published object", a.Name, tool)
		}
		return nil
	}
	for _, tool := range a.Tools {
		if err := known(tool); err != nil {
			return err
		}
	}
	for _, cp := range a.Checkpoints {
		if known(cp) != nil {
			return fmt.Errorf("The checkpoint %s is not an action of a published object", cp)
		}
	}
	if a.HandoffRole != "" && !slices.Contains(roles, a.HandoffRole) && !o.env.DeclaresRole(a.HandoffRole) {
		return fmt.Errorf("The role %s is not a role of this builder", a.HandoffRole)
	}
	for i, c := range a.Cases {
		if strings.TrimSpace(c.Goal) == "" {
			return fmt.Errorf("Case %d needs a goal", i+1)
		}
		for _, x := range append(slices.Clone(c.Takes), c.Avoids...) {
			if known(x) != nil {
				return fmt.Errorf("Case %q names %s, not an action of a published object", c.Name, x)
			}
		}
	}
	for name, other := range o.agents {
		if name == a.Name && other.ID != a.ID {
			return fmt.Errorf("Agent %s is already declared", a.Name)
		}
	}
	if old, ok := o.read(a.Published); ok && old.Name != a.Name {
		return fmt.Errorf("A published agent keeps its name")
	}
	return nil
}

func (o *builderAgentOwner) publish(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	a, ok := record.(*Agent)
	if !ok {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := o.checkAgent(*a); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if a.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An agent may retain at most 64 published versions")
	}
	if a.Published != "" && !slices.Contains(a.Versions, a.Published) {
		a.Versions = append(slices.Clone(a.Versions), a.Published)
	}
	a.Version++
	a.State = "published"
	a.Published = string(platform.Raw(*a))
	a.Versions = append(a.Versions, a.Published)
	if !c.Staging() {
		o.agents[a.Name] = *a
	}
	return nil
}

func (o *builderAgentOwner) stamps() []string {
	var names []string
	for name, a := range o.agents {
		names = append(names, fmt.Sprintf("%s@%d", name, a.Version))
	}
	return names
}

func (o *builderAgentOwner) declarations() []platform.Agent {
	names := slices.Sorted(maps.Keys(o.agents))
	out := make([]platform.Agent, 0, len(names))
	for _, name := range names {
		out = append(out, o.definition(o.agents[name]))
	}
	return out
}

func (o *builderAgentOwner) version(name string) int { return o.agents[name].Version }

func (o *builderAgentOwner) releaseAsset(a Agent, source string) (platform.ReleaseAsset, error) {
	cases, err := json.Marshal(a.Cases)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	if string(cases) == "null" {
		cases = []byte("[]")
	}
	descriptor := platform.AgentReleaseDescriptor{Model: a.Model, ModelOptions: a.ModelOptions, Name: a.Name, Title: a.Title, Description: a.Description, Instructions: a.Instructions, Tools: slices.Clone(a.Tools), Budget: o.definition(a).Budget, Checkpoints: slices.Clone(a.Checkpoints), HandoffRole: a.HandoffRole, Cases: cases}
	if err := descriptor.Check(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return o.env.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.App, Kind: platform.AssetAgent, Name: a.Name}, ContractVersion: 1, SourceVersion: source + ".agent-" + strconv.Itoa(a.Version), Body: body})
}

func (o *builderAgentOwner) decodeAsset(asset platform.ReleaseAsset) (Agent, error) {
	var descriptor platform.AgentReleaseDescriptor
	versionText, found := strings.CutPrefix(asset.SourceVersion, "1.agent-")
	version, err := strconv.Atoi(versionText)
	if !found || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != versionText || asset.Ref.App != o.env.App || asset.Ref.Kind != platform.AssetAgent || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &descriptor) != nil || descriptor.Native || descriptor.Name != asset.Ref.Name || descriptor.Check() != nil || o.env.CheckDependencies(asset) != nil {
		return Agent{}, fmt.Errorf("invalid builder agent asset")
	}
	record := Agent{Model: descriptor.Model, ModelOptions: descriptor.ModelOptions, Name: descriptor.Name, Title: descriptor.Title, Description: descriptor.Description, Instructions: descriptor.Instructions, Tools: slices.Clone(descriptor.Tools), Steps: descriptor.Budget.Steps, Tokens: descriptor.Budget.Tokens, Actions: descriptor.Budget.Actions, Cost: descriptor.Budget.Cost, Checkpoints: slices.Clone(descriptor.Checkpoints), HandoffRole: descriptor.HandoffRole, Version: version, State: "published"}
	if len(descriptor.Cases) > 0 {
		if err := json.Unmarshal(descriptor.Cases, &record.Cases); err != nil {
			return Agent{}, err
		}
	}
	return record, nil
}

func (o *builderAgentOwner) installAsset(asset platform.ReleaseAsset) error {
	record, err := o.decodeAsset(asset)
	if err != nil {
		return err
	}
	if err = o.checkAgent(record); err != nil {
		return err
	}
	o.agents[record.Name] = record
	return nil
}

func (o *builderAgentOwner) loadExecution(asset platform.ReleaseAsset) error {
	record, err := o.decodeAsset(asset)
	if err != nil {
		return err
	}
	o.agents[record.Name] = record
	return nil
}

func (o *builderAgentOwner) draftFromAsset(asset platform.ReleaseAsset) (any, error) {
	record, err := o.decodeAsset(asset)
	if err != nil {
		return nil, err
	}
	record.Version, record.State = 0, "draft"
	return record, nil
}

func (o *builderAgentOwner) draftAsset(c platform.Caller, id string) (platform.ReleaseAsset, error) {
	record, found := platform.Get[Agent](c, id)
	if !found || record.Archived {
		return platform.ReleaseAsset{}, fmt.Errorf("saved agent draft is unavailable")
	}
	record.Version++
	return o.releaseAsset(record, o.env.SourceVersion())
}

func (o *builderAgentOwner) published(c platform.Caller, id string) bool {
	record, _ := platform.Get[Agent](c, id)
	return record.Published != ""
}

func (o *builderAgentOwner) retainedAsset(ref platform.AssetRef, sourceVersion string) (platform.ReleaseAsset, error) {
	rows, err := o.inventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		if record.Archived || record.Name != ref.Name {
			continue
		}
		for _, raw := range append(slices.Clone(record.Versions), record.Published) {
			retained, ok := o.read(raw)
			if !ok {
				continue
			}
			asset, err := o.releaseAsset(retained, o.env.SourceVersion())
			if err == nil && asset.Ref == ref && asset.SourceVersion == sourceVersion {
				return asset, nil
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("agent version is not retained")
}

func (o *builderAgentOwner) resolve(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	for _, record := range o.agents {
		asset, err := o.releaseAsset(record, o.env.SourceVersion())
		if err != nil {
			return platform.ReleaseAsset{}, err
		}
		if asset.Ref == ref && asset.SourceVersion == version {
			return asset, nil
		}
	}
	return o.retainedAsset(ref, version)
}

func (o *builderAgentOwner) assets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, name := range slices.Sorted(maps.Keys(o.agents)) {
		asset, err := o.releaseAsset(o.agents[name], o.env.SourceVersion())
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (o *builderAgentOwner) draftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = o.env.ReleaseAssets()
	if err != nil {
		return
	}
	rows, problem := o.inventory()
	if problem != nil {
		err = problem
		return
	}
	index := slices.IndexFunc(rows, func(a Agent) bool { return a.ID == id && !a.Archived })
	if index < 0 {
		err = fmt.Errorf("saved agent draft is unavailable")
		return
	}
	record := rows[index]
	if previous, ok := o.read(record.Published); ok {
		prior = platform.AssetRef{App: o.env.App, Kind: platform.AssetAgent, Name: previous.Name}
		hadPrior = true
	}
	if err = o.checkAgent(record); err != nil {
		return
	}
	if record.Version >= 64 {
		err = fmt.Errorf("agent version family is full")
		return
	}
	record.Version++
	next = platform.AssetRef{App: o.env.App, Kind: platform.AssetAgent, Name: record.Name}
	asset, problem := o.releaseAsset(record, o.env.SourceVersion())
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (o *builderAgentOwner) preparePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := o.inventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != o.env.App || asset.Ref.Kind != platform.AssetAgent {
			continue
		}
		frozen, err := o.decodeAsset(asset)
		if err != nil {
			return nil, err
		}
		index := slices.IndexFunc(rows, func(a Agent) bool { return !a.Archived && a.Name == frozen.Name })
		if index < 0 {
			return nil, fmt.Errorf("agent has no local draft")
		}
		record := rows[index]
		if frozen.Version <= record.Version {
			retained, err := o.retainedAsset(asset.Ref, asset.SourceVersion)
			if err != nil || !o.env.EqualAsset(retained, asset) {
				return nil, fmt.Errorf("retained agent bytes differ")
			}
		} else if record.Version != 0 && frozen.Version != record.Version+1 {
			return nil, fmt.Errorf("agent needs its next version or retained bytes")
		}
		if record.Published != "" && !slices.Contains(record.Versions, record.Published) {
			record.Versions = append(slices.Clone(record.Versions), record.Published)
		}
		frozen.Record = record.Record
		selected := string(platform.Raw(frozen))
		for _, raw := range record.Versions {
			if old, ok := o.read(raw); ok && old.Version == frozen.Version {
				selected = raw
				break
			}
		}
		if !slices.Contains(record.Versions, selected) {
			record.Versions = append(record.Versions, selected)
		}
		record.Version, record.Published = max(record.Version, frozen.Version), selected
		record.State = "published"
		if frozen.Version < record.Version {
			record.State = "draft"
		}
		image, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ReleasePublication{Schema: o.env.Type + ".publish", Image: image})
	}
	return out, nil
}

func (o *builderAgentOwner) validateImage(image json.RawMessage) error {
	var record Agent
	if err := json.Unmarshal(image, &record); err != nil {
		return err
	}
	agent, ok := o.read(record.Published)
	if !ok {
		return fmt.Errorf("accepted agent has no published definition")
	}
	return o.checkAgent(agent)
}

func (o *builderAgentOwner) installImage(image json.RawMessage) error {
	var record Agent
	if err := json.Unmarshal(image, &record); err != nil {
		return err
	}
	agent, ok := o.read(record.Published)
	if !ok {
		return fmt.Errorf("accepted agent has no published definition")
	}
	o.agents[agent.Name] = agent
	return nil
}

func (o *builderAgentOwner) readFrozen(image json.RawMessage) (json.RawMessage, error) {
	var record Agent
	if err := json.Unmarshal(image, &record); err != nil {
		return nil, err
	}
	return json.RawMessage(record.Published), nil
}

func (o *builderAgentOwner) restore() error {
	rows, err := o.inventory()
	if err != nil {
		return err
	}
	for _, record := range rows {
		if agent, ok := o.read(record.Published); ok && !record.Archived {
			o.agents[agent.Name] = agent
		}
	}
	return nil
}
