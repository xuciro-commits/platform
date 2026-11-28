package objects

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type queryOwner struct {
	env     publication.QueryAuthoringEnvironment
	records map[string]Query
}

func QueryPublication(env publication.QueryAuthoringEnvironment) publication.QueryAuthoringBinding {
	owner := &queryOwner{env: env, records: map[string]Query{}}
	return owner.binding()
}

func (o *queryOwner) binding() publication.QueryAuthoringBinding {
	return publication.QueryAuthoringBinding{
		Fork: func(env publication.QueryAuthoringEnvironment) publication.QueryAuthoringBinding {
			next := &queryOwner{env: env, records: map[string]Query{}}
			for name, query := range o.records {
				next.records[name] = query
			}
			return next.binding()
		},
		Entity:          o.entity,
		Declarations:    o.declarations,
		Definition:      o.definition,
		Exists:          func(name string) bool { _, ok := o.records[name]; return ok },
		ToolNames:       o.toolNames,
		Stamps:          o.stamps,
		Published:       o.published,
		CheckRecordSets: o.checkRecordSets,
		Assets:          o.assets,
		Resolve:         o.resolve,
		InstallAsset:    o.installAsset,
		Restore:         o.restore,
		ValidateImage:   o.validateImage,
		InstallImage:    o.installImage,
		FreezeBatch:     o.prepare,
		VersionedDraft:  o.draftAssets,
		LoadExecution:   o.loadExecution,
		Draft: func(asset platform.ReleaseAsset) (any, error) {
			q, err := o.fromAsset(asset)
			q.State, q.Version = "", 0
			return q, err
		},
	}
}

