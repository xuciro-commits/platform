package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

type constraintWrite struct {
	caller platform.Caller
	before *row
}

// Refusal facts are durable, but their values are re-filtered at delivery.
// Revoking a field/row grant must also narrow an idempotent refused answer.
func (t *Tenant) readableFieldIssues(member platform.Member, issues []platform.FieldIssue, at time.Time) []platform.FieldIssue {
	issues = slices.Clone(issues)
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	for i, issue := range issues {
		issues[i].Values = maps.Clone(issue.Values)
		if issue.Code == "business_refusal" {
			if !t.readableIn(t.records, member, issue.Record, at) {
				issues[i].Values = nil
				issues[i].Translations = nil
				issues[i].Message = "The business record is no longer readable"
			}
			continue
		}
		if issue.Constraint == "" {
			continue
		}
		typ, id, _ := strings.Cut(issue.Record, "/")
		et := t.records.types[typ]
		if et == nil {
			issues[i].Values = nil
			continue
		}
		info := platform.ConstraintEntity(et.info)
		if et.rows.get(id) != nil && !t.readableIn(t.records, member, issue.Record, at) {
			issues[i].Values = nil
			continue
		}
		if current := et.rows.get(id); current != nil && et.info.HasReadSources() {
			narrowed, _ := t.narrowerIn(t.records, member, at, true).derive(et, current.value)
			for _, derivation := range et.info.Derived {
				paths := derivation.Fields
				if len(derivation.List) > 0 {
					paths = [][]int{derivation.List}
				}
				for _, indices := range paths {
					if reflect.DeepEqual(current.value.FieldByIndex(indices).Interface(), narrowed.FieldByIndex(indices).Interface()) {
						continue
					}
					delete(issues[i].Values, fieldName(et.info.Go, indices))
				}
			}
		}
		for name := range issues[i].Values {
			field, ok := info.Field(name)
			if !ok || !field.ReadsAny(member.RolesIn(info.App)) {
				delete(issues[i].Values, name)
			}
		}
	}
	return issues
}

func (t *Tenant) localizedFieldIssues(member platform.Member, issues []platform.FieldIssue, language string) []platform.FieldIssue {
	issues = slices.Clone(issues)
	for i := range issues {
		if translated := issues[i].Translations[language]; translated != "" {
			issues[i].Message = translated
		} else {
			issues[i].Message = t.i18n.said(&kernel.Error{Message: issues[i].Message}, language).Message
		}
	}
	return issues
}

func (d *stagedDecision) constraintLookup(typ string) (platform.EntityInfo, bool) {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	et := d.records.types[typ]
	if et == nil {
		return platform.EntityInfo{}, false
	}
	return platform.ConstraintEntity(et.info), true
}

func constraintValues(info platform.EntityInfo, value reflect.Value) map[string]any {
	values := map[string]any{"id": recordOf(value).ID, "archived": recordOf(value).Archived}
	for _, field := range info.Fields {
		values[field.Name] = value.FieldByIndex(field.Index).Interface()
	}
	return values
}

func constraintEmpty(value any) bool {
	v := reflect.ValueOf(value)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return true
	}
	if v.Kind() == reflect.String {
		return strings.TrimSpace(v.String()) == ""
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		return v.Len() == 0
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).IsZero()
	}
	return false // a stored zero number or false is present
}

