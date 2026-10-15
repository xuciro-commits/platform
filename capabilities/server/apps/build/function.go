package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
	if f.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An AI function may retain at most 64 published versions")
	}
	f.Version++
	f.State = "published"
	f.Published = published(*f)
	f.Versions = append(f.Versions, f.Published)
	if !c.Staging() {
		if err := b.installFunction(c, *f); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	}
	return nil
}

func functionImage(image []byte) (Function, error) {
	var record Function
	if err := json.Unmarshal(image, &record); err != nil {
		return Function{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return Function{}, fmt.Errorf("accepted function has an invalid version family")
	}
	var latest Function
	for i, raw := range record.Versions {
		f, ok := wasPublished[Function](raw)
		if !ok || f.State != "published" || f.ID != record.ID || f.Version != i+1 || f.Published != "" || len(f.Versions) != 0 || f.Definition().Check() != nil || i > 0 && (f.Name != latest.Name || f.Object != latest.Object) {
			return Function{}, fmt.Errorf("accepted function has a malformed version %d", i+1)
		}
		latest = f
	}
	return latest, nil
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
	installed, ok := b.functions[name]
	if ok && (version == 0 || version == installed.Version) {
		return installed.Definition(), installed.Version, true
	}
	if version == 0 {
		return platform.AIFunction{}, 0, false
	}
	list, err := b.functionInventory()
	if err != nil {
		return platform.AIFunction{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := functionImage(raw)
		if err != nil || latest.Name != name {
			continue
		}
		f, ok := wasPublished[Function](record.Versions[version-1])
		if ok && f.Version == version {
			return f.Definition(), version, true
		}
	}
	return platform.AIFunction{}, 0, false
}

func (b *Build) functionDeclarations() []platform.AIFunction {
	names := make([]string, 0, len(b.functions))
	for name := range b.functions {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.AIFunction, 0, len(names))
	for _, name := range names {
		out = append(out, b.functions[name].Definition())
	}
	return out
}
