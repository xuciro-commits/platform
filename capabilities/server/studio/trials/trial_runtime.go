package trials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
	"slices"
	"time"
)

type Simulation struct {
	Issues          []platform.FieldIssue    `json:"issues,omitempty"`
	RefusalCode     string                   `json:"refusalCode,omitempty"`
	Unsupported     bool                     `json:"unsupported,omitempty"`
	TraceTruncated  bool                     `json:"traceTruncated,omitempty"`
	Input           *SimulationStep          `json:"input,omitempty"`
	Trace           []platform.DecisionTrace `json:"trace,omitempty"`
	Read            json.RawMessage          `json:"read,omitempty"`
	RolledBack      bool                     `json:"rolledBack,omitempty"`
	Accepted        bool                     `json:"accepted"`
	Refusal         string                   `json:"refusal,omitempty"`
	Changes         []SimulatedChange        `json:"changes"`
	Matched         *bool                    `json:"matched,omitempty"`
	Flows           []SimulatedFlow          `json:"flows,omitempty"`
	Tasks           []platform.WorkTask      `json:"tasks,omitempty"`
	Functions       []build.FunctionRun      `json:"functions,omitempty"`
	ComputeMatched  *bool                    `json:"computeMatched,omitempty"`
	FunctionMatched *bool                    `json:"functionMatched,omitempty"`
}

type SimulatedFlow struct {
	ID           string                     `json:"id"`
	Flow         string                     `json:"flow"`
	Version      int                        `json:"version"`
	Dependencies string                     `json:"dependencies,omitempty"`
	Release      string                     `json:"release,omitempty"`
	State        string                     `json:"state"`
	Tokens       []platform.FlowToken       `json:"tokens"`
	Trace        []platform.FlowTraceLine   `json:"trace"`
	Outputs      map[string]json.RawMessage `json:"outputs,omitempty"`
}

type SimulatedChange struct {
	Before json.RawMessage `json:"before,omitempty"`
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Record json.RawMessage `json:"record"`
}

type CandidateSimulationRequest struct {
	Organization  *platform.OrgSeed   `json:"organization,omitempty"`
	Roots         []platform.AssetRef `json:"roots,omitempty"`
	ApplicationID string              `json:"applicationId,omitempty"`
	CandidateID   string              `json:"candidateId,omitempty"`
	Actors        []string            `json:"actors,omitempty"`
	Sample        string              `json:"sample,omitempty"`
	ObjectID      string              `json:"objectId,omitempty"`
	ProcessID     string              `json:"processId,omitempty"`
	FunctionID    string              `json:"functionId,omitempty"`
	Model         string              `json:"model,omitempty"`
	As            string              `json:"as,omitempty"`
	At            time.Time           `json:"at"`
	Steps         []SimulationStep    `json:"steps"`
	Samples       []SimulationSample  `json:"samples,omitempty"`
}

type SimulationStep struct {
	Read           bool                      `json:"read,omitempty"`
	Type           string                    `json:"type"`
	ID             string                    `json:"id"`
	Action         string                    `json:"action"`
	Payload        json.RawMessage           `json:"payload"`
	Expect         string                    `json:"expect,omitempty"`
	As             string                    `json:"as,omitempty"`
	AdvanceSeconds int                       `json:"advanceSeconds,omitempty"`
	Flow           string                    `json:"flow,omitempty"`
	Step           string                    `json:"step,omitempty"`
	Answer         string                    `json:"answer,omitempty"`
	Function       *platform.FunctionFixture `json:"function,omitempty"`
	Compute        *platform.ComputeFixture  `json:"compute,omitempty"`
}

type SimulationActor struct {
	Member    string       `json:"member"`
	Steps     []Simulation `json:"steps"`
	TestID    string       `json:"testId"`
	Recovered bool         `json:"recovered"`
	Passed    *bool        `json:"passed,omitempty"`
}

type CandidateSimulation struct {
	Actors      []SimulationActor `json:"actors,omitempty"`
	CandidateID string            `json:"candidateId"`
	TestID      string            `json:"testId,omitempty"`
	Model       string            `json:"model,omitempty"`
	Fixture     bool              `json:"fixture,omitempty"`
	Steps       []Simulation      `json:"steps"`
	Recovered   bool              `json:"recovered"`
	Passed      *bool             `json:"passed,omitempty"`
}

type SimulationSample struct {
	Type    string            `json:"type"`
	Records []json.RawMessage `json:"records"`
}

type Environment struct {
	Organization        *platform.OrgSeed
	OrganizationFactory platform.SimulationOrganizationFactory
	Factories           map[string]platform.SimulationFactory
	Identity            platform.SimulationIdentityFactory
	Modules             map[string][]byte
	Manifests           []platform.Manifest
	Samples             []SimulationSample
}

