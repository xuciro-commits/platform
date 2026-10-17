package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
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
	if !named(f.Name) {
		return fmt.Errorf("The function name %s must be lower-case letters and digits", f.Name)
	}
	_, installed := b.installed[f.Object]
	shape, selected := b.jointEntities[f.Object]
	if !installed && (!selected || shape.App != ID) {
		return fmt.Errorf("Function %s needs a published object of this builder", f.Name)
	}
	if f.Proposal != nil && f.Proposal.HandoffRole != "" && !slices.Contains(b.Manifest().AllRoles(), f.Proposal.HandoffRole) {
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
		installed, _ := wasPublished[Function](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Function %s is already declared", f.Name)
		}
	}
	if old, ok := wasPublished[Function](f.Published); ok && (old.Name != f.Name || old.Object != f.Object) {
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
		MaxInputBytes: f.MaxInputBytes, MaxOutputBytes: f.MaxOutputBytes, MaxTokens: f.MaxTokens, Roles: f.Roles}
}