// Unique sees the complete private owner store under its record lock. The
// tenant's single writer keeps this check and durable acceptance indivisible.
// It never returns the competing record or its fields to the applicant.
func (s *recordStore) uniqueConstraint(info platform.EntityInfo, rule platform.Constraint, value reflect.Value) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.types[info.Type]
	keyOf := func(v reflect.Value) (string, error) {
		values := []any{}
		for _, name := range rule.Fields {
			field, _ := info.Field(name)
			if name == "id" {
				values = append(values, recordOf(v).ID)
			} else if name == "archived" {
				values = append(values, recordOf(v).Archived)
			} else {
				x := v.FieldByIndex(field.Index).Interface()
				if constraintEmpty(x) {
					return "", nil // required is a separate invariant
				}
				values = append(values, x)
			}
		}
		if rule.Scope != "" && rule.Scope != "tenant" {
			path, err := platform.ConstraintScopePath(info, rule.Scope)
			if err != nil {
				return "", err
			}
			current, currentType := v, info
			for i, name := range path {
				field, ok := currentType.Field(name)
				if !ok {
					return "", fmt.Errorf("unique scope is unavailable")
				}
				x := current.FieldByIndex(field.Index)
				if constraintEmpty(x.Interface()) {
					return "", fmt.Errorf("unique scope is missing")
				}
				if i == len(path)-1 {
					values = append(values, field.Ref, x.Interface())
					break
				}
				parent := s.types[field.Ref]
				if parent == nil || parent.info.App != info.App || parent.rows.get(x.String()) == nil {
					return "", fmt.Errorf("unique scope reference is unavailable")
				}
				current, currentType = parent.rows.get(x.String()).value, parent.info
			}
		}
		raw, err := json.Marshal(values)
		return string(raw), err
	}
	key, err := keyOf(value)
	if err != nil || key == "" {
		return err == nil, err
	}
	for id, other := range et.rows.all() {
		if id == recordOf(value).ID {
			continue
		}
		otherKey, err := keyOf(other.value)
		if err != nil {
			// Old incomplete scopes remain readable and occupy no new scope.
			continue
		}
		if key == otherKey {
			return false, nil
		}
	}
	return true, nil
}

func constraintLocked(info platform.EntityInfo, before *row) bool {
	if before == nil || info.Lifecycle == nil {
		return false
	}
	life := info.Lifecycle
	field, _ := info.Field(life.Field)
	if before.value.FieldByIndex(field.Index).String() != life.Initial {
		return true
	}
	for _, change := range before.recordHistory() {
		for _, changed := range change.Fields {
			if changed.Field != life.Field {
				continue
			}
			for _, raw := range []json.RawMessage{changed.Before, changed.After} {
				var state string
				if json.Unmarshal(raw, &state) == nil && state != "" && state != life.Initial {
					return true
				}
			}
		}
	}
	return false
}

func (d *stagedDecision) checkConstraint(c platform.Caller, info platform.EntityInfo, rule platform.Constraint, after *row, before *row) (refusal *kernel.Error) {
	if d.TraceEnabled() {
		defer func() {
			event := platform.DecisionTrace{Kind: "constraint", Name: rule.Name, Type: info.Type, ID: recordOf(after.value).ID, Operation: rule.Kind}
			if refusal != nil {
				event.Refusal = refusal.Message
			}
			d.RecordDecisionTrace(event)
		}()
	}
	values := constraintValues(info, after.value)
	fail := func(reason string) *kernel.Error {
		visible := map[string]json.RawMessage{}
		read, readErr := c.ReadRecord(info.Type, recordOf(after.value).ID, d.at)
		var readable map[string]json.RawMessage
		if readErr == nil {
			_ = json.Unmarshal(read, &readable)
		}
		for _, name := range rule.Fields {
			field, ok := platform.ConstraintEntity(info).Field(name)
			if ok && field.ReadsAny(c.RolesIn(info.App)) && readErr == nil {
				raw := platform.Raw(values[name])
				if len(raw) <= 4096 && string(raw) == string(readable[name]) {
					visible[name] = raw
				}
			}
		}
		return c.RefuseConstraint(rule, info.Type+"/"+recordOf(after.value).ID, visible, reason)
	}
	switch rule.Kind {
	case "required":
		if life := info.Lifecycle; life != nil && values[life.Field] == life.Initial {
			return nil
		}
		for _, name := range rule.Fields {
			if constraintEmpty(values[name]) {
				return fail("required")
			}
		}
	case "unique":
		if rule.Scope != "" && rule.Scope != "tenant" {
			path, err := platform.ConstraintScopePath(info, rule.Scope)
			if err != nil {
				return fail("scope_unavailable")
			}
			if _, _, err := c.ReadRecordPath(info.Type, recordOf(after.value).ID, path, d.at); err != nil {
				return fail("scope_unavailable")
			}
		}
		ok, err := d.records.uniqueConstraint(info, rule, after.value)
		if err != nil {
			return fail("scope_unavailable")
		}
		if !ok {
			return fail("unique")
		}
	case "immutable":
		if constraintLocked(info, before) {
			old := constraintValues(info, before.value)
			for _, field := range rule.Fields {
				if string(platform.Raw(old[field])) != string(platform.Raw(values[field])) {
					return fail("immutable")
				}
			}
		}
	default:
		if rule.Expr == nil {
			return fail("definition_unavailable")
		}
		spec := platform.ConstraintSpec(info, rule)
		lookup := func(typ string) (platform.EntityInfo, bool) {
			other, ok := d.constraintLookup(typ)
			return other, ok && other.App == info.App
		}
		schema, err := platform.RuleSchema(*rule.Expr, spec, lookup)
		if err != nil {
			return fail("definition_unavailable")
		}
		context, err := c.RuleContext(*rule.Expr, spec, lookup, values, nil, d.at)
		if err != nil {
			return fail("input_unavailable")
		}
		program, err := platform.CompileExpr(*rule.Expr, schema)
		if err != nil {
			return fail("definition_unavailable")
		}
		result, err := program.Eval(context)
		if err != nil {
			if issue, ok := err.(*platform.ExprIssue); ok {
				return fail(issue.Code)
			}
			return fail("input_unavailable")
		}
		if string(result.Value.Data) != "true" {
			return fail(rule.Kind)
		}
	}
	return nil
}

