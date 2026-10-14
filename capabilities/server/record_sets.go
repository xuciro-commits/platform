package platformserver

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
)

func (t *Tenant) recordSetBinding(binding platform.AssetBinding) (*platform.Definition, bool) {
	for _, definition := range t.definitions {
		if definition.Ref != binding.Ref {
			continue
		}
		if binding.Ref.Kind == platform.AssetLinkType {
			selected := definition.LinkVersion(binding.SourceVersion)
			return selected, selected != nil
		}
		selected := definition.QueryVersion(binding.SourceVersion)
		return selected, selected != nil
	}
	return nil, false
}

func (h hostView) ValidateRecordSet(info platform.EntityInfo, set platform.RecordSet) error {
	if set.Binding == nil {
		return nil
	}
	definition, ok := h.t.recordSetBinding(*set.Binding)
	if !ok {
		return fmt.Errorf("retained collection source is unavailable")
	}
	if query := definition.Query; query != nil {
		if query.Object != set.Object || query.By != "" && !slices.ContainsFunc(mustEntity(h.t, query.Object).Fields, func(field platform.FieldInfo) bool { return field.Name == query.By && field.Ref == info.Type }) {
			return fmt.Errorf("collection query source or parent type differs")
		}
		if set.Direction != "" {
			return fmt.Errorf("query collections have no relationship direction")
		}
		return nil
	}
	link := definition.LinkType
	if link == nil || set.Direction == "forward" && (link.Parent.Name != info.Type || link.Child.Name != set.Object) || set.Direction == "reverse" && (link.Child.Name != info.Type || link.Parent.Name != set.Object) {
		return fmt.Errorf("collection link endpoints differ")
	}
	return nil
}
func mustEntity(t *Tenant, typ string) platform.EntityInfo { info, _ := t.entity(typ); return info }

func (t *Tenant) checkRecordSetDeclarations(info platform.EntityInfo) error {
	lookup := func(typ string) (platform.EntityInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return t.entity(typ)
	}
	if err := platform.CheckVersioning(info, lookup); err != nil {
		return err
	}
	if err := platform.CheckRecordSets(info, lookup); err != nil {
		return err
	}
	for _, set := range info.Sets {
		if err := (hostView{t: t}).ValidateRecordSet(info, set); err != nil {
			return err
		}
	}
	return nil
}

func (r runtime) ReadRecordSet(c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	return r.t.readRecordSetFrom(r.t.records, c, object, id, name, at)
}
func (d *stagedDecision) ReadRecordSet(c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	return d.tenant.readRecordSetFrom(d.records, c, object, id, name, at)
}

// A proposed owner row supplies reference values to its own rules before Put.
// The copied read view has no history, writes or authoritative side effects.
func (d *stagedDecision) ReadRecordSetCandidate(c platform.Caller, object, id, name string, raw json.RawMessage, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	store := d.records.forkRecords()
	et := store.types[object]
	if !c.Staging() || et == nil || et.info.App != c.App || id == "" {
		return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection candidate is outside its owner")
	}
	value := reflect.New(et.info.Go).Elem()
	if json.Unmarshal(raw, value.Addr().Interface()) != nil || recordOf(value).ID != id {
		return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Collection candidate is invalid")
	}
	if before := et.rows.get(id); before != nil {
		recordOf(value).Created = recordOf(before.value).Created
	} else {
		recordOf(value).Created.By = c.ID
	}
	et.setRow(id, &row{value: value})
	return d.tenant.readRecordSetFrom(store, c, object, id, name, at)
}

