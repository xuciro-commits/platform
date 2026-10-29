package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"time"
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
	if !Named(f.Name) {
		return fmt.Errorf("The query name %s must be lower-case letters and digits", f.Name)
	}
	shape, selected := b.jointEntities[f.Object]
	declared, native := b.conditionLookup(f.Object)
	if _, ok := b.installed[f.Object]; f.Interface == "" && !ok && (!selected || shape.App != ID) && (!native || declared.App != ID) {
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
		installed, _ := ReadPublication[Query](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Query %s is already declared", f.Name)
		}
	}
	if old, ok := ReadPublication[Query](f.Published); ok && (old.Name != f.Name || old.Object != f.Object || old.Interface != f.Interface) {
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
	return publishRetainedDefinition(c, f, "A query may retain at most 64 published versions", b.installQuery)
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
	if asset, selected := b.jointDeclarations[platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: name}]; selected {
		ordinal := b.definitionAssetVersion(asset, platform.AssetQuery, "query")
		if ordinal == version && version > 0 {
			var query platform.NamedQuery
			valid := asset.ContractVersion == 1 && json.Unmarshal(asset.Body, &query) == nil && query.Name == name && query.Check() == nil
			return query, ordinal, valid
		}
	}
	return retainedDefinition(b.queries, b.queryInventory, queryImage, name, version)
}

func (b *Build) queryDeclarations() []platform.NamedQuery {
	return definitionDeclarations(b.queries)
}

func queryReleaseAsset(f Query, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: f.Name}, sourceVersion+".query-"+strconv.Itoa(f.Version), f.Definition())
}

// QueryReleaseAsset resolves only a retained version from this owner.
func (b *Build) QueryReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "query", "query", platform.AssetQuery, name, sourceVersion, b.QueryDefinition)
}

func (b *Build) queryAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.queries, func(record Query) (platform.ReleaseAsset, error) {
		return queryReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) queryAsset(asset platform.ReleaseAsset) (Query, error) {
	version := b.definitionAssetVersion(asset, platform.AssetQuery, "query")
	var f Query
	if version == 0 || asset.ContractVersion != 1 ||
		json.Unmarshal(asset.Body, &f) != nil || f.Name != asset.Ref.Name ||
		f.Definition().Check() != nil {
		return Query{}, fmt.Errorf("invalid builder query asset %s", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return f, nil
}

// InstallQueryAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallQueryAsset(asset platform.ReleaseAsset) error {
	f, err := b.queryAsset(asset)
	if err != nil {
		return err
	}
	if _, exists := b.queries[f.Name]; exists {
		return fmt.Errorf("query asset %s is already installed", asset.Ref)
	}
	return b.installQuery(platform.Caller{Replaying: true}, f)
}

func (b *Build) queryDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetQuery, "query", b.queryInventory, func(record Query) (Query, error) {
		return b.bindQuery(record)
	}, func(record Query) (platform.ReleaseAsset, error) {
		record.Version++
		return queryReleaseAsset(record, b.Manifest().Version)
	})
}

// Freeze the immutable candidate beside the mutable editor row. Existing
// ordinals must match retained bytes; only the next ordinal can publish.
func (b *Build) prepareQueryReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.queryInventory, platform.AssetQuery, "query", SchemaQuery, "query", b.QueryReleaseAsset, func(record Query, asset platform.ReleaseAsset, version int) (Query, error) {
		var q platform.NamedQuery
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Object != record.Object || q.Interface != record.Interface {
			return record, fmt.Errorf("invalid next query version")
		}
		frozen := Query{Record: record.Record, Name: q.Name, Title: q.Title, Description: q.Description, Object: q.Object, Interface: q.Interface, InterfaceShape: q.InterfaceShape, Implementations: q.Implementations, By: q.By, Domain: q.Domain, Sort: q.Sort, Limit: q.Limit, State: "published", Version: version}
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}

func (b *Build) checkRecordSets(o Object, engines ...platform.RuleEngine) error {
	if len(o.Sets) == 0 {
		return nil
	}
	info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return b.conditionLookup(typ)
	}
	if err := platform.CheckRecordSets(info, lookup, engines...); err != nil {
		return err
	}
	for _, set := range o.Sets {
		if set.Binding != nil {
			reader, ok := b.host.(platform.RecordSetHost)
			if !ok {
				return fmt.Errorf("set %s needs a typed host binding", set.Name)
			}
			if err := reader.ValidateRecordSet(info, set); err != nil {
				return fmt.Errorf("set %s: %w", set.Name, err)
			}
		}
	}
	return nil
}

const QueryType = "build.query"
