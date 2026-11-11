package pages

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
)

type ModelHost interface {
	ModelMember(platform.Member) (platform.Member, *kernel.Error)
	AuthorizePreview(platform.Member) *kernel.Error
	ModelCaller(m platform.Member, app string) platform.Caller
	FilterCaller(platform.Caller) platform.Caller
	Entity(typ string) (platform.EntityInfo, bool)
	Definitions(platform.Member) []platform.Definition
	AllDefinitions() []platform.Definition
	ModelQueryBinding(platform.AssetRef) (platform.AssetBinding, bool)
	ModelCriteria(platform.AssetRef) (*platform.RuleCriteria, bool)
	Rules() platform.RuleEngine
	VisibleLookup(platform.Member) func(string) (platform.EntityInfo, bool)
	RuleEntities() map[string]platform.EntityInfo
	RuleRecord(typ, id string, fallback any) any
	LoadObjectSet(c platform.Caller, set platform.ObjectSet, w objects.Window, at time.Time, bound ...platform.NamedQuery) (objects.RecordPage, string, *kernel.Error)
}

type ModelReader struct{ host ModelHost }

func NewModelReader(host ModelHost) ModelReader { return ModelReader{host: host} }

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

func (r ModelReader) Read(member platform.Member, request PageModelRead, at time.Time) (PageModelResult, *kernel.Error) {
	result := PageModelResult{Records: []map[string]any{}, Fields: []platform.FieldInfo{}, Access: map[string]map[string]platform.AuthzDecision{}}
	if request.Offset < 0 || request.Limit < 1 || request.Limit > 200 || len(request.Records) > 128 || len(request.Values) > 128 || len(request.Search) > 1024 || len(request.Sort) > 16 {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model read exceeds its bounded window")
	}
	member, err := r.host.ModelMember(member)
	if err != nil {
		return result, err
	}
	var page platform.Page
	if request.Draft != nil {
		if err := r.host.AuthorizePreview(member); err != nil {
			return result, err
		}
		if err := json.Unmarshal(platform.Raw(request.Draft), &page); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid page model")
		}
		if err := page.NormalizeModelBindings(r.host.Entity); err != nil {
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
					if binding, ok := r.host.ModelQueryBinding(model.Set.Query.Ref); ok {
						*model.Set.Query = binding
					}
				}
			}
		}
	} else {
		for _, definition := range r.host.Definitions(member) {
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
		if err := page.CheckDataBinding(binding, r.host.Entity, r.host.ModelCriteria, r.host.Rules()); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}

	if err := CheckModelQueries(page, r.host.AllDefinitions(), r.host.Entity); err != nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	index := slices.IndexFunc(page.DataBindings, func(section platform.PageDataBinding) bool { return section.ID == request.Section })
	if index < 0 || page.DataBindings[index].PrimaryModel() == nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Model section is unavailable")
	}
	section := page.DataBindings[index]
	if len(section.Models) > 0 {
		return r.composite(member, page, section, request, at)
	}
	return r.source(member, page, section, request, at)
}

