package host

import (
	"encoding/json"
	"fmt"
	pageowner "platformserver/services/apps/pages"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type PageModelRecord struct {
	Object string `json:"object"`
	ID     string `json:"id"`
}

type PageModelRule struct {
	Action   platform.AssetRef     `json:"action"`
	Title    string                `json:"title"`
	Criteria platform.RuleCriteria `json:"criteria"`
}

type PageModelRead struct {
	Page           platform.AssetRef          `json:"page"`
	ContentVersion string                     `json:"contentVersion,omitempty"`
	Query          *platform.Query            `json:"query,omitempty"`
	Draft          *platform.Page             `json:"draft,omitempty"`
	Section        string                     `json:"section"`
	Records        map[string]PageModelRecord `json:"records,omitempty"`
	Values         map[string]json.RawMessage `json:"values,omitempty"`
	Offset         int                        `json:"offset,omitempty"`
	Limit          int                        `json:"limit,omitempty"`
	Search         string                     `json:"search,omitempty"`
	Sort           []string                   `json:"sort,omitempty"`
}

type PageModelResult struct {
	Composite     *PageCompositeResult                         `json:"composite,omitempty"`
	Object        string                                       `json:"object"`
	Records       []map[string]any                             `json:"records"`
	Total         int                                          `json:"total"`
	Partial       bool                                         `json:"partial"`
	Hidden        bool                                         `json:"hidden"`
	Fields        []platform.FieldInfo                         `json:"fields"`
	Access        map[string]map[string]platform.AuthzDecision `json:"access"`
	Authorization *platform.AuthzCollection                    `json:"authorization,omitempty"`
	Model         *platform.ModelBinding                       `json:"model"`
	LabelField    string                                       `json:"labelField,omitempty"`
	Sort          []string                                     `json:"sort,omitempty"`
	SortLocked    bool                                         `json:"sortLocked,omitempty"`
}

func (t *Tenant) modelTransition(ref platform.AssetRef) (platform.Transition, string, bool) {
	if ref.Kind != platform.AssetAction {
		return platform.Transition{}, "", false
	}
	owner := t.owner["action:"+ref.Name]
	if owner == nil || owner.Manifest().ID != ref.App {
		return platform.Transition{}, "", false
	}
	for _, entity := range owner.Manifest().Entities {
		if entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if entity.Type+"."+transition.Name == ref.Name {
				return transition, entity.Lifecycle.Field, true
			}
		}
	}
	return platform.Transition{}, "", false
}

func (t *Tenant) modelCriteria(ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	transition, _, ok := t.modelTransition(ref)
	if !ok || transition.Criteria == nil {
		return nil, false
	}
	return transition.Criteria, true
}

func (t *Tenant) modelVisibleLookup(member platform.Member) func(string) (platform.EntityInfo, bool) {
	entities := map[string]platform.EntityInfo{}
	for _, entity := range t.Entities(member) {
		entities[entity.Type] = entity
	}
	return func(typ string) (platform.EntityInfo, bool) {
		value, ok := entities[typ]
		return platform.ConstraintEntity(value), ok
	}
}

type pageModelRowsResult struct {
	records       []map[string]any
	total         int
	partial       bool
	authorization *platform.AuthzCollection
	sort          []string
	sortLocked    bool
}

type pageModelFilter struct {
	caller   platform.Caller
	model    *platform.ModelBinding
	criteria *platform.RuleCriteria
	inputs   map[string]any
	lookup   func(string) (platform.EntityInfo, bool)
	at       time.Time
	partial  bool
	err      *kernel.Error
}

func (f *pageModelFilter) match(value reflect.Value) bool {
	if f.err != nil {
		return false
	}
	record := value.Interface()
	if f.criteria != nil {
		matched, _, refusal := f.criteria.Evaluate(f.caller, record, f.inputs, f.at, f.lookup)
		if refusal != nil {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				f.partial = true
				return false
			}
			f.err = refusal
			return false
		}
		if !matched {
			return false
		}
	}
	for _, predicate := range f.model.Where {
		value, refusal := platform.EvaluateRule(predicate, platform.RuleReadSpec{Object: f.model.Type}, f.caller, record, nil, f.at, f.lookup)
		if refusal != nil {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				f.partial = true
				return false
			}
			f.err = refusal
			return false
		}
		if string(value.Value.Data) != "true" {
			return false
		}
	}
	return true
}

type pageModelRuntime struct{ runtime }

func (r pageModelRuntime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	return r.t.authorizeDepth(r.t.records, c, q, at, true, 0, newAuthorizationWalk())
}

