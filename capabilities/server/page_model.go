package platformserver

import (
	"encoding/json"
	"fmt"
	pageowner "platformserver/studio/pages"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

func (t *Tenant) modelTransition(ref platform.AssetRef) (platform.Transition, bool) {
	if ref.Kind != platform.AssetAction {
		return platform.Transition{}, false
	}
	owner := t.owner["action:"+ref.Name]
	if owner == nil || owner.Manifest().ID != ref.App {
		return platform.Transition{}, false
	}
	for _, entity := range owner.Manifest().Entities {
		if entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if entity.Type+"."+transition.Name == ref.Name {
				return transition, true
			}
		}
	}
	return platform.Transition{}, false
}

func (t *Tenant) modelCriteria(ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	transition, ok := t.modelTransition(ref)
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
	rows, partial, authorization, err := t.pageModelRows(caller, page, section, request, at)
	if err != nil {
		return result, err
	}
	result.Partial, result.Authorization = partial, authorization
	result.Sort = slices.Clone(request.Sort)
	set := model.Set
	if model.Options != nil && model.Options.Set != nil {
		set = model.Options.Set
	}
	if set != nil && set.Query != nil {
		if named := pageowner.QuerySourceDefinition(t.definitions, set.Query); named != nil && named.Query != nil && len(named.Query.Sort) > 0 {
			result.Sort = slices.Clone(named.Query.Sort)
			result.SortLocked = true
		}
	}
	if set != nil && set.Name != "" {
		root, _ := t.entity(page.RecordResourceObject(set.RecordVariable).Name)
		for _, declared := range root.Sets {
			if declared.Name != set.Name {
				continue
			}
			result.SortLocked = true
			result.Sort = nil
			for _, order := range declared.Order {
				if order.Expr.Op != "" || order.Expr.Ref == "" {
					result.Sort = nil
					break
				}
				name := order.Expr.Ref
				if order.Desc {
					name = "-" + name
				}
				result.Sort = append(result.Sort, name)
			}
		}
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
	selected := []map[string]any{}
	for _, row := range rows {
		id := fmt.Sprint(row["id"])
		ruleRecord := t.pageModelRuleRecord(model.Type, id, row)
		if criteria != nil {
			matched, _, refusal := criteria.Evaluate(caller, ruleRecord, inputs, at, t.entity)
			if refusal != nil {
				if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					result.Partial = true
					continue
				}
				return result, refusal
			}
			if !matched {
				continue
			}
		}
		keep := true
		for _, predicate := range model.Where {
			value, refusal := platform.EvaluateRule(predicate, platform.RuleReadSpec{Object: model.Type}, caller, ruleRecord, nil, at, t.entity)
			if refusal != nil {
				if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					result.Partial = true
					keep = false
					break
				}
				return result, refusal
			}
			if string(value.Value.Data) != "true" {
				keep = false
				break
			}
		}
		if !keep {
			continue
		}
		projectedRow := map[string]any{}
		for key, value := range row {
			projectedRow[key] = value
		}
		access := map[string]platform.AuthzDecision{}
		for _, field := range fieldRefs {
			if field.Key() == "id" {
				continue
			}
			if _, err := platform.RecordPathField(model.Type, field.Segments(), visibleLookup); err != nil {
				result.Partial = true
				delete(projectedRow, field.Key())
				continue
			}
			if len(field.Segments()) > 1 {
				access[field.Key()] = platform.AuthzDecision{Allow: false, Rule: "none", Reason: "Relationship fields are read-only projections", Layers: []platform.AuthzLayer{{Name: "fields", Reason: "Relationship fields are read-only projections"}}}
				value, _, refusal := caller.ReadRecordPath(model.Type, id, field.Segments(), at)
				if refusal != nil {
					result.Partial = true
					delete(projectedRow, field.Key())
					continue
				}
				decoded, decodeErr := platform.DecodeValue(value, 64<<10)
				if decodeErr != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, decodeErr.Error())
				}
				projectedRow[field.Key()] = decoded
			} else {
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
				result.Partial = true
				continue
			}
			var label string
			_ = json.Unmarshal(value.Value.Data, &label)
			projectedRow[result.LabelField] = label
		}
		if request.Search != "" {
			text := string(platform.Raw(projectedRow))
			if !strings.Contains(strings.ToLower(text), strings.ToLower(request.Search)) {
				continue
			}
		}
		result.Access[id] = access
		selected = append(selected, projectedRow)
	}
	if len(request.Sort) > 0 && !result.SortLocked {
		var sortErr error
		slices.SortStableFunc(selected, func(left, right map[string]any) int {
			for _, key := range request.Sort {
				name := strings.TrimPrefix(key, "-")
				field, found := projected.Field(name)
				if !found && name != "id" {
					sortErr = fmt.Errorf("model sort field is unavailable")
					return 0
				}
				kind := platform.RuleFieldKind(field.Type)
				if name == "id" {
					kind = "text"
				}
				comparison, err := t.services.Rules.CompareExprValues(platform.ExprPresent(kind, left[name]), platform.ExprPresent(kind, right[name]))
				if err != nil {
					sortErr = err
					return 0
				}
				if key != name {
					comparison = -comparison
				}
				if comparison != 0 {
					return comparison
				}
			}
			return strings.Compare(fmt.Sprint(left["id"]), fmt.Sprint(right["id"]))
		})
		if sortErr != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, sortErr.Error())
		}
	}
	result.Total = len(selected)
	start := min(request.Offset, len(selected))
	end := min(start+request.Limit, len(selected))
	result.Records = selected[start:end]
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