func (o *queryOwner) entity() platform.RecordType {
	return platform.RecordType{Type: o.env.Type, Title: "Object set", Plural: "Object sets", Model: Query{}, Display: "title",
		Description: "A saved object set over a published object or interface; reads use the shared ObjectSet executor and the caller’s permissions.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{o.env.Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{o.env.Builder}, Capability: "queries"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Description: "Publish this object set as its next retained version.",
				Roles:       []string{o.env.Builder},
				Capability:  "queries",
				Parameters:  []platform.Field{},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules: []platform.ActionRule{{Function: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
					return o.publish(c, record)
				}}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (o *queryOwner) declarations() []platform.NamedQuery {
	names := make([]string, 0, len(o.records))
	for name := range o.records {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.NamedQuery, 0, len(names))
	for _, name := range names {
		out = append(out, o.records[name].Definition())
	}
	return out
}

func (o *queryOwner) inventory() ([]Query, error) {
	return publication.Inventory[Query](o.env.Host().Automation(platform.Caller{}, o.env.App))
}

func (o *queryOwner) draftObjects() []platform.RecordTypeInfo {
	joint := o.env.JointEntities()
	out := make([]platform.RecordTypeInfo, 0, len(joint))
	for _, info := range joint {
		out = append(out, info)
	}
	return out
}

func (o *queryOwner) validate(query Query) (Query, error) {
	if query.Interface != "" {
		declaration := query.Definition()
		declaration.InterfaceShape, declaration.Implementations = nil, nil
		bound, err := o.env.Host().BindInterfaceQuery(declaration, o.draftObjects()...)
		if err != nil {
			return query, err
		}
		query.InterfaceShape, query.Implementations = bound.InterfaceShape, bound.Implementations
	}
	if !publication.ValidName(query.Name) {
		return query, fmt.Errorf("The query name %s must be lower-case letters and digits", query.Name)
	}
	shape, selected := o.env.JointEntities()[query.Object]
	declared, native := o.env.LookupEntity(query.Object)
	if _, ok := o.env.InstalledObject(query.Object); query.Interface == "" && !ok && (!selected || shape.App != o.env.App) && (!native || declared.App != o.env.App) {
		return query, fmt.Errorf("Query %s needs a published object of this builder", query.Name)
	}
	var validation error
	if query.Interface == "" && selected {
		validation = query.Definition().Check()
	} else if query.Interface != "" && o.env.JointMode() {
		_, validation = o.env.Host().BindInterfaceQuery(query.Definition(), o.draftObjects()...)
	} else {
		validation = o.env.Host().ValidateInstallQuery(query.Definition())
	}
	return query, validation
}

func (o *queryOwner) check(query Query) (Query, error) {
	query, err := o.validate(query)
	if err != nil {
		return query, err
	}
	family := query.family()
	records, err := o.inventory()
	if err != nil {
		return query, err
	}
	for _, other := range records {
		otherFamily := other.family()
		installed, _ := publication.Read[Query](otherFamily.Published)
		if otherFamily.ID != family.ID && (otherFamily.Name == family.Name || installed.Name == family.Name) {
			return query, fmt.Errorf("Object set %s is already declared", family.Name)
		}
	}
	if old, ok := publication.Read[Query](family.Published); ok && !old.keepsPublishedIdentity(query) {
		return query, fmt.Errorf("A published query keeps its name and source object")
	}
	return query, nil
}

func (o *queryOwner) publish(c platform.Caller, record any) *platform.Error {
	var query *Query
	switch value := record.(type) {
	case *Query:
		query = value
	default:
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	bound, err := o.check(*query)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	*query = bound
	return publication.PublishRetained(c, query, func(record *Query) publication.Family { return record.family() }, setQueryFamily,
		"A query may retain at most 64 published versions", func(record *Query) string { return record.image() }, o.installRecord)
}

func (o *queryOwner) installRecord(c platform.Caller, query Query) error {
	if err := o.env.Host().InstallQuery(c, query.Definition(), query.Version); err != nil {
		return err
	}
	o.records[query.Name] = query
	return nil
}

func (o *queryOwner) definition(name string, version int) (platform.NamedQuery, int, bool) {
	if asset, selected := o.env.JointDeclarations()[platform.AssetRef{App: o.env.App, Kind: platform.AssetQuery, Name: name}]; selected {
		ordinal := o.env.AssetVersion(asset)
		if ordinal == version && version > 0 {
			var declaration platform.NamedQuery
			valid := json.Unmarshal(asset.Body, &declaration) == nil && asset.ContractVersion == declaration.ContractVersion() &&
				QueryFromDeclaration(declaration).Name == name && declaration.Check() == nil
			return declaration, ordinal, valid
		}
	}
	return publication.Retained(o.records, o.inventory, o.decode, name, version, Query.family, func(query Query) platform.NamedQuery { return query.Definition() })
}

func (o *queryOwner) decode(raw []byte) (Query, error) {
	return publication.Decode(raw, "object set", Query.family, func(query Query) error { return query.Definition().Check() }, Query.keepsPublishedIdentity)
}

func (o *queryOwner) releaseAsset(query Query) (platform.ReleaseAsset, error) {
	family := query.family()
	return o.release(platform.AssetRef{App: o.env.App, Kind: platform.AssetQuery, Name: family.Name}, o.env.Version+".query-"+fmt.Sprint(family.Version), query.Definition())
}

func (o *queryOwner) release(ref platform.AssetRef, sourceVersion string, definition platform.NamedQuery) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(definition)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return o.env.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: definition.ContractVersion(), SourceVersion: sourceVersion, Body: body})
}

func (o *queryOwner) assets() ([]platform.ReleaseAsset, error) {
	return publication.InstalledAssets(o.records, Query.family, o.releaseAsset)
}

func (o *queryOwner) resolve(name, sourceVersion string) (platform.ReleaseAsset, error) {
	return publication.RetainedAssetWith(o.env.App, o.env.Version, "query", "object set", platform.AssetQuery, name, sourceVersion, o.definition,
		func(ref platform.AssetRef, version string, definition any) (platform.ReleaseAsset, error) {
			return o.release(ref, version, definition.(platform.NamedQuery))
		})
}

func (o *queryOwner) fromAsset(asset platform.ReleaseAsset) (Query, error) {
	var zero Query
	version := o.env.AssetVersion(asset)
	var declaration platform.NamedQuery
	if version == 0 || json.Unmarshal(asset.Body, &declaration) != nil || asset.ContractVersion != declaration.ContractVersion() || declaration.Check() != nil || declaration.Name != asset.Ref.Name {
		return zero, fmt.Errorf("invalid builder query asset %s", asset.Ref)
	}
	query := QueryFromDeclaration(declaration)
	family := query.family()
	family.Version, family.State = version, "published"
	setQueryFamily(&query, family)
	return query, nil
}