func (r pageModelRuntime) ReadRecordPath(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	return r.t.readRecordPathHeld(c, typ, id, path, at)
}

func (t *Tenant) readPageModel(member platform.Member, request PageModelRead, at time.Time) (PageModelResult, *kernel.Error) {
	result := PageModelResult{Records: []map[string]any{}, Fields: []platform.FieldInfo{}, Access: map[string]map[string]platform.AuthzDecision{}}
	if request.Offset < 0 || request.Limit < 1 || request.Limit > 200 || len(request.Records) > 128 || len(request.Values) > 128 || len(request.Search) > 1024 || len(request.Sort) > 16 {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model read exceeds its bounded window")
	}
	member = t.authorizationMember(member)
	if err := t.admits(member); err != nil {
		return result, err
	}
	var page platform.Page
	if request.Draft != nil {
		caller := platform.NewCaller(runtime{t}, member, "build", false, false)
		if err := authorizationRefusal(t.authorizeNative(caller, "POST /v1/page-model/preview", "read", []string{"builder"}, true)); err != nil {
			return result, err
		}
		if err := json.Unmarshal(platform.Raw(request.Draft), &page); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid page model")
		}
		if err := page.NormalizeModelBindings(t.entity); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		projected, err := page.Safety()
		if err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		page = projected
		for _, section := range page.DataBindings {
			for _, model := range section.ModelBindings() {
				if model.Set != nil && model.Set.Query != nil && model.Set.Query.SourceVersion == "" {
					if binding, ok := (hostView{t: t}).ModelQueryBinding(model.Set.Query.Ref); ok {
						*model.Set.Query = binding
					}
				}
			}
		}
	} else {
		for _, definition := range t.Definitions(member) {
			if definition.Ref == request.Page && definition.Page != nil {
				if request.ContentVersion != "" && definition.ContentVersion != request.ContentVersion {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Model publication changed; refresh the page")
				}
				page = *definition.Page
				break
			}
		}
		if page.Name == "" {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Model page is unavailable")
		}
	}
	projectedPage, projectionError := page.Safety()
	if projectionError != nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, projectionError.Error())
	}
	page = projectedPage
	for _, binding := range page.DataBindings {
		if err := page.CheckDataBinding(binding, t.entity, t.modelCriteria, t.services.Rules); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}

	if err := pageowner.CheckModelQueries(page, t.definitions, t.entity); err != nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	index := slices.IndexFunc(page.DataBindings, func(section platform.PageDataBinding) bool { return section.ID == request.Section })
	if index < 0 || page.DataBindings[index].PrimaryModel() == nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Model section is unavailable")
	}
	section := page.DataBindings[index]
	if len(section.Models) > 0 {
		return t.readPageComposite(member, page, section, request, at)
	}
	return t.readPageModelSource(member, page, section, request, at)
}

