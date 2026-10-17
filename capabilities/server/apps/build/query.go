package build

import (
	"encoding/json"
	"fmt"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaQuery = QueryType + ".publish"

// Query owns one bounded declaration and its retained publications. Draft
// edits never replace what callers use until the publication result commits.
type Query struct {
	platform.Record
	Name            string              `json:"name" field:"required,search"`
	Title           string              `json:"title" field:"required,search"`
	Description     string              `json:"description" field:"required" type:"longtext"`
	Object          string              `json:"object,omitempty" title:"Source object"`
	Interface       string              `json:"interface,omitempty" title:"Source interface"`
	InterfaceShape  *platform.Interface `json:"interfaceShape,omitempty" field:"readonly,aside" type:"json" title:"Published interface shape"`
	Implementations []string            `json:"implementations,omitempty" field:"readonly,aside" title:"Published implementations"`
	By              string              `json:"by,omitempty" title:"Parent reference"`
	Domain          json.RawMessage     `json:"domain,omitempty" field:"aside" type:"json"`
	Sort            []string            `json:"sort,omitempty" field:"aside"`
	Limit           int                 `json:"limit" field:"required"`
	State           string              `json:"state" field:"readonly" choices:"draft,published"`
	Version         int                 `json:"version,omitempty" field:"readonly"`
	Published       string              `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions        []string            `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

// definition projects the stored fields into the shared runtime declaration;
// field validation and execution remain owned by the original record reader.
func (f Query) Definition() platform.NamedQuery {
	return platform.NamedQuery{Name: f.Name, Title: f.Title, Description: f.Description, Object: f.Object, Interface: f.Interface, InterfaceShape: f.InterfaceShape, Implementations: f.Implementations, By: f.By, Domain: f.Domain, Sort: f.Sort, Limit: f.Limit}
}

func (b *Build) queryEntity() platform.Entity {
	return platform.Entity{Type: QueryType, Title: "Query", Plural: "Queries", Model: Query{}, Display: "title",
		Description: "A reusable record query over a published object or interface; draft edits do not change published bindings.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "queries"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this record query as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "queries", Payload: []platform.Field{}, Do: b.publishQuery}}}}
}

func (b *Build) checkQuery(f Query) error {
	if !named(f.Name) {
		return fmt.Errorf("The query name %s must be lower-case letters and digits", f.Name)
	}
	shape, selected := b.jointEntities[f.Object]
	if _, ok := b.installed[f.Object]; f.Interface == "" && !ok && (!selected || shape.App != ID) {
		return fmt.Errorf("Query %s needs a published object of this builder", f.Name)
	}
	var validation error
	if f.Interface == "" && selected {
		// The original private installation checks fields against the exact
		// selected model, not a different live version during compilation.
		validation = f.Definition().Check()
	} else if f.Interface != "" && b.jointMode {
		_, validation = b.host.BindInterfaceQuery(f.Definition(), b.queryDraftObjects()...)
	} else {
		validation = b.host.ValidateInstallQuery(f.Definition())
	}
	if validation != nil {
		return validation
	}
	list, err := b.queryInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		installed, _ := wasPublished[Query](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Query %s is already declared", f.Name)
		}
	}
	if old, ok := wasPublished[Query](f.Published); ok && (old.Name != f.Name || old.Object != f.Object || old.Interface != f.Interface) {
		return fmt.Errorf("A published query keeps its name and source object")
	}
	return nil
}

func (b *Build) bindQuery(f Query) (Query, error) {
	if f.Interface != "" {
		q := f.Definition()
		q.InterfaceShape, q.Implementations = nil, nil
		bound, err := b.host.BindInterfaceQuery(q, b.queryDraftObjects()...)
		if err != nil {
			return f, err
		}
		f.InterfaceShape, f.Implementations = bound.InterfaceShape, bound.Implementations
	}
	return f, b.checkQuery(f)
}

func (b *Build) queryDraftObjects() []platform.EntityInfo {
	var out []platform.EntityInfo
	for _, info := range b.jointEntities {
		out = append(out, info)
	}
	return out
}

func (b *Build) publishQuery(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	f, ok := record.(*Query)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	bound, err := b.bindQuery(*f)
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	*f = bound
	if f.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A query may retain at most 64 published versions")
	}
	f.Version++
	f.State = "published"
	f.Published = published(*f)
	f.Versions = append(f.Versions, f.Published)
	if !c.Staging() {
		if err := b.installQuery(c, *f); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	}
	return nil
}

func queryImage(image []byte) (Query, error) {
	return definitionImage[Query](image, "query", func(before, after Query) bool {
		return before.Name == after.Name && before.Object == after.Object && before.Interface == after.Interface
	})
}

func (b *Build) queryInventory() ([]Query, error) {
	return readDefinitionInventory[Query](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installQuery(c platform.Caller, f Query) error {

	if err := b.host.InstallQuery(c, f.Definition(), f.Version); err != nil {
		return err
	}
	b.queries[f.Name] = f
	return nil
}

func (b *Build) QueryDefinition(name string, version int) (platform.NamedQuery, int, bool) {
	return retainedDefinition(b.queries, b.queryInventory, queryImage, name, version)
}

func (b *Build) queryDeclarations() []platform.NamedQuery {
	return definitionDeclarations(b.queries)
}