func (r ModelReader) source(member platform.Member, page platform.Page, section platform.PageDataBinding, request PageModelRead, at time.Time) (PageModelResult, *kernel.Error) {
	result := PageModelResult{Records: []map[string]any{}, Fields: []platform.FieldInfo{}, Access: map[string]map[string]platform.AuthzDecision{}}
	model := section.Model
	result.Model, result.Object = model, model.Type
	info, known := r.host.Entity(model.Type)
	if !known {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Model object is unavailable")
	}
	caller := r.host.ModelCaller(member, info.App)
	visibleLookup := r.host.VisibleLookup(member)
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
		criteria, known = r.host.ModelCriteria(rule.Action)
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
	window, err := r.rows(caller, page, section, request, projection, criteria, inputs, at)
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
		ruleRecord := r.host.RuleRecord(model.Type, id, row)
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
				read := caller.Authorize(platform.AuthzRequest{Principal: member.ID, Tenant: caller.Tenant, Kind: "read", Type: model.Type, Record: id, Fields: []string{field.Name}}, at)
				if !read.Allow || len(read.Withheld) > 0 {
					result.Partial = true
					delete(row, field.Key())
				}
				decision := caller.Authorize(platform.AuthzRequest{Principal: member.ID, Tenant: caller.Tenant, Kind: "write", Type: model.Type, Record: id, Fields: []string{field.Name}}, at)
				if f, ok := visibleInfo.Field(field.Name); ok && f.ReadOnly {
					decision.Allow = false
					decision.Reason = "The original model declares this field read-only"
				}
				access[field.Key()] = decision
			}
		}
		if model.Label != nil {
			value, refusal := platform.EvaluateRule(*model.Label, platform.RuleReadSpec{Object: model.Type}, caller, ruleRecord, nil, at, r.host.Entity)
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

func (r ModelReader) rows(c platform.Caller, page platform.Page, section platform.PageDataBinding, request PageModelRead, projection []string, criteria *platform.RuleCriteria, inputs map[string]any, at time.Time) (pageModelRowsResult, *kernel.Error) {
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
		if root, ok := r.host.Entity(page.RecordResourceObject(collection.RecordVariable).Name); ok {
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
		query, ok := r.query(c.Member, collection)
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
	allEntities := r.host.RuleEntities()
	var filter *pageModelFilter
	if criteria != nil || len(model.Where) > 0 {
		filter = &pageModelFilter{
			caller: r.host.FilterCaller(c),
			model:  model, criteria: criteria, inputs: inputs, at: at,
			lookup: func(typ string) (platform.EntityInfo, bool) {
				info, ok := allEntities[typ]
				return platform.ConstraintEntity(info), ok
			},
		}
	}
	var projectedErr *kernel.Error
	window := objects.Window{
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
	loaded, object, err := r.host.LoadObjectSet(c, set, window, at, bound...)
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

func (r ModelReader) query(member platform.Member, collection *platform.ModelRecordSet) (platform.NamedQuery, bool) {
	for _, definition := range r.host.Definitions(member) {
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

type PageCompositeSource struct {
	Object  string           `json:"object"`
	Records []map[string]any `json:"records"`
}
type PageMatrixCell struct {
	Row    string `json:"row"`
	Column string `json:"column"`
	Value  any    `json:"value"`
	Kind   string `json:"kind"`
}
type PageCompositeResult struct {
	Sources map[string]PageCompositeSource `json:"sources"`
	Cells   []PageMatrixCell               `json:"cells,omitempty"`
}

func (r ModelReader) composite(member platform.Member, page platform.Page, section platform.PageDataBinding, request PageModelRead, at time.Time) (PageModelResult, *kernel.Error) {
	result := PageModelResult{Object: section.PrimaryModel().Type, Records: []map[string]any{}, Fields: []platform.FieldInfo{}, Access: map[string]map[string]platform.AuthzDecision{}, Composite: &PageCompositeResult{Sources: map[string]PageCompositeSource{}}}
	c := section
	recordLimit, cellLimit, byteLimit := platform.CompositeLimits()
	keys := make([]string, 0, len(c.Models))
	for key := range c.Models {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		binding := c.Models[key]
		info, _ := r.host.Entity(binding.Model.Type)
		model := binding.Model
		model.Fields = slices.Clone(model.Fields)
		roles := make([]string, 0, len(binding.Roles))
		for role := range binding.Roles {
			roles = append(roles, role)
		}
		slices.Sort(roles)
		for _, role := range roles {
			field := binding.Roles[role]
			model.Fields = append(model.Fields, field)
		}
		read := request
		read.Offset, read.Limit, read.Search, read.Sort, read.Query = 0, recordLimit, "", nil, nil
		read.Records = map[string]PageModelRecord{}
		for name, record := range request.Records {
			if name != "$record" && name != "$parent" {
				read.Records[name] = record
			}
		}
		value, err := r.source(member, page, platform.PageDataBinding{Model: &model, ID: section.ID, Kind: "records"}, read, at)
		if err != nil {
			return result, err
		}
		if value.Total > read.Limit {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Composite source exceeds 200 records; bind a narrower declared collection")
		}
		result.Partial = result.Partial || value.Partial
		result.Hidden = result.Hidden || value.Hidden
		caller := r.host.ModelCaller(member, info.App)
		rows := []map[string]any{}
		for _, row := range value.Records {
			id := fmt.Sprint(row["id"])
			ruleRecord := r.host.RuleRecord(model.Type, id, row)
			for _, role := range roles {
				field := binding.Roles[role]
				if v, ok := row[field.Key()]; ok {
					row["$view."+role] = v
				}
			}
			if value.LabelField != "" {
				if _, explicit := binding.Roles["label"]; !explicit {
					row["$view.label"] = row[value.LabelField]
				}
			}
			denied := false
			for role, expr := range binding.Expressions {
				answer, refusal := platform.EvaluateRule(expr, platform.RuleReadSpec{Object: model.Type}, caller, ruleRecord, nil, at, r.host.Entity)
				if refusal != nil {
					if refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
						return result, refusal
					}
					result.Partial, denied = true, true
					break
				}
				var decoded any
				if err := json.Unmarshal(answer.Value.Data, &decoded); err != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Composite expression returned an unreadable value")
				}
				row["$view."+role] = decoded
			}
			if denied {
				if model.Visibility != "mark" {
					result.Hidden = true
				}
				continue
			}
			rows = append(rows, row)
		}
		result.Composite.Sources[key] = PageCompositeSource{Object: model.Type, Records: rows}
	}
	if result.Hidden {
		result.Composite.Sources = map[string]PageCompositeSource{}
		return result, nil
	}
	if _, graph := section.Models["nodes"]; graph {
		if err := checkCompositeGraph(result.Composite, c.Acyclic, result.Partial); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if section.Value != nil {
		rows, columns := result.Composite.Sources["rows"], result.Composite.Sources["columns"]
		if len(rows.Records)*len(columns.Records) > cellLimit {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Matrix exceeds 1600 cells; bind narrower declared collections")
		}
		info, _ := r.host.Entity(rows.Object)
		caller := r.host.ModelCaller(member, info.App)
		for _, row := range rows.Records {
			for _, column := range columns.Records {
				answer, err := platform.EvaluateRule(*c.Value, platform.MatrixRuleReads(c), caller, nil, map[string]any{"row": row["id"], "column": column["id"]}, at, r.host.Entity)
				if err != nil {
					if err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
						return result, err
					}
					result.Partial = true
					if c.Models["rows"].Model.Visibility != "mark" || c.Models["columns"].Model.Visibility != "mark" {
						result.Hidden = true
						result.Composite.Sources = map[string]PageCompositeSource{}
						result.Composite.Cells = nil
						return result, nil
					}
					continue
				}
				var value any
				if err := json.Unmarshal(answer.Value.Data, &value); err != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Matrix expression returned an unreadable value")
				}
				result.Composite.Cells = append(result.Composite.Cells, PageMatrixCell{Row: fmt.Sprint(row["id"]), Column: fmt.Sprint(column["id"]), Value: value, Kind: c.Value.Kind})
			}
		}
	}
	if len(platform.Raw(result)) > byteLimit {
		return PageModelResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Composite result exceeds its byte budget")
	}
	return result, nil
}

func checkCompositeGraph(data *PageCompositeResult, acyclic, partial bool) error {
	nodes := map[string]bool{}
	layers := map[string]bool{}
	for _, row := range data.Sources["layers"].Records {
		layers[fmt.Sprint(row["id"])] = true
	}
	for _, row := range data.Sources["nodes"].Records {
		nodes[fmt.Sprint(row["id"])] = true
		if layer, ok := row["$view.layer"]; ok && layer != nil && layer != "" && !layers[fmt.Sprint(layer)] && !partial {
			return fmt.Errorf("Graph node names a layer outside its bound collection")
		}
	}
	adjacent := map[string][]string{}
	for _, edge := range data.Sources["edges"].Records {
		from, to := fmt.Sprint(edge["$view.from"]), fmt.Sprint(edge["$view.to"])
		if !nodes[from] || !nodes[to] {
			if !partial {
				return fmt.Errorf("Graph edge names a node outside its bound collection")
			}
			continue
		}
		adjacent[from] = append(adjacent[from], to)
	}
	for _, row := range data.Sources["overlay"].Records {
		if !nodes[fmt.Sprint(row["$view.node"])] && !partial {
			return fmt.Errorf("Graph overlay names a node outside its bound collection")
		}
	}
	if acyclic {
		state := map[string]int{}
		var visit func(string) bool
		visit = func(id string) bool {
			if state[id] == 1 {
				return false
			}
			if state[id] == 2 {
				return true
			}
			state[id] = 1
			for _, next := range adjacent[id] {
				if !visit(next) {
					return false
				}
			}
			state[id] = 2
			return true
		}
		for id := range nodes {
			if !visit(id) {
				return fmt.Errorf("The bound graph contains a cycle")
			}
		}
	}
	return nil
}