func (t *Tenant) readPageModelSource(member platform.Member, page platform.Page, section platform.PageDataBinding, request PageModelRead, at time.Time) (PageModelResult, *kernel.Error) {
	result := PageModelResult{Records: []map[string]any{}, Fields: []platform.FieldInfo{}, Access: map[string]map[string]platform.AuthzDecision{}}
	model := section.Model
	result.Model, result.Object = model, model.Type
	info, known := t.entity(model.Type)
	if !known {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Model object is unavailable")
	}
	caller := platform.NewCaller(runtime{t}, member, info.App, false, false)
	visibleLookup := t.modelVisibleLookup(member)
	visibleInfo, readable := visibleLookup(model.Type)
	if !readable {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Model object is outside the caller's permissions")
	}
	for name, reference := range request.Records {
		typ := page.RecordVariableObject(name)
		if name == "$record" {
			typ = model.Type
		}
		if name == "$parent" && model.Relation != nil {
			typ = model.Relation.ParentType
		}
		if typ == "" || reference.Object != typ || reference.ID == "" {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model context does not match its declared record variable")
		}
		if _, err := caller.ReadRecord(typ, reference.ID, at); err != nil {
			return result, err
		}
	}
	for name, raw := range request.Values {
		variable, ok := page.InputSchema.Variables[name]
		if !ok || variable.Type == "record" || len(raw) > 64<<10 {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model input is not a declared scalar variable")
		}
	}
	fieldRefs := slices.Clone(model.Fields)
	for _, field := range section.Fields {
		if !slices.ContainsFunc(fieldRefs, func(f platform.FieldRef) bool { return f.Key() == field.Key() }) {
			fieldRefs = append(fieldRefs, field)
		}
	}
	for _, fields := range section.FieldSlots {
		for _, field := range fields {
			if !slices.ContainsFunc(fieldRefs, func(f platform.FieldRef) bool { return f.Key() == field.Key() }) {
				fieldRefs = append(fieldRefs, field)
			}
		}
	}
	projected := platform.ModelFields(visibleInfo, model, visibleLookup)
	for _, field := range projected.Fields {
		if slices.ContainsFunc(fieldRefs, func(ref platform.FieldRef) bool { return ref.Key() == field.Name }) {
			result.Fields = append(result.Fields, field)
		}
	}
	if model.Label != nil {
		result.LabelField = "$modelLabel"
		result.Fields = append(result.Fields, platform.FieldInfo{Name: result.LabelField, Title: "Model label", Type: "text", ReadOnly: true})
	}
	var criteria *platform.RuleCriteria
	inputs := map[string]any{}
	if model.Options != nil && model.Options.Rule != nil {
		rule := model.Options.Rule
		criteria, known = t.modelCriteria(rule.Action)
		if !known {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Original model action criteria changed")
		}
		for name, typ := range criteria.Reads.Inputs {
			variable := rule.Inputs[name]
			if typ.Kind == "reference" {
				reference := request.Records[variable]
				if reference.Object != typ.Object || reference.ID == "" {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Select the model rule's context record")
				}
				inputs[name] = reference.ID
			} else {
				raw := request.Values[variable]
				if len(raw) == 0 {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model rule input is unavailable")
				}
				value, err := platform.DecodeValue(raw, 64<<10)
				if err != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				inputs[name] = value
			}
		}
	}
	projection := []string{}
	for _, field := range projected.Fields {
		if len(fieldRefs) > 0 && !slices.ContainsFunc(fieldRefs, func(ref platform.FieldRef) bool { return ref.Key() == field.Name }) {
			continue
		}
		if !strings.Contains(field.Name, ".") {
			projection = append(projection, field.Name)
		}
	}
	window, err := t.pageModelRows(caller, page, section, request, projection, criteria, inputs, at)
	if err != nil {
		return result, err
	}
	result.Partial, result.Authorization = window.partial, window.authorization
	result.Sort, result.SortLocked = window.sort, window.sortLocked
	result.Total = window.total
	selected := make([]map[string]any, 0, len(window.records))
	for _, row := range window.records {
		id, _ := row["id"].(string)
		if id == "" {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Model source row has no identity")
		}
		ruleRecord := t.pageModelRuleRecord(model.Type, id, row)
		access := map[string]platform.AuthzDecision{}
		for _, field := range fieldRefs {
			if field.Key() == "id" {
				continue
			}
			if _, err := platform.RecordPathField(model.Type, field.Segments(), visibleLookup); err != nil {
				result.Partial = true
				delete(row, field.Key())
				continue
			}
			if len(field.Segments()) > 1 {
				access[field.Key()] = platform.AuthzDecision{Allow: false, Rule: "none", Reason: "Relationship fields are read-only projections", Layers: []platform.AuthzLayer{{Name: "fields", Reason: "Relationship fields are read-only projections"}}}
				value, _, refusal := caller.ReadRecordPath(model.Type, id, field.Segments(), at)
				if refusal != nil {
					result.Partial = true
					delete(row, field.Key())
					continue
				}
				decoded, decodeErr := platform.DecodeValue(value, 64<<10)
				if decodeErr != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, decodeErr.Error())
				}
				row[field.Key()] = decoded
			} else {
				read := caller.Authorize(platform.AuthzRequest{Principal: member.ID, Tenant: t.ID, Kind: "read", Type: model.Type, Record: id, Fields: []string{field.Name}}, at)
				if !read.Allow || len(read.Withheld) > 0 {
					result.Partial = true
					delete(row, field.Key())
				}
				decision := caller.Authorize(platform.AuthzRequest{Principal: member.ID, Tenant: t.ID, Kind: "write", Type: model.Type, Record: id, Fields: []string{field.Name}}, at)
				if f, ok := visibleInfo.Field(field.Name); ok && f.ReadOnly {
					decision.Allow = false
					decision.Reason = "The original model declares this field read-only"
				}
				access[field.Key()] = decision
			}
		}
		if model.Label != nil {
			value, refusal := platform.EvaluateRule(*model.Label, platform.RuleReadSpec{Object: model.Type}, caller, ruleRecord, nil, at, t.entity)
			if refusal != nil {
				if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					result.Partial = true
				} else {
					return result, refusal
				}
			} else {
				var label string
				_ = json.Unmarshal(value.Value.Data, &label)
				row[result.LabelField] = label
			}
		}
		result.Access[id] = access
		selected = append(selected, row)
	}
	result.Records = selected
	windowAccess := map[string]map[string]platform.AuthzDecision{}
	for _, row := range result.Records {
		id := fmt.Sprint(row["id"])
		windowAccess[id] = result.Access[id]
	}
	result.Access = windowAccess
	if result.Partial && model.Visibility != "mark" {
		result.Hidden = true
		result.Records = []map[string]any{}
		result.Total = 0
		result.Access = map[string]map[string]platform.AuthzDecision{}
	}
	if len(platform.Raw(result)) > platform.ExprMaxContextBytes {
		return PageModelResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model result exceeds its byte budget")
	}
	return result, nil
}