type sampleOwner struct {
	manifest platform.Manifest
	ledger   *platform.Ledger
}

func NewSampleOwner(tenant string, manifest platform.Manifest) *sampleOwner {
	var classes []string
	for _, entity := range manifest.Entities {
		classes = append(classes, entity.Type)
	}
	return &sampleOwner{manifest: manifest, ledger: platform.NewLedger(tenant, manifest.ID, manifest.Actions, classes...)}
}
func (a *sampleOwner) AcceptedLedger() *platform.Ledger         { return a.ledger }
func (a *sampleOwner) Manifest() platform.Manifest              { return a.manifest }
func (a *sampleOwner) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *sampleOwner) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *sampleOwner) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }
func (a *sampleOwner) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Native business actions require an isolated owner test fixture")
}
func PrepareEnvironment(apps []platform.App, candidate platform.ReleaseCandidate, samples []SimulationSample) (Environment, error) {
	environment := Environment{Modules: map[string][]byte{}, Samples: samples, Factories: map[string]platform.SimulationFactory{}}
	ownersByID := map[string]platform.App{}
	for _, app := range apps {
		ownersByID[app.Manifest().ID] = app
		if factory, ok := app.(platform.SimulationOrganizationFactory); ok {
			environment.OrganizationFactory = factory
		}
		if factory, ok := app.(platform.SimulationIdentityFactory); ok {
			environment.Identity = factory
		}
		if factory, ok := app.(platform.SimulationFactory); ok {
			environment.Factories[app.Manifest().ID] = factory
		}
	}
	selected := map[platform.AssetRef]bool{}
	owners := map[string]bool{}
	for _, asset := range candidate.Assets {
		selected[asset.Ref] = true
		if asset.Ref.Kind == platform.AssetFlow {
			var descriptor platform.FlowReleaseDescriptor
			if json.Unmarshal(asset.Body, &descriptor) != nil {
				return environment, fmt.Errorf("flow descriptor is invalid")
			}
			if descriptor.Native {
				return environment, fmt.Errorf("native flow %s needs an isolated owner test fixture", asset.Ref)
			}
		}
		if asset.Ref.App != "build" {
			owners[asset.Ref.App] = true
		}
		if asset.Ref.App != "build" && asset.Ref.Kind == platform.AssetAction {
			return environment, fmt.Errorf("native action %s needs an isolated owner test fixture", asset.Ref)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(owners)) {
		app := ownersByID[id]
		if app == nil {
			return environment, fmt.Errorf("sample owner %s is not installed", id)
		}
		original := app.Manifest()
		manifest := platform.Manifest{ID: id, Title: original.Title, Version: original.Version, Actions: platform.NewCatalog(), Roles: original.AllRoles(), Languages: original.Languages}
		for _, entity := range original.Entities {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetObject, Name: entity.Type}] {
				if entity.Scope.Through != nil || entity.Scope.Participants != nil || entity.Scope.Structure != "" {
					return environment, fmt.Errorf("sample source %s needs an isolated scope fixture", entity.Type)
				}
				entity.Seed = nil
				entity.Standard = platform.Standard{}
				manifest.Entities = append(manifest.Entities, entity)
			}
		}
		for _, l := range original.LinkTypes {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetLinkType, Name: l.Name}] {
				manifest.LinkTypes = append(manifest.LinkTypes, l)
			}
		}
		for _, query := range original.Queries {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetQuery, Name: query.Name}] {
				manifest.Queries = append(manifest.Queries, query)
			}
		}
		for _, function := range original.Functions {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetFunction, Name: function.Name}] {
				manifest.Functions = append(manifest.Functions, function)
			}
		}
		for _, operation := range original.Operations {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetCompute, Name: operation.Name}] {
				manifest.Operations = append(manifest.Operations, operation)
			}
		}
		environment.Manifests = append(environment.Manifests, manifest)
	}
	if len(samples) > 8 {
		return environment, fmt.Errorf("candidate samples accept at most eight object types")
	}
	total := 0
	seen := map[string]bool{}
	for _, sample := range samples {
		allowed := false
		for ref := range selected {
			allowed = allowed || ref.Kind == platform.AssetObject && ref.Name == sample.Type
		}
		if !allowed || seen[sample.Type] || len(sample.Records) > 100 {
			return environment, fmt.Errorf("sample type %s is missing, duplicate or exceeds 100 records", sample.Type)
		}
		seen[sample.Type] = true
		ids := map[string]bool{}
		for _, raw := range sample.Records {
			total += len(raw)
			if total > 256<<10 {
				return environment, fmt.Errorf("candidate sample bytes exceed their bound")
			}
			if _, err := platform.DecodeValue(raw, 32<<10); err != nil {
				return environment, err
			}
			var identity struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &identity) != nil || identity.ID == "" || len(identity.ID) > 128 || ids[identity.ID] {
				return environment, fmt.Errorf("sample rows need unique bounded IDs")
			}
			ids[identity.ID] = true
		}
	}
	return environment, nil
}