func (t *Tenant) pageModelRows(c platform.Caller, page platform.Page, section platform.PageDataBinding, request PageModelRead, at time.Time) ([]map[string]any, bool, *platform.AuthzCollection, *kernel.Error) {
	model := section.Model
	set := model.Set
	if model.Options != nil && model.Options.Set != nil {
		set = model.Options.Set
	}
	var source []json.RawMessage
	partial := false
	var authorization *platform.AuthzCollection
	if set != nil && set.Name != "" {
		reference := request.Records[set.RecordVariable]
		if reference.ID == "" {
			return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Select the declared collection's source record")
		}
		value, refusal := c.ReadRecordSet(reference.Object, reference.ID, set.Name, at)
		if refusal != nil {
			return nil, false, nil, refusal
		}
		if value.Object != model.Type {
			return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Declared collection object changed")
		}
		source, partial, authorization = value.Records, value.Partial, value.Authorization
	} else if reference := request.Records["$record"]; reference.ID != "" && section.Kind == "record" {
		value, refusal := c.ReadRecord(reference.Object, reference.ID, at)
		if refusal != nil {
			return nil, false, nil, refusal
		}
		source = []json.RawMessage{value}
	} else {
		domain := []any{}
		if relation := model.Relation; relation != nil {
			reference := request.Records["$parent"]
			if reference.ID == "" {
				return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Select a parent record for this model binding")
			}
			domain = append(domain, []any{relation.Field, "=", reference.ID})
		}
		for offset := 0; ; {
			query := platform.Query{Domain: platform.Raw(domain), Offset: offset, Limit: 200, Sort: []string{"id"}}
			if request.Query != nil && set == nil {
				query = *request.Query
				query.Offset = offset
				query.Limit = 200
				query.Sort = []string{"id"}
				query.Search = ""
				if len(domain) > 0 {
					var terms []any
					_ = json.Unmarshal(query.Domain, &terms)
					terms = append(terms, domain...)
					query.Domain = platform.Raw(terms)
				}
			}
			var rows RecordPage
			var refusal *kernel.Error
			if set != nil && set.Query != nil {
				of := request.Records[set.RecordVariable].ID
				rows, refusal = t.queryWindowRead(c.Member, set.Query.Ref.App, set.Query.Ref.Name, of, set.Query.SourceVersion, query, at)
			} else {
				rows, refusal = t.recordsRead(c.Member, model.Type, query, at)
			}
			if refusal != nil {
				return nil, false, nil, refusal
			}
			partial = partial || rows.Partial
			if rows.Total > platform.RecordSetMaxRows {
				return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model collection exceeds 10000 visible records")
			}
			for _, row := range rows.Records {
				source = append(source, platform.Raw(row))
			}
			if len(source) >= rows.Total {
				break
			}
			if len(rows.Records) == 0 {
				return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Model collection source is incomplete")
			}
			offset += len(rows.Records)
		}
	}
	rows := make([]map[string]any, 0, len(source))
	for _, raw := range source {
		value, err := platform.DecodeValue(raw, 1<<20)
		if err != nil {
			return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		row, ok := value.(map[string]any)
		if !ok {
			return nil, false, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Model source must contain records")
		}
		rows = append(rows, row)
	}
	return rows, partial, authorization, nil
}
