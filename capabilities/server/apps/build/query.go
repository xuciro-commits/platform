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

const QueryType = "build.query"
const SchemaQuery = QueryType + ".publish"

// Query owns one bounded declaration and its retained publications. Draft
// edits never replace what callers use until the publication result commits.
type Query struct {
	platform.Record
	Name        string          `json:"name" field:"required,search"`
	Title       string          `json:"title" field:"required,search"`
	Description string          `json:"description" field:"required" type:"longtext"`
	Object      string          `json:"object" field:"required" title:"Source object"`
	By          string          `json:"by,omitempty" title:"Parent reference"`
	Domain      json.RawMessage `json:"domain,omitempty" field:"aside" type:"json"`
	Sort        []string        `json:"sort,omitempty" field:"aside"`
	Limit       int             `json:"limit" field:"required"`
	State       string          `json:"state" field:"readonly" choices:"draft,published"`
	Version     int             `json:"version,omitempty" field:"readonly"`
	Published   string          `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions    []string        `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

// definition projects the stored fields into the shared runtime declaration;
// field validation and execution remain owned by the original record reader.
func (f Query) definition() platform.NamedQuery {
	return platform.NamedQuery{Name: f.Name, Title: f.Title, Description: f.Description, Object: f.Object, By: f.By, Domain: f.Domain, Sort: f.Sort, Limit: f.Limit}
}

func (b *Build) queryEntity() platform.Entity {
	return platform.Entity{Type: QueryType, Title: "Query", Plural: "Queries", Model: Query{}, Display: "title",
		Description: "A reusable record query over one published object; draft edits do not change page bindings.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "queries"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this record query as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "queries", Payload: []platform.Field{}, Do: b.publishQuery}}}}
}

func (b *Build) checkQuery(f Query) error {
	if !named(f.Name) {
		return fmt.Errorf("The query name %s must be lower-case letters and digits", f.Name)
	}
	if _, ok := b.installed[f.Object]; !ok {
		return fmt.Errorf("Query %s needs a published object of this builder", f.Name)
	}
	if err := b.host.ValidateInstallQuery(f.definition()); err != nil {
		return err
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
	if old, ok := wasPublished[Query](f.Published); ok && (old.Name != f.Name || old.Object != f.Object) {
		return fmt.Errorf("A published query keeps its name and source object")
	}
	return nil
}

func (b *Build) publishQuery(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	f, ok := record.(*Query)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkQuery(*f); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
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
	var record Query
	if err := json.Unmarshal(image, &record); err != nil {
		return Query{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return Query{}, fmt.Errorf("accepted query has an invalid version family")
	}
	var latest Query
	for i, raw := range record.Versions {
		f, ok := wasPublished[Query](raw)
		if !ok || f.State != "published" || f.ID != record.ID || f.Version != i+1 || f.Published != "" || len(f.Versions) != 0 || f.definition().Check() != nil || i > 0 && (f.Name != latest.Name || f.Object != latest.Object) {
			return Query{}, fmt.Errorf("accepted query has a malformed version %d", i+1)
		}
		latest = f
	}
	return latest, nil
}

func (b *Build) queryInventory() ([]Query, error) {
	return readDefinitionInventory[Query](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installQuery(c platform.Caller, f Query) error {

	if err := b.host.InstallQuery(c, f.definition(), f.Version); err != nil {
		return err
	}
	b.queries[f.Name] = f
	return nil
}

func (b *Build) QueryDefinition(name string, version int) (platform.NamedQuery, int, bool) {
	installed, ok := b.queries[name]
	if ok && (version == 0 || version == installed.Version) {
		return installed.definition(), installed.Version, true
	}
	if version == 0 {
		return platform.NamedQuery{}, 0, false
	}
	list, err := b.queryInventory()
	if err != nil {
		return platform.NamedQuery{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := queryImage(raw)
		if err != nil || latest.Name != name {
			continue
		}
		f, ok := wasPublished[Query](record.Versions[version-1])
		if ok && f.Version == version {
			return f.definition(), version, true
		}
	}
	return platform.NamedQuery{}, 0, false
}

func (b *Build) queryDeclarations() []platform.NamedQuery {
	names := make([]string, 0, len(b.queries))
	for name := range b.queries {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.NamedQuery, 0, len(names))
	for _, name := range names {
		out = append(out, b.queries[name].definition())
	}
	return out
}