func (t *Tenant) pageModelRows(c platform.Caller, page platform.Page, section platform.PageDataBinding, request PageModelRead, projection []string, criteria *platform.RuleCriteria, inputs map[string]any, at time.Time) (pageModelRowsResult, *kernel.Error) {
	model := section.Model
	result := pageModelRowsResult{records: []map[string]any{}, sort: slices.Clone(request.Sort)}
	collection := model.Set
	if model.Options != nil && model.Options.Set != nil {
		collection = model.Options.Set
	}
	var set platform.ObjectSet
	var bound []platform.NamedQuery
	switch {
	case collection != nil && collection.Name != "":
		reference := request.Records[collection.RecordVariable]
		if reference.ID == "" {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Select the declared collection's source record")
		}
		value, refusal := c.ReadRecordSet(reference.Object, reference.ID, collection.Name, at)
		if refusal != nil {
			return result, refusal
		}
		if value.Object != model.Type {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Declared collection object changed")
		}
		result.partial, result.authorization = value.Partial, value.Authorization
		if len(value.Records) == 0 {
			return result, nil
		}
		ids := make([]string, 0, len(value.Records))
		for _, raw := range value.Records {
			var identity struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &identity) != nil || identity.ID == "" {
				return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Declared collection contains an unreadable record")
			}
			ids = append(ids, identity.ID)
		}
		set = platform.ObjectSet{Object: model.Type, IDs: ids}
		if root, ok := t.entity(page.RecordResourceObject(collection.RecordVariable).Name); ok {
			for _, declared := range root.Sets {
				if declared.Name != collection.Name {
					continue
				}
				result.sortLocked = true
				result.sort = nil
				for _, order := range declared.Order {
					if order.Expr.Op != "" || order.Expr.Ref == "" {
						result.sort = nil
						break
					}
					name := order.Expr.Ref
					if order.Desc {
						name = "-" + name
					}
					result.sort = append(result.sort, name)
				}
			}
		}
	case collection != nil && collection.Query != nil:
		reference := request.Records[collection.RecordVariable]
		if reference.ID == "" && collection.Query.Ref.Kind == platform.AssetQuery {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Select the query binding's source record")
		}
		query, ok := t.pageModelQuery(c.Member, collection)
		if !ok {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Model query is unavailable")
		}
		set = query.ObjectSet(reference.ID)
		bound = append(bound, query)
		if len(query.Sort) > 0 {
			result.sort = slices.Clone(query.Sort)
			result.sortLocked = true
		}
	default:
		set = platform.ObjectSet{Object: model.Type}
		if request.Query != nil {
			set = request.Query.ObjectSet(model.Type, nil)
		}
		if relation := model.Relation; relation != nil {
			reference := request.Records["$parent"]
			if reference.ID == "" {
				return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Select a parent record for this model binding")
			}
			domain, _ := json.Marshal([]any{[]any{relation.Field, "=", reference.ID}})
			set = platform.ObjectSet{Intersect: []platform.ObjectSet{set, {Object: model.Type, Domain: domain}}}
		}
		if section.Kind == "record" {
			if reference := request.Records["$record"]; reference.ID != "" {
				set = platform.ObjectSet{Intersect: []platform.ObjectSet{set, {Object: model.Type, IDs: []string{reference.ID}}}}
			}
		}
	}
	set.Search = request.Search
	if !result.sortLocked {
		result.sort = slices.Clone(request.Sort)
	}
	if set.Check() != nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model source is not one bounded object set")
	}
	allEntities := t.ruleEntitySnapshot()
	var filter *pageModelFilter
	if criteria != nil || len(model.Where) > 0 {
		filter = &pageModelFilter{
			caller: platform.NewCaller(pageModelRuntime{runtime{t}}, c.Member, c.App, c.Replaying, c.Automation),
			model:  model, criteria: criteria, inputs: inputs, at: at,
			lookup: func(typ string) (platform.EntityInfo, bool) {
				info, ok := allEntities[typ]
				return platform.ConstraintEntity(info), ok
			},
		}
	}
	var projectedErr *kernel.Error
	window := objectWindow{
		Sort: result.sort, Offset: request.Offset, Limit: request.Limit, Bounded: true,
		Project: func(_ platform.EntityInfo, _ reflect.Value, record any) any {
			if projectedErr != nil {
				return nil
			}
			value, err := platform.DecodeValue(platform.Raw(record), 1<<20)
			if err != nil {
				projectedErr = platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				return nil
			}
			row, ok := value.(map[string]any)
			if !ok {
				projectedErr = platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model source must contain records")
				return nil
			}
			out := map[string]any{}
			for _, name := range []string{"id", "revision", "created", "changed", "archived"} {
				if item, exists := row[name]; exists {
					out[name] = item
				}
			}
			for _, name := range projection {
				if item, exists := row[name]; exists {
					out[name] = item
				}
			}
			return out
		},
	}
	if filter != nil {
		window.Match = filter.match
	}
	loaded, object, err := t.loadObjectSet(t.records, c, set, window, at, bound...)
	if err != nil {
		return result, err
	}
	if object != model.Type {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Model source object changed")
	}
	if filter != nil {
		result.partial = result.partial || filter.partial
		if filter.err != nil {
			return result, filter.err
		}
	}
	if projectedErr != nil {
		return result, projectedErr
	}
	result.total, result.partial = loaded.Total, result.partial || loaded.Partial
	for _, record := range loaded.Records {
		row, ok := record.(map[string]any)
		if !ok {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Model projection lost its record shape")
		}
		result.records = append(result.records, row)
	}
	return result, nil
}