func (t *Tenant) readRecordSetFrom(store *recordStore, c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	result := platform.RecordSetResult{Records: []json.RawMessage{}}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		store.mu.Lock()
		defer store.mu.Unlock()
		et := store.types[typ]
		if et == nil {
			return platform.EntityInfo{}, false
		}
		return et.info, true
	}
	info, known := lookup(object)
	if !known || !slices.ContainsFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == name }) {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Declared record set is unavailable")
	}
	set := info.Sets[slices.IndexFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == name })]
	result.Object = set.Object
	result.Definition, _ = canonicalDigest(set)
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: object, Record: id}
	outer := t.authorizeFrom(store, c, question, at, false)
	result.Authorization = &platform.AuthzCollection{Decision: outer, Elements: []platform.AuthzElement{}}
	if refusal := authorizationRefusal(outer); refusal != nil {
		return result, refusal
	}
	root, err := t.readRecordFrom(store, c, object, id, at)
	if err != nil {
		return result, err
	}
	var rootValues map[string]json.RawMessage
	_ = json.Unmarshal(root, &rootValues)
	domain := []any{}
	if set.Binding != nil {
		definition, ok := t.recordSetBinding(*set.Binding)
		if !ok {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Retained set source is unavailable")
		}
		if !slices.ContainsFunc(t.Definitions(c.Member), func(d platform.Definition) bool { return d.Ref == set.Binding.Ref }) {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection source is not readable")
		}
		if query := definition.Query; query != nil {
			if len(query.Domain) > 0 {
				if json.Unmarshal(query.Domain, &domain) != nil {
					return result, unknown()
				}
			}
			if query.By != "" {
				domain = append(domain, []any{query.By, "=", id})
			}
		} else if link := definition.LinkType; link != nil {
			if link.Storage == "edge" {
				ids, partial, err := t.ownedRelationTargets(store, c, *link, set.Direction, id, at)
				if err != nil {
					return result, err
				}
				result.Partial = partial
				domain = append(domain, []any{"id", "in", ids})
			} else if set.Direction == "forward" {
				domain = append(domain, []any{link.Via, "=", id})
			} else {
				var parent string
				if field, ok := info.Field(link.Via); !ok || !field.ReadsAny(c.RolesIn(info.App)) {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection reference is withheld")
				}
				_ = json.Unmarshal(rootValues[link.Via], &parent)
				domain = append(domain, []any{"id", "=", parent})
			}
		}
	} else if set.Direction == "reference" {
		field, ok := info.Field(set.From)
		if !ok || !field.ReadsAny(c.RolesIn(info.App)) {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection reference is withheld")
		}
		var target string
		_ = json.Unmarshal(rootValues[set.From], &target)
		domain = append(domain, []any{"id", "=", target})
	} else {
		target, ok := lookup(set.Object)
		index := slices.IndexFunc(target.Fields, func(field platform.FieldInfo) bool { return field.Ref == object && field.Inverse == set.From })
		if !ok || index < 0 {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Collection inverse is unavailable")
		}
		field := target.Fields[index]
		domain = append(domain, []any{field.Name, "=", id})
	}
	store.mu.Lock()
	et := store.types[set.Object]
	store.mu.Unlock()
	if et == nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable")
	}
	visible, refusal := t.visibleIn(store, c.Member, et, at)
	if refusal != nil {
		return result, refusal
	}
	view, hidden := viewOf(c.Member, et)
	// Selection, filtering, ordering and paging use one captured private store.
	// Read grants and derived-source checks still come from the original owner.
	store.mu.Lock()
	query := businessQuery(et.info, platform.Query{Domain: platform.Raw(domain), BusinessTime: set.BusinessTime}, c.Member, at)
	raw, queryErr := store.matchingQuery(et, query, nil)
	values, visibleErr := store.matchingQuery(view, query, visible)
	if queryErr != nil || visibleErr != nil {
		store.mu.Unlock()
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection membership is unavailable")
	}
	result.Partial = result.Partial || len(raw) != len(values)
	decisions := map[string]platform.AuthzDecision{}
	walk := newAuthorizationWalk()
	question.Type = set.Object
	for _, value := range raw {
		question.Record = recordOf(value).ID
		decision := t.authorizeDepth(store, c, question, at, true, 0, walk)
		if !decision.Allow && result.Authorization.Filtered == nil {
			result.Authorization.Filtered = &decision
		}
		if decision.Allow {
			decisions[question.Record] = decision
		}
	}
	narrower := t.narrowerIn(store, c.Member, at, true)
	rows := make([]any, 0, len(values))
	for _, value := range values {
		v := masked(et, value, hidden)
		if et.info.HasReadSources() || len(et.info.VersionPins) > 0 {
			narrowed, _ := narrower.derive(et, reflect.ValueOf(v))
			v = narrowed.Interface()
		}
		rows = append(rows, v)
	}
	store.mu.Unlock()
	ruleLookup := func(typ string) (platform.EntityInfo, bool) {
		other, ok := lookup(typ)
		return platform.ConstraintEntity(other), ok
	}
	spec := platform.RuleReadSpec{Object: set.Object}
	type selected struct {
		raw  json.RawMessage
		id   string
		keys []platform.ExprValue
	}
	selectedRows := []selected{}
	programs := map[string]*platform.ExprProgram{}
	predicates := make([]platform.Expr, len(set.Where))
	for i, where := range set.Where {
		lowered, err := platform.LowerRecordSetPredicate(set, where, lookup)
		if err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		predicates[i] = lowered
	}
	evaluate := func(expr platform.Expr, row any) (platform.ExprValue, *kernel.Error) {
		key := string(platform.Raw(expr))
		program := programs[key]
		if program == nil {
			schema, err := platform.RuleSchema(expr, spec, ruleLookup)
			if err != nil {
				return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			program, err = platform.CompileExpr(expr, schema)
			if err != nil {
				return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			programs[key] = program
		}
		context, captureErr := c.RuleContext(expr, spec, ruleLookup, row, nil, at)
		if captureErr != nil {
			return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, captureErr.Error())
		}
		value, evalErr := program.Eval(context)
		if evalErr != nil {
			return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, evalErr.Error())
		}
		result.Sources = append(result.Sources, context.Sources...)
		return value.Value, nil
	}
	for _, row := range rows {
		value := reflect.New(reflect.TypeOf(row)).Elem()
		value.Set(reflect.ValueOf(row))
		rowValues := constraintValues(et.info, value)
		keep := true
		for _, where := range predicates {
			value, err := evaluate(where, rowValues)
			if err != nil {
				return result, err
			}
			if string(value.Data) != "true" {
				keep = false
				break
			}
		}
		if !keep {
			continue
		}
		item := selected{raw: platform.Raw(row), id: rowValues["id"].(string)}
		for _, order := range set.Order {
			value, err := evaluate(order.Expr, rowValues)
			if err != nil {
				return result, err
			}
			item.keys = append(item.keys, value)
		}
		selectedRows = append(selectedRows, item)
		if len(selectedRows) > set.Limit {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Set {name} contains {count} visible records; limit is {limit}", set.Name, len(selectedRows), set.Limit)
		}
	}
	var orderErr error
	slices.SortStableFunc(selectedRows, func(a, b selected) int {
		for i, key := range a.keys {
			comparison, err := platform.CompareExprValues(key, b.keys[i])
			if err != nil {
				orderErr = err
				return 0
			}
			if set.Order[i].Desc {
				comparison = -comparison
			}
			if comparison != 0 {
				return comparison
			}
		}
		return strings.Compare(a.id, b.id)
	})
	if orderErr != nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, orderErr.Error())
	}
	result.Sources = append(result.Sources, object+"/"+id)
	for _, item := range selectedRows {
		result.Records = append(result.Records, item.raw)
		result.Authorization.Elements = append(result.Authorization.Elements, platform.AuthzElement{Record: item.id, Decision: decisions[item.id]})
		result.Sources = append(result.Sources, set.Object+"/"+item.id)
	}
	slices.Sort(result.Sources)
	result.Sources = slices.Compact(result.Sources)
	return result, nil
}
