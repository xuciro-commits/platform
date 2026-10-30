package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/records"
	pageowner "platformserver/studio/pages"

	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) recordSetBinding(binding platform.AssetBinding) (*platform.Definition, bool) {
	selected := pageowner.QuerySourceDefinition(t.definitions, &binding)
	return selected, selected != nil
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

func (t *Tenant) checkRecordSetDeclarations(info platform.EntityInfo, engines ...platform.RuleEngine) error {
	lookup := func(typ string) (platform.EntityInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return t.entity(typ)
	}
	if err := platform.CheckVersioning(info, lookup); err != nil {
		return err
	}
	if err := platform.CheckRecordSets(info, lookup, engines...); err != nil {
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

func (d *stagedDecision) ReadRecordSetCandidate(c platform.Caller, object, id, name string, raw json.RawMessage, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	store := d.records.ForkRecords()
	et := store.Type(object)
	if !c.Staging() || et == nil || et.Info().App != c.App || id == "" {
		return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection candidate is outside its owner")
	}
	value := reflect.New(et.Info().Go).Elem()
	if json.Unmarshal(raw, value.Addr().Interface()) != nil || records.Header(value).ID != id {
		return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Collection candidate is invalid")
	}
	if before := et.Rows().Get(id); before != nil {
		records.Header(value).Created = records.Header(before.Value).Created
	} else {
		records.Header(value).Created.By = c.ID
	}
	et.SetRow(id, &records.Row{Value: value})
	return d.tenant.readRecordSetFrom(store, c, object, id, name, at)
}

func (t *Tenant) readRecordSetFrom(store *records.Store, c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	result := platform.RecordSetResult{Records: []json.RawMessage{}}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		store.Lock()
		defer store.Unlock()
		et := store.Type(typ)
		if et == nil {
			return platform.EntityInfo{}, false
		}
		return et.Info(), true
	}
	info, known := lookup(object)
	if !known || !slices.ContainsFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == name }) {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Declared record set is unavailable")
	}
	set := info.Sets[slices.IndexFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == name })]
	result.Object = set.Object
	result.Definition, _ = platform.CanonicalDigest(set)
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
	selection := platform.ObjectSet{Object: set.Object, AsOf: set.BusinessTime}
	domain := []any{}
	bindRoot := func(field, root string, value string) {
		selection.Filter = &platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", field), platform.ExprReference("text", "root."+root)}}
		selection.Parameters = map[string]platform.ExprValue{"root." + root: platform.ExprPresent("text", value)}
	}
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
				bindRoot(query.By, "id", id)
			}
		} else if link := definition.LinkType; link != nil {
			if link.Storage == "edge" {
				ids, partial, err := t.ownedRelationTargets(store, c, *link, set.Direction, id, at)
				if err != nil {
					return result, err
				}
				result.Partial = partial
				selection.IDs = ids
			} else if set.Direction == "forward" {
				bindRoot(link.Via, "id", id)
			} else {
				var parent string
				if field, ok := info.Field(link.Via); !ok || !field.ReadsAny(c.RolesIn(info.App)) {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection reference is withheld")
				}
				_ = json.Unmarshal(rootValues[link.Via], &parent)
				bindRoot("id", link.Via, parent)
			}
		}
	} else if set.Direction == "reference" {
		field, ok := info.Field(set.From)
		if !ok || !field.ReadsAny(c.RolesIn(info.App)) {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection reference is withheld")
		}
		var target string
		_ = json.Unmarshal(rootValues[set.From], &target)
		bindRoot("id", set.From, target)
	} else {
		target, ok := lookup(set.Object)
		index := slices.IndexFunc(target.Fields, func(field platform.FieldInfo) bool { return field.Ref == object && field.Inverse == set.From })
		if !ok || index < 0 {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Collection inverse is unavailable")
		}
		field := target.Fields[index]
		bindRoot(field.Name, "id", id)
	}
	selection.Domain = platform.Raw(domain)
	store.Lock()
	et := store.Type(set.Object)
	store.Unlock()
	if et == nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable")
	}
	reader, refusal := t.newSetRead(store, c, at)
	if refusal != nil {
		return result, refusal
	}
	node, refusal := reader.prepare(selection, false, nil)
	if refusal != nil {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection membership is unavailable")
	}
	read := reader.types[set.Object]
	view, hidden := read.view, read.hidden
	decisions := map[string]platform.AuthzDecision{}
	var rows []any
	if refusal := func() *kernel.Error {
		store.Lock()
		defer store.Unlock()
		rawRead := *read
		rawRead.view, rawRead.hidden, rawRead.visible = et, nil, nil
		reader.types[set.Object] = &rawRead
		rawPlan, rawErr := reader.compile(node)
		reader.types[set.Object] = read
		visiblePlan, visibleErr := reader.compile(node)
		if rawErr != nil || visibleErr != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection membership is unavailable")
		}
		var raw, values []reflect.Value
		store.VisitPlan(et, rawPlan.plans[0], nil, func(v reflect.Value) { raw = append(raw, v) })
		store.VisitPlan(view, visiblePlan.plans[0], read.visible, func(v reflect.Value) { values = append(values, v) })
		result.Partial = result.Partial || len(raw) != len(values)
		walk := newAuthorizationWalk()
		question.Type = set.Object
		for _, value := range raw {
			question.Record = records.Header(value).ID
			decision := t.authorizeDepth(store, c, question, at, true, 0, walk)
			if !decision.Allow && result.Authorization.Filtered == nil {
				result.Authorization.Filtered = &decision
			}
			if decision.Allow {
				decisions[question.Record] = decision
			}
		}
		narrower := t.narrowerIn(store, c.Member, at, true)
		rows = make([]any, 0, len(values))
		for _, value := range values {
			v := records.Mask(et.Info(), value, hidden)
			if et.Info().HasReadSources() || len(et.Info().VersionPins) > 0 {
				narrowed, _ := narrower.derive(et, reflect.ValueOf(v))
				v = narrowed.Interface()
			}
			rows = append(rows, v)
		}
		return nil
	}(); refusal != nil {
		return result, refusal
	}
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
	programs := map[string]platform.ExprProgram{}
	predicates := make([]platform.Expr, len(set.Where))
	for i, where := range set.Where {
		lowered, err := platform.LowerRecordSetPredicate(set, where, lookup, c.Rules())
		if err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		predicates[i] = lowered
	}
	evaluate := func(expr platform.Expr, row any) (platform.ExprValue, *kernel.Error) {
		key := string(platform.Raw(expr))
		program := programs[key]
		if program == nil {
			schema, err := platform.RuleSchema(expr, spec, ruleLookup, c.Rules())
			if err != nil {
				return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			program, err = c.Rules().CompileExpr(expr, schema)
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
		rowValues := constraintValues(et.Info(), value)
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
			comparison, err := c.Rules().CompareExprValues(key, b.keys[i])
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