func (t *Tenant) pageModelQuery(member platform.Member, collection *platform.ModelRecordSet) (platform.NamedQuery, bool) {
	for _, definition := range t.Definitions(member) {
		if definition.Ref != collection.Query.Ref {
			continue
		}
		if definition.Query == nil {
			return platform.NamedQuery{}, false
		}
		if collection.Query.SourceVersion == "" {
			return *definition.Query, true
		}
		if retained := definition.QueryVersion(collection.Query.SourceVersion); retained != nil && retained.Query != nil {
			return *retained.Query, true
		}
		return platform.NamedQuery{}, false
	}
	return platform.NamedQuery{}, false
}

func (t *Tenant) ruleEntitySnapshot() map[string]platform.EntityInfo {
	t.records.Lock()
	defer t.records.Unlock()
	out := map[string]platform.EntityInfo{}
	for _, entity := range t.records.Types() {
		out[entity.Info().Type] = entity.Info()
	}
	return out
}

func (t *Tenant) readRecordPathHeld(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	if len(path) == 0 || len(path) > 16 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record paths need 1–16 fields")
	}
	var sources []string
	for i, name := range path {
		raw, refusal := t.heldRecordJSON(c, typ, id, at)
		if refusal != nil {
			return nil, nil, refusal
		}
		entity := t.records.Type(typ)
		if entity == nil {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path object is unavailable")
		}
		info := entity.Info()
		field, declared := info.Field(name)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		value, visible := fields[name]
		if !declared || !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: []string{name}}, at)
		if !permission.Allow || len(permission.Withheld) > 0 {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		ref := typ + "/" + id
		sources = append(sources, ref, ref+"#"+name)
		if !visible {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input has no value")
		}
		if i == len(path)-1 {
			return value, sources, nil
		}
		if field.Type != "reference" || field.Ref == "" {
			result, err := (platform.Binding{Source: "subject", Path: path[i+1:]}).Resolve(&platform.Run{}, value)
			if err != nil {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return result, sources, nil
		}
		if json.Unmarshal(value, &id) != nil || id == "" {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input reference is empty")
		}
		typ = field.Ref
	}
	return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input is unavailable")
}

func (t *Tenant) heldRecordJSON(c platform.Caller, typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
	entity := t.records.Type(typ)
	if entity == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path object is unavailable")
	}
	visible, refusal := t.visibleForCallerIn(t.records, c, entity, at)
	if refusal != nil {
		return nil, refusal
	}
	row := entity.Rows().Get(id)
	if row == nil || visible != nil && !visible(row.Value) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path record is unavailable")
	}
	_, hidden := viewOf(c.Member, entity)
	record := records.Mask(entity.Info(), row.Value, hidden)
	if entity.Info().HasReadSources() || len(entity.Info().VersionPins) > 0 {
		narrowed, _ := t.narrowerIn(t.records, c.Member, at, true).Derive(entity, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return raw, nil
}