// Every staged Put reaches this gate. Record rules see the prospective row and
// earlier private writes. A refusal poisons the whole decision, even if an app
// ignores Put's return value; nothing has yet reached the authoritative store.
func (d *stagedDecision) checkRecordConstraints(c platform.Caller, info platform.EntityInfo, after, before *row) *kernel.Error {
	for _, rule := range info.Constraints {
		if rule.Level == "txn" {
			continue
		}
		if err := d.checkConstraint(c, info, rule, after, before); err != nil {
			return err
		}
	}
	return nil
}

// Final checks cover the actual affected closure, including unchanged parents
// of new/edited children. Unrelated historical violations are not a data clean-
// up obligation. A complete global set deliberately affects every owner row.
func (d *stagedDecision) checkFinalConstraints() *kernel.Error {
	if d.failure != nil {
		return d.failure
	}
	if err := d.checkBusinessVersionPins(); err != nil {
		d.failure = err
		return err
	}
	if err := d.checkOwnedRelationConstraints(); err != nil {
		d.failure = err
		return err
	}
	refs := make([]string, 0, len(d.constraintWrites))
	for ref := range d.constraintWrites {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for _, et := range d.records.sortedTypes() {
		if len(et.info.Constraints) == 0 {
			continue
		}
		plans := make([]constraintReadPlan, len(et.info.Constraints))
		local := true
		for i, rule := range et.info.Constraints {
			plans[i] = d.constraintReadPlan(et.info, rule)
			local = local && plans[i].local
		}
		var ids []string
		if local {
			for _, ref := range refs {
				typ, id, _ := strings.Cut(ref, "/")
				if typ == et.info.Type && et.rows.get(id) != nil {
					ids = append(ids, id)
				}
			}
		} else {
			ids = make([]string, 0, et.rows.len())
			for id := range et.rows.all() {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			ref := et.info.Type + "/" + id
			for i, rule := range et.info.Constraints {
				write, touched := d.constraintWrites[ref]
				if !touched {
					for _, source := range refs {
						candidate := d.constraintWrites[source]
						if candidate.caller.App == et.info.App && d.constraintDepends(et.info, rule, plans[i], et.rows.get(id), source, candidate.before) {
							write, touched = candidate, true
							write.before = et.rows.get(id)
							break
						}
					}
				}
				if !touched {
					continue
				}
				if err := d.checkConstraint(write.caller, et.info, rule, et.rows.get(id), write.before); err != nil {
					d.failure = err
					return err
				}
			}
		}
	}
	return nil
}

func (d *stagedDecision) constraintDepends(info platform.EntityInfo, rule platform.Constraint, plan constraintReadPlan, root *row, changed string, old *row) bool {
	typ, id, _ := strings.Cut(changed, "/")
	rootID := recordOf(root.value).ID
	if rule.Reads != nil {
		for _, set := range rule.Reads.Collections {
			if set.Set != "" {
				index := slices.IndexFunc(info.Sets, func(declaration platform.RecordSet) bool { return declaration.Name == set.Set })
				if index < 0 {
					return true
				}
				declaration := info.Sets[index]
				if (typ == declaration.Object || typ == platform.OwnedRelationType(info.App)) && d.declaredSetDepends(info, root, declaration, typ, id, old) {
					return true
				}
				continue
			}
			if set.Object != typ {
				continue
			}
			if set.Of != "$subject" {
				return true
			}
			q, _, ok := (hostView{t: d.tenant}).RuleQuery(set.App, set.Query, set.Version)
			if !ok {
				return true // missing evidence must refuse, never silently pass
			}
			et := d.records.types[typ]
			if et == nil {
				return true
			}
			field, ok := et.info.Field(q.By)
			if !ok {
				return true
			}
			for _, candidate := range []*row{et.rows.get(id), old} {
				if candidate != nil && candidate.value.FieldByIndex(field.Index).String() == rootID {
					return true
				}
			}
		}
	}
	if plan.unknown {
		return true
	}
	paths := plan.paths
	follows := func(root *row, info platform.EntityInfo, path []string) bool {
		current, currentInfo := root, info
		for _, name := range path {
			field, ok := currentInfo.Field(name)
			if !ok || field.Type != "reference" {
				break
			}
			targetID := current.value.FieldByIndex(field.Index).String()
			if field.Ref == typ && targetID == id {
				return true
			}
			target := d.records.types[field.Ref]
			if target == nil || target.rows.get(targetID) == nil {
				break
			}
			current, currentInfo = target.rows.get(targetID), target.info
		}
		return false
	}
	for _, path := range paths[""] {
		if follows(root, info, path) {
			return true
		}
	}
	for collection, itemPaths := range paths {
		if collection == "" || rule.Reads == nil {
			continue
		}
		set := rule.Reads.Collections[collection]
		if set.Set != "" {
			index := slices.IndexFunc(info.Sets, func(declaration platform.RecordSet) bool { return declaration.Name == set.Set })
			et := d.records.types[set.Object]
			if index < 0 || et == nil {
				return true
			}
			for itemID, item := range et.rows.all() {
				if !d.declaredSetDepends(info, root, info.Sets[index], et.info.Type, itemID, nil) {
					continue
				}
				for _, path := range itemPaths {
					if follows(item, et.info, path) {
						return true
					}
				}
			}
			continue
		}
		q, _, ok := (hostView{t: d.tenant}).RuleQuery(set.App, set.Query, set.Version)
		et := d.records.types[set.Object]
		if !ok || et == nil {
			return true
		}
		// Reuse the original query's fixed domain. An unrelated source used
		// only by excluded rows is not a historical clean-up obligation.
		candidates, err := d.records.matchingQuery(et, platform.Query{Domain: q.Domain}, nil)
		if err != nil {
			return true
		}
		for _, value := range candidates {
			if set.Of == "$subject" {
				field, ok := et.info.Field(q.By)
				if !ok {
					return true
				}
				if value.FieldByIndex(field.Index).String() != rootID {
					continue
				}
			}
			for _, path := range itemPaths {
				if follows(&row{value: value}, et.info, path) {
					return true
				}
			}
		}
	}
	return false
}

type constraintReadPlan struct {
	paths          map[string][][]string
	local, unknown bool
}

// Prepare declaration paths once per final check, not once per candidate row.
func (d *stagedDecision) constraintReadPlan(info platform.EntityInfo, rule platform.Constraint) constraintReadPlan {
	plan := constraintReadPlan{paths: map[string][][]string{}, local: rule.Reads == nil}
	paths := plan.paths
	if rule.Kind == "unique" && rule.Scope != "" && rule.Scope != "tenant" {
		plan.local = false
		path, _ := platform.ConstraintScopePath(info, rule.Scope)
		paths[""] = append(paths[""], path)
	}
	var visit func(platform.Expr, string)
	visit = func(expr platform.Expr, collection string) {
		if expr.Op == "path" || expr.Agg != nil || expr.Window != nil {
			plan.local = false
		}
		if expr.Agg != nil {
			collection = expr.Agg.Source
			if len(expr.Agg.Field) > 1 {
				paths[collection] = append(paths[collection], expr.Agg.Field)
			}
		}
		if expr.Op == "path" && len(expr.Args) == 1 {
			ref := expr.Args[0].Ref
			if strings.HasPrefix(ref, "item.") {
				paths[collection] = append(paths[collection], append([]string{strings.TrimPrefix(ref, "item.")}, expr.Path...))
			} else {
				paths[""] = append(paths[""], append([]string{ref}, expr.Path...))
			}
		}
		for _, child := range expr.Args {
			visit(child, collection)
		}
		if expr.In != nil {
			visit(*expr.In, collection)
		}
	}
	if rule.Expr != nil {
		visit(*rule.Expr, "")
	}
	if rule.Reads != nil {
		for name, binding := range rule.Reads.Collections {
			if binding.Set == "" {
				continue
			}
			index := slices.IndexFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == binding.Set })
			if index < 0 {
				plan.unknown = true
				return plan
			}
			set := info.Sets[index]
			var capture func(platform.Expr)
			capture = func(expr platform.Expr) {
				if expr.Op == "path" && len(expr.Args) == 1 {
					paths[name] = append(paths[name], append([]string{expr.Args[0].Ref}, expr.Path...))
				}
				for _, child := range expr.Args {
					capture(child)
				}
			}
			for _, predicate := range set.Where {
				lowered, err := platform.LowerRecordSetPredicate(set, predicate, d.constraintLookup)
				if err != nil {
					plan.unknown = true
					return plan
				}
				capture(lowered)
			}
			for _, order := range set.Order {
				capture(order.Expr)
			}
		}
	}
	return plan
}