func (o *queryOwner) installAsset(asset platform.ReleaseAsset) error {
	query, err := o.fromAsset(asset)
	if err != nil {
		return err
	}
	if _, exists := o.records[query.Name]; exists {
		return fmt.Errorf("query asset %s is already installed", asset.Ref)
	}
	return o.installRecord(platform.Caller{Replaying: true}, query)
}

func (o *queryOwner) prepare(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return publication.Prepare(o.env.App, o.env.Version, assets, platform.AssetQuery, "query", o.env.Type+".publish", o.inventory, Query.family, setQueryFamily, o.resolve,
		func(record Query, asset platform.ReleaseAsset, version int) (Query, error) {
			family := record.family()
			prior, retained := publication.Read[Query](family.Published)
			var declaration platform.NamedQuery
			if version != family.Version+1 || json.Unmarshal(asset.Body, &declaration) != nil || declaration.Check() != nil || o.env.CheckDependencies(asset) != nil {
				return record, fmt.Errorf("invalid next object set version")
			}
			frozen := QueryFromDeclaration(declaration)
			if !record.keepsPublishedIdentity(frozen) || retained && !prior.keepsPublishedIdentity(frozen) {
				return record, fmt.Errorf("invalid next object set version")
			}
			frozen.Record = record.Record
			kept := frozen.family()
			kept.State, kept.Version = "published", version
			setQueryFamily(&frozen, kept)
			family.State, family.Version, family.Published = "published", version, frozen.image()
			family.Versions = append(slices.Clone(family.Versions), family.Published)
			setQueryFamily(&record, family)
			return record, nil
		})
}

func (o *queryOwner) draftAssets(id string, releaseAssets func() ([]platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return publication.DraftAssets(o.env.App, id, "object set", platform.AssetQuery, releaseAssets, o.inventory,
		func(query Query) (Query, error) { return o.check(query) },
		Query.family,
		func(query Query) (platform.ReleaseAsset, error) {
			family := query.family()
			family.Version++
			setQueryFamily(&query, family)
			return o.releaseAsset(query)
		})
}

func (o *queryOwner) validateImage(raw json.RawMessage) error {
	query, err := o.decode(raw)
	if err != nil {
		return err
	}
	return o.env.Host().ValidateInstallQuery(query.Definition())
}

func (o *queryOwner) installImage(raw json.RawMessage) error {
	return publication.ApplyHistory(raw, o.decode, Query.family, o.installRecord)
}

func (o *queryOwner) restore() error {
	records, err := o.inventory()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		if err := o.installImage(raw); err != nil {
			return err
		}
	}
	return nil
}

func (o *queryOwner) loadExecution(asset platform.ReleaseAsset) error {
	query, err := o.fromAsset(asset)
	if err != nil {
		return err
	}
	o.records[query.Name] = query
	return nil
}

func (o *queryOwner) toolNames(c platform.Caller) ([]string, error) {
	records, err := publication.Inventory[Query](c)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, record := range records {
		if !record.Archived {
			names = append(names, record.Name)
		}
	}
	return names, nil
}

func (o *queryOwner) stamps() []string {
	names := make([]string, 0, len(o.records))
	for name, query := range o.records {
		names = append(names, fmt.Sprintf("query:%s@%d", name, query.Version))
	}
	return names
}

func (o *queryOwner) published(c platform.Caller, id string) bool {
	query, ok := platform.Get[Query](c, id)
	return ok && query.Published != ""
}

func (o *queryOwner) checkRecordSets(info platform.RecordTypeInfo, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) error {
	if err := records.CheckRecordSets(info, lookup, engines...); err != nil {
		return err
	}
	for _, set := range info.Sets {
		if set.Binding == nil {
			continue
		}
		if err := o.env.Host().ValidateRecordSet(info, set); err != nil {
			return fmt.Errorf("set %s: %w", set.Name, err)
		}
	}
	return nil
}