type Trace struct {
	Events      []platform.DecisionTrace
	Truncated   bool
	bytes       int
	deniedReads map[string]bool
}

func (trace *Trace) Record(event platform.DecisionTrace) {
	if trace.deniedReads == nil {
		trace.deniedReads = map[string]bool{}
	}
	if event.Request != nil && event.Authorization != nil && event.Request.Kind == "read" && !event.Authorization.Allow {
		question := *event.Request
		question.Record = ""
		question.From = ""
		event.Request = &question
		key := string(platform.Raw(event))
		if trace.deniedReads[key] {
			return
		}
		trace.deniedReads[key] = true
	}
	if len(trace.Events) >= 512 || trace.bytes >= 1<<20 {
		trace.Truncated = true
		return
	}
	raw, _ := json.Marshal(event)
	if len(raw) > 64<<10 {
		event.Reads = nil
		event.Before = nil
		event.After = nil
		trace.Truncated = true
		raw, _ = json.Marshal(event)
	}
	if len(raw) > 64<<10 || trace.bytes+len(raw) > 1<<20 {
		trace.Truncated = true
		return
	}
	trace.bytes += len(raw)
	trace.Events = append(trace.Events, event)
}

type Runtime interface {
	Step(int, SimulationStep) (Simulation, *kernel.Error)
	HasAI() bool
	Pending() (compute, functions bool)
	Fault() string
	Snapshot() (json.RawMessage, error)
	Restore(json.RawMessage) error
	Check() error
}

func RunCandidate(request CandidateSimulationRequest, candidate platform.ReleaseCandidate, member platform.Member, members []platform.Member, functionName string, compose func() (Runtime, error)) (CandidateSimulation, *kernel.Error) {
	var empty CandidateSimulation
	sandbox, err := compose()
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	out := CandidateSimulation{CandidateID: candidate.ID, Steps: []Simulation{}}
	out.TestID, err = platform.CanonicalDigest([]any{candidate.ID, request, members, member.ID})
	if err != nil {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The candidate cannot be tested: {why}", err.Error())
	}
	if sandbox.HasAI() {
		out.Model, out.Fixture = request.Model, true
	}
	passed, asserted, functionAttempted := true, 0, false
	for i, step := range request.Steps {
		if request.FunctionID != "" && step.Action == build.SchemaFunctionCall {
			var call platform.FunctionRequest
			if json.Unmarshal(step.Payload, &call) == nil {
				functionAttempted = functionAttempted || call.Name == functionName
			}
		}
		if step.Expect != "" && step.Expect != "accepted" && step.Expect != "refused" {
			return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose accepted or refused as the expected test outcome")
		}
		result, refusal := sandbox.Step(i, step)
		if refusal != nil {
			return empty, refusal
		}
		if result.ComputeMatched != nil {
			passed = passed && *result.ComputeMatched
			out.Fixture = true
		}
		if result.FunctionMatched != nil {
			passed = passed && *result.FunctionMatched
		}
		if step.Expect != "" {
			matched := (step.Expect == "accepted") == result.Accepted
			result.Matched = &matched
			passed = passed && matched
			asserted++
		}
		out.Steps = append(out.Steps, result)
	}
	if request.FunctionID != "" && !functionAttempted {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A function test must call the selected candidate function")
	}
	compute, functions := sandbox.Pending()
	if compute {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Provide a fixed answer for every pending compute call")
	}
	if functions {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Provide a fixed answer for every pending function call")
	}
	if asserted == len(request.Steps) {
		out.Passed = &passed
	}
	if fault := sandbox.Fault(); fault != "" {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Candidate execution failed: {why}", fault)
	}
	saved, err := sandbox.Snapshot()
	if err == nil {
		var restored Runtime
		restored, err = compose()
		if err == nil {
			err = restored.Restore(saved)
		}
		if err == nil {
			err = restored.Check()
		}
		if err == nil {
			var after json.RawMessage
			after, err = restored.Snapshot()
			out.Recovered = err == nil && bytes.Equal(saved, after)
		}
	}
	if err != nil || !out.Recovered {
		return empty, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The test state did not recover exactly: {why}", fmt.Sprint(err))
	}
	return out, nil
}