// This is dependency discovery, not a second rule evaluation. It considers
// both sides of a moved reference and archived edges before the original
// caller's constraint is evaluated through the normal scoped reader.
func (d *stagedDecision) declaredSetDepends(info platform.EntityInfo, root *row, set platform.RecordSet, typ, id string, old *row) bool {
	if typ != set.Object && (set.Binding == nil || set.Binding.Ref.Kind != platform.AssetLinkType) {
		return false
	}
	rootID := recordOf(root.value).ID
	rootRef := info.Type + "/" + rootID
	matchField := func(field platform.FieldInfo) bool {
		et := d.records.types[typ]
		if et == nil {
			return true
		}
		for _, candidate := range []*row{et.rows.get(id), old} {
			if candidate != nil && candidate.value.FieldByIndex(field.Index).String() == rootID {
				return true
			}
		}
		return false
	}
	if set.Binding != nil {
		definition, ok := d.tenant.recordSetBinding(*set.Binding)
		if !ok {
			return true
		}
		if query := definition.Query; query != nil {
			if typ != set.Object {
				return false
			}
			if query.By == "" {
				return true
			}
			et := d.records.types[typ]
			if et == nil {
				return true
			}
			field, ok := et.info.Field(query.By)
			return !ok || matchField(field)
		}
		link := definition.LinkType
		if link == nil {
			return true
		}
		if link.Storage == "edge" {
			et := d.records.types[platform.OwnedRelationType(info.App)]
			if et == nil {
				return true
			}
			candidates := []*row{}
			if typ == et.info.Type {
				candidates = append(candidates, et.rows.get(id), old)
			} else {
				for _, edge := range et.rows.all() {
					candidates = append(candidates, edge)
				}
				for ref, write := range d.constraintWrites {
					if strings.HasPrefix(ref, et.info.Type+"/") {
						candidates = append(candidates, write.before)
					}
				}
			}
			for _, edge := range candidates {
				if edge == nil {
					continue
				}
				var value struct{ Binding, Parent, Child string }
				_ = json.Unmarshal(platform.Raw(edge.value.Interface()), &value)
				if value.Binding != link.Name {
					continue
				}
				from, to := value.Parent, value.Child
				if set.Direction == "reverse" {
					from, to = to, from
				}
				if from == rootRef && (typ == et.info.Type || to == typ+"/"+id) {
					return true
				}
			}
			return false
		}
		if typ != set.Object {
			return false
		}
		if set.Direction == "reverse" {
			field, ok := info.Field(link.Via)
			return !ok || root.value.FieldByIndex(field.Index).String() == id
		}
		et := d.records.types[typ]
		if et == nil {
			return true
		}
		field, ok := et.info.Field(link.Via)
		return !ok || matchField(field)
	}
	if set.Direction == "reference" {
		field, ok := info.Field(set.From)
		return !ok || root.value.FieldByIndex(field.Index).String() == id
	}
	et := d.records.types[typ]
	if et == nil {
		return true
	}
	for _, field := range et.info.Fields {
		if field.Inverse == set.From && field.Ref == info.Type {
			return matchField(field)
		}
	}
	return true
}
