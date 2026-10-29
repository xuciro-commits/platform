package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"
)

const AgentType = "build.agent"
const SchemaAgentPublish = AgentType + ".publish"

// Agent is an AI agent a builder declares over the ontology: what
// it is for, its instructions, the published actions and queries it may use,
// its budget, who takes over when it stops, and the cases it must pass. The
// host runs it as any code-declared agent: every step journaled,
// irreversible effects drafted for a person to confirm, evaluations archived
// against its version.
type Agent struct {
	platform.Record
	Name         string                `json:"name" field:"required,search" help:"Lower-case letters and digits; the agent is build.<name>"`
	Title        string                `json:"title" field:"required,search"`
	Description  string                `json:"description" field:"required" type:"longtext" help:"What it does for others; people and other agents see this"`
	Instructions string                `json:"instructions" field:"required" type:"longtext" help:"Its private instructions: the job, the order of work, when to ask"`
	Model        string                `json:"model,omitempty" field:"aside"`
	ModelOptions platform.ModelOptions `json:"modelOptions,omitzero" field:"aside" type:"json"`
	Tools        []string              `json:"tools" field:"aside" help:"Actions of published objects (build.<object>.<action>) and queries (query:<name>) it may use"`
	Steps        int                   `json:"steps,omitempty" title:"Model turns per run" help:"Default 10"`
	Tokens       int                   `json:"tokens,omitempty" title:"Tokens per run" help:"Default 40000"`
	Actions      int                   `json:"actions,omitempty" title:"Actions per run" help:"Default 3"`
	Cost         float64               `json:"cost,omitempty" title:"USD per run" help:"0: no cap"`
	// Checkpoints are the tools whose use always waits for a person, even when
	// the agent could act: the irreversible ones (post, ship, pay).
	Checkpoints []string    `json:"checkpoints,omitempty" field:"aside" help:"Tools that always wait for a person to confirm"`
	HandoffRole string      `json:"handoffRole,omitempty" title:"Hands over to" help:"The builder-app role that takes the goal when a run stops; empty: the builder"`
	Cases       []AgentCase `json:"cases,omitempty" type:"json" title:"Evaluation cases"`
	State       string      `json:"state" field:"readonly" choices:"draft,published"`
	Version     int         `json:"version,omitempty" field:"readonly"`
	Published   string      `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions    []string    `json:"versions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// AgentCase is one test of the agent, as data: a goal about a
// record, and what a dry run must come to.
type AgentCase struct {
	Name   string   `json:"name"`
	Goal   string   `json:"goal"`
	Ref    string   `json:"ref,omitempty" help:"The record it is about, <type>/<id>"`
	Expect string   `json:"expect,omitempty" help:"Text the result must contain"`
	Asks   bool     `json:"asks,omitempty" help:"It must ask a person"`
	Takes  []string `json:"takes,omitempty" help:"Actions it must take"`
	Avoids []string `json:"avoids,omitempty" help:"Actions it must not take"`
}

func (b *Build) agentEntity() platform.Entity {
	return platform.Entity{Type: AgentType, Title: "Agent", Plural: "Agents", Model: Agent{}, Display: "title",
		Description: "An AI agent over the ontology: bounded tools, a budget, checkpoints and cases; publishing installs it.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "functions"},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if err := b.checkAgent(*record.(*Agent)); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this agent as its next version; a changed definition stops existing runs.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "functions", Payload: []platform.Field{}, Do: b.publishAgent}}}}
}

// Only create/edit may refer to readable saved definitions. Publication and
// candidate installation keep checking the original installed/selected tools.
func (b *Build) agentDraftEntity() platform.Entity {
	entity := b.agentEntity()
	entity.Validate = func(c platform.Caller, record any) *kernel.Error {
		if c.Replaying {
			return nil
		}
		objects, err := readDefinitionInventory[Object](c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		queries, err := readDefinitionInventory[Query](c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		tools := map[string]bool{}
		var roles []string
		for _, object := range objects {
			if object.Archived {
				continue
			}
			model := Entity(object)
			for _, action := range objectActions(object, model) {
				tools[action.Schema] = true
			}
			for role := range model.Scope.Levels {
				roles = append(roles, role)
			}
		}
		for _, query := range queries {
			if !query.Archived {
				tools["query:"+query.Name] = true
			}
		}
		if err := b.checkAgentWithDrafts(*record.(*Agent), tools, roles); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		return nil
	}
	return entity
}

func (b *Build) checkAgent(a Agent) error {
	var roles []string
	tools := map[string]bool{}
	for _, model := range b.jointEntities {
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
	for ref := range b.jointDeclarations {
		if ref.App == ID && ref.Kind == platform.AssetQuery {
			tools["query:"+ref.Name] = true
		}
	}
	return b.checkAgentWithDrafts(a, tools, roles)
}

func (b *Build) checkAgentWithDrafts(a Agent, tools map[string]bool, roles []string) error {
	if !Named(a.Name) {
		return fmt.Errorf("The agent name %s must be lower-case letters and digits", a.Name)
	}
	if strings.TrimSpace(a.Instructions) == "" || strings.TrimSpace(a.Description) == "" {
		return fmt.Errorf("The agent %s needs a description and instructions", a.Name)
	}
	if len(a.Tools) == 0 {
		return fmt.Errorf("The agent %s needs at least one action or query", a.Name)
	}
	for _, tool := range a.Tools {
		if tools[tool] {
			continue
		}
		if query, ok := strings.CutPrefix(tool, "query:"); ok {
			if b.jointAssets[platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: query}] {
				continue
			}
			if _, found := b.queries[query]; !found {
				return fmt.Errorf("The agent %s uses query %s, which is not published", a.Name, query)
			}
			continue
		}
		if b.jointAssets[platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: tool}] {
			continue
		}
		if _, ok := b.ledger.Catalog.Action(tool); !ok || !strings.HasPrefix(tool, ID+".") || b.installedActionOf(tool) == "" {
			return fmt.Errorf("The agent %s uses %s, which is not an action of a published object", a.Name, tool)
		}
	}
	for _, cp := range a.Checkpoints {
		if !slices.Contains(a.Tools, cp) {
			return fmt.Errorf("The checkpoint %s is not one of the agent's tools", cp)
		}
	}
	if a.HandoffRole != "" && !slices.Contains(roles, a.HandoffRole) && !b.declaresRole(a.HandoffRole) {
		return fmt.Errorf("The role %s is not a role of this builder", a.HandoffRole)
	}
	for i, c := range a.Cases {
		if strings.TrimSpace(c.Goal) == "" {
			return fmt.Errorf("Case %d needs a goal", i+1)
		}
		for _, x := range append(slices.Clone(c.Takes), c.Avoids...) {
			if !slices.Contains(a.Tools, x) {
				return fmt.Errorf("Case %q names %s, not one of the agent's tools", c.Name, x)
			}
		}
	}
	for name, other := range b.agents {
		if name == a.Name && other.ID != a.ID {
			return fmt.Errorf("Agent %s is already declared", a.Name)
		}
	}
	if old, ok := ReadPublication[Agent](a.Published); ok && old.Name != a.Name {
		return fmt.Errorf("A published agent keeps its name")
	}
	return nil
}

// installedActionOf says which installed object type an action schema belongs to.
func (b *Build) installedActionOf(schema string) string {
	for typ := range b.installed {
		if strings.HasPrefix(schema, typ+".") {
			return typ
		}
	}
	return ""
}

func (b *Build) publishAgent(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	a, ok := record.(*Agent)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkAgent(*a); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if a.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An agent may retain at most 64 published versions")
	}
	if a.Published != "" && !slices.Contains(a.Versions, a.Published) {
		a.Versions = append(slices.Clone(a.Versions), a.Published)
	}
	a.Version++
	a.State = "published"
	a.Published = published(*a)
	a.Versions = append(a.Versions, a.Published)
	if !c.Staging() {
		b.agents[a.Name] = *a
	}
	return nil
}

func (b *Build) agentInventory() ([]Agent, error) {
	return readDefinitionInventory[Agent](b.host.Automation(platform.Caller{}, ID))
}

// AgentStamp changes whenever the set of installed agents or any of their
// versions does; the host re-declares them when it sees a new stamp.
func (b *Build) AgentStamp() string {
	names := make([]string, 0, len(b.agents))
	for name, a := range b.agents {
		names = append(names, fmt.Sprintf("%s@%d", name, a.Version))
	}
	for name, query := range b.queries {
		names = append(names, fmt.Sprintf("query:%s@%d", name, query.Version))
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// agentDeclarations are the installed agents as the host runs them.
func (b *Build) agentDeclarations() []platform.Agent {
	return definitionDeclarations(b.agents)
}

func (a Agent) Definition() platform.Agent {
	checkpoints := slices.Clone(a.Checkpoints)
	role := a.HandoffRole
	d := platform.Agent{Model: a.Model, ModelOptions: a.ModelOptions, Name: a.Name, Title: a.Title, Description: a.Description, Instructions: a.Instructions, Tools: slices.Clone(a.Tools), Checkpoints: checkpoints,
		Budget: platform.Budget{Steps: a.Steps, Tokens: a.Tokens, Actions: a.Actions, Cost: a.Cost},
		Guard: func(c platform.Caller, r platform.AgentRun, action, target string, payload json.RawMessage) *kernel.Error {
			if slices.Contains(checkpoints, action) && r.OnBehalf == "" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This step waits for a person: ask, or finish with what you propose.")
			}
			return nil
		},
		To: func(platform.Caller, platform.AgentRun) []platform.Recipient {
			if role == "" {
				return []platform.Recipient{{AppRole: Builder}}
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

func (b *Build) AgentVersion(name string) int { return b.agents[name].Version }

func agentReleaseAsset(a Agent, source string) (platform.ReleaseAsset, error) {
	cases, err := json.Marshal(a.Cases)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	if string(cases) == "null" {
		cases = []byte("[]")
	}
	descriptor := platform.AgentReleaseDescriptor{Model: a.Model, ModelOptions: a.ModelOptions, Name: a.Name, Title: a.Title, Description: a.Description, Instructions: a.Instructions, Tools: slices.Clone(a.Tools), Budget: a.Definition().Budget, Checkpoints: slices.Clone(a.Checkpoints), HandoffRole: a.HandoffRole, Cases: cases}
	if err := descriptor.Check(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: a.Name}, ContractVersion: 1, SourceVersion: source + ".agent-" + strconv.Itoa(a.Version), Body: body})
}

func decodeAgentAsset(asset platform.ReleaseAsset) (Agent, error) {
	var descriptor platform.AgentReleaseDescriptor
	versionText, found := strings.CutPrefix(asset.SourceVersion, "1.agent-")
	version, err := strconv.Atoi(versionText)
	if !found || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != versionText || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetAgent || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &descriptor) != nil || descriptor.Native || descriptor.Name != asset.Ref.Name || descriptor.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
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

func (b *Build) InstallAgentAsset(asset platform.ReleaseAsset) error {
	record, err := decodeAgentAsset(asset)
	if err != nil {
		return err
	}
	if err := b.checkAgent(record); err != nil {
		return err
	}
	b.agents[record.Name] = record
	return nil
}

func (b *Build) retainedAgentAsset(ref platform.AssetRef, sourceVersion string) (platform.ReleaseAsset, error) {
	rows, err := b.agentInventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		if record.Archived || record.Name != ref.Name {
			continue
		}
		for _, raw := range append(slices.Clone(record.Versions), record.Published) {
			retained, ok := ReadPublication[Agent](raw)
			if !ok {
				continue
			}
			asset, err := agentReleaseAsset(retained, b.Manifest().Version)
			if err == nil && asset.Ref == ref && asset.SourceVersion == sourceVersion {
				return asset, nil
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("agent version is not retained")
}

func (b *Build) agentAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.agents, func(record Agent) (platform.ReleaseAsset, error) {
		return agentReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) agentDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	rows, problem := b.agentInventory()
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
	if previous, ok := ReadPublication[Agent](record.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: previous.Name}
		hadPrior = true
	}
	if err = b.checkAgent(record); err != nil {
		return
	}
	if record.Version >= 64 {
		err = fmt.Errorf("agent version family is full")
		return
	}
	record.Version++
	next = platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: record.Name}
	asset, problem := agentReleaseAsset(record, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareAgentReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := b.agentInventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetAgent {
			continue
		}
		frozen, err := decodeAgentAsset(asset)
		if err != nil {
			return nil, err
		}
		index := slices.IndexFunc(rows, func(a Agent) bool { return !a.Archived && a.Name == frozen.Name })
		if index < 0 {
			return nil, fmt.Errorf("agent has no local draft")
		}
		record := rows[index]
		if frozen.Version <= record.Version {
			retained, err := b.retainedAgentAsset(asset.Ref, asset.SourceVersion)
			if err != nil || !equalIntegrationAsset(retained, asset) {
				return nil, fmt.Errorf("retained agent bytes differ")
			}
		} else if record.Version != 0 && frozen.Version != record.Version+1 {
			return nil, fmt.Errorf("agent needs its next version or retained bytes")
		}
		if record.Published != "" && !slices.Contains(record.Versions, record.Published) {
			record.Versions = append(slices.Clone(record.Versions), record.Published)
		}
		frozen.Record = record.Record
		selected := published(frozen)
		for _, raw := range record.Versions {
			if old, ok := ReadPublication[Agent](raw); ok && old.Version == frozen.Version {
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
		out = append(out, platform.ReleasePublication{Schema: SchemaAgentPublish, Image: image})
	}
	return out, nil
}
