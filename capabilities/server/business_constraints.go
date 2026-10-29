package platformserver

import (
	"encoding/json"

	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformkernel/kernel"

	"platformserver/kernel/records"
	"platformserver/platform"
)

type constraintWrite struct {
	caller platform.Caller
	before *records.Row
}

// Refusal facts are durable, but their values are re-filtered at delivery.
// Revoking a field/row grant must also narrow an idempotent refused answer.
func (t *Tenant) readableFieldIssues(member platform.Member, issues []platform.FieldIssue, at time.Time) []platform.FieldIssue {
	issues = slices.Clone(issues)
	t.records.Lock()
	defer t.records.Unlock()
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
		et := t.records.Type(typ)
		if et == nil {
			issues[i].Values = nil
			continue
		}
		info := platform.ConstraintEntity(et.Info())
		if et.Rows().Get(id) != nil && !t.readableIn(t.records, member, issue.Record, at) {
			issues[i].Values = nil
			continue
		}
		if current := et.Rows().Get(id); current != nil && et.Info().HasReadSources() {
			narrowed, _ := t.narrowerIn(t.records, member, at, true).derive(et, current.Value)
			for _, derivation := range et.Info().Derived {
				paths := derivation.Fields
				if len(derivation.List) > 0 {
					paths = [][]int{derivation.List}
				}
				for _, indices := range paths {
					if reflect.DeepEqual(current.Value.FieldByIndex(indices).Interface(), narrowed.FieldByIndex(indices).Interface()) {
						continue
					}
					delete(issues[i].Values, fieldName(et.Info().Go, indices))
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
			issues[i].Message = t.i18n.Said(&kernel.Error{Message: issues[i].Message}, language).Message
		}
	}
	return issues
}

func (d *stagedDecision) constraintLookup(typ string) (platform.EntityInfo, bool) {
	d.records.Lock()
	defer d.records.Unlock()
	et := d.records.Type(typ)
	if et == nil {
		return platform.EntityInfo{}, false
	}
	return platform.ConstraintEntity(et.Info()), true
}

func constraintValues(info platform.EntityInfo, value reflect.Value) map[string]any {
	values := map[string]any{"id": records.Header(value).ID, "archived": records.Header(value).Archived}
	for _, field := range info.Fields {
		values[field.Name] = value.FieldByIndex(field.Index).Interface()
	}
	return values
}

func constraintLocked(info platform.EntityInfo, before *records.Row) bool {
	if before == nil || info.Lifecycle == nil {
		return false
	}
	life := info.Lifecycle
	field, _ := info.Field(life.Field)
	if before.Value.FieldByIndex(field.Index).String() != life.Initial {
		return true
	}
	for _, change := range before.RecordHistory() {
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

func (d *stagedDecision) checkConstraint(c platform.Caller, info platform.EntityInfo, rule platform.Constraint, after *records.Row, before *records.Row) (refusal *kernel.Error) {
	if d.TraceEnabled() {
		defer func() {
			event := platform.DecisionTrace{Kind: "constraint", Name: rule.Name, Type: info.Type, ID: records.Header(after.Value).ID, Operation: rule.Kind}
			if refusal != nil {
				event.Refusal = refusal.Message
			}
			d.RecordDecisionTrace(event)
		}()
	}
	values := constraintValues(info, after.Value)
	fail := func(reason string) *kernel.Error {
		visible := map[string]json.RawMessage{}
		read, readErr := c.ReadRecord(info.Type, records.Header(after.Value).ID, d.at)
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
		return c.RefuseConstraint(rule, info.Type+"/"+records.Header(after.Value).ID, visible, reason)
	}
	evaluate := func(subject map[string]any) (bool, *kernel.Error) {
		spec := platform.ConstraintSpec(info, rule)
		lookup := func(typ string) (platform.EntityInfo, bool) {
			other, ok := d.constraintLookup(typ)
			return other, ok && other.App == info.App
		}
		schema, err := platform.RuleSchema(*rule.Expr, spec, lookup, c.Rules())
		if err != nil {
			return false, fail("definition_unavailable")
		}
		context, err := c.RuleContext(*rule.Expr, spec, lookup, subject, nil, d.at)
		if err != nil {
			return false, fail("input_unavailable")
		}
		program, err := c.Rules().CompileExpr(*rule.Expr, schema)
		if err != nil {
			return false, fail("definition_unavailable")
		}
		result, err := program.Eval(context)
		if err != nil {
			if issue, ok := err.(*platform.ExprIssue); ok {
				return false, fail(issue.Code)
			}
			return false, fail("input_unavailable")
		}
		return string(result.Value.Data) == "true", nil
	}
	switch rule.Kind {
	case "required":
		if life := info.Lifecycle; life != nil && values[life.Field] == life.Initial {
			return nil
		}
		for _, name := range rule.Fields {
			if records.ConstraintEmpty(values[name]) {
				return fail("required")
			}
		}
	case "unique":
		if rule.Scope != "" && rule.Scope != "tenant" {
			path, err := platform.ConstraintScopePath(info, rule.Scope)
			if err != nil {
				return fail("scope_unavailable")
			}
			if _, _, err := c.ReadRecordPath(info.Type, records.Header(after.Value).ID, path, d.at); err != nil {
				return fail("scope_unavailable")
			}
		}
		ok, err := d.records.UniqueConstraint(info, rule, after.Value)
		if err != nil {
			return fail("scope_unavailable")
		}
		if !ok {
			return fail("unique")
		}
	case "immutable":
		locked := constraintLocked(info, before)
		if before != nil && rule.Expr != nil {
			var err *kernel.Error
			locked, err = evaluate(constraintValues(info, before.Value))
			if err != nil {
				return err
			}
		}
		if locked {
			old := constraintValues(info, before.Value)
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
		matched, err := evaluate(values)
		if err != nil {
			return err
		}
		if !matched {
			return fail(rule.Kind)
		}
	}
	return nil
}

// Every staged Put reaches this gate. Record rules see the prospective row and
// earlier private writes. A refusal poisons the whole decision, even if an app
// ignores Put's return value; nothing has yet reached the authoritative store.
func (d *stagedDecision) checkRecordConstraints(c platform.Caller, info platform.EntityInfo, after, before *records.Row) *kernel.Error {
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
	for _, et := range d.records.SortedTypes() {
		if len(et.Info().Constraints) == 0 {
			continue
		}
		plans := make([]constraintReadPlan, len(et.Info().Constraints))
		local := true
		for i, rule := range et.Info().Constraints {
			plans[i] = d.constraintReadPlan(et.Info(), rule)
			local = local && (plans[i].local || rule.Kind == "unique" && rule.Reads == nil)
		}
		var ids []string
		if local {
			var candidates records.Index[bool]
			for _, ref := range refs {
				typ, id, _ := strings.Cut(ref, "/")
				if typ == et.Info().Type && et.Rows().Get(id) != nil {
					candidates.Set(id, true)
				}
				for _, rule := range et.Info().Constraints {
					if rule.Kind == "unique" {
						for dependent := range records.ScopeDependents(d.records.IndexImage, et.Info(), rule, typ, id).All() {
							candidates.Set(dependent, true)
						}
					}
				}
			}
			for id := range candidates.All() {
				ids = append(ids, id)
			}
		} else {
			ids = make([]string, 0, et.Rows().Len())
			for id := range et.Rows().All() {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			ref := et.Info().Type + "/" + id
			for i, rule := range et.Info().Constraints {
				write, touched := d.constraintWrites[ref]
				if !touched {
					for _, source := range refs {
						candidate := d.constraintWrites[source]
						if candidate.caller.App == et.Info().App && d.constraintDepends(et.Info(), rule, plans[i], et.Rows().Get(id), source, candidate.before) {
							write, touched = candidate, true
							write.before = et.Rows().Get(id)
							break
						}
					}
				}
				if !touched {
					continue
				}
				if err := d.checkConstraint(write.caller, et.Info(), rule, et.Rows().Get(id), write.before); err != nil {
					d.failure = err
					return err
				}
			}
		}
	}
	return nil
}

func (d *stagedDecision) constraintDepends(info platform.EntityInfo, rule platform.Constraint, plan constraintReadPlan, root *records.Row, changed string, old *records.Row) bool {
	typ, id, _ := strings.Cut(changed, "/")
	rootID := records.Header(root.Value).ID
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
			et := d.records.Type(typ)
			if et == nil {
				return true
			}
			field, ok := et.Info().Field(q.By)
			if !ok {
				return true
			}
			for _, candidate := range []*records.Row{et.Rows().Get(id), old} {
				if candidate != nil && candidate.Value.FieldByIndex(field.Index).String() == rootID {
					return true
				}
			}
		}
	}
	if plan.unknown {
		return true
	}
	paths := plan.paths
	follows := func(root *records.Row, info platform.EntityInfo, path []string) bool {
		current, currentInfo := root, info
		for _, name := range path {
			field, ok := currentInfo.Field(name)
			if !ok || field.Type != "reference" {
				break
			}
			targetID := current.Value.FieldByIndex(field.Index).String()
			if field.Ref == typ && targetID == id {
				return true
			}
			target := d.records.Type(field.Ref)
			if target == nil || target.Rows().Get(targetID) == nil {
				break
			}
			current, currentInfo = target.Rows().Get(targetID), target.Info()
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
			et := d.records.Type(set.Object)
			if index < 0 || et == nil {
				return true
			}
			for itemID, item := range et.Rows().All() {
				if !d.declaredSetDepends(info, root, info.Sets[index], et.Info().Type, itemID, nil) {
					continue
				}
				for _, path := range itemPaths {
					if follows(item, et.Info(), path) {
						return true
					}
				}
			}
			continue
		}
		q, _, ok := (hostView{t: d.tenant}).RuleQuery(set.App, set.Query, set.Version)
		et := d.records.Type(set.Object)
		if !ok || et == nil {
			return true
		}
		// Reuse the original query's fixed domain. An unrelated source used
		// only by excluded rows is not a historical clean-up obligation.
		candidates, err := d.records.MatchingQuery(et, platform.Query{Domain: q.Domain}, nil)
		if err != nil {
			return true
		}
		for _, value := range candidates {
			if set.Of == "$subject" {
				field, ok := et.Info().Field(q.By)
				if !ok {
					return true
				}
				if value.FieldByIndex(field.Index).String() != rootID {
					continue
				}
			}
			for _, path := range itemPaths {
				if follows(&records.Row{Value: value}, et.Info(), path) {
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
func (d *stagedDecision) declaredSetDepends(info platform.EntityInfo, root *records.Row, set platform.RecordSet, typ, id string, old *records.Row) bool {
	if typ != set.Object && (set.Binding == nil || set.Binding.Ref.Kind != platform.AssetLinkType) {
		return false
	}
	rootID := records.Header(root.Value).ID
	rootRef := info.Type + "/" + rootID
	matchField := func(field platform.FieldInfo) bool {
		et := d.records.Type(typ)
		if et == nil {
			return true
		}
		for _, candidate := range []*records.Row{et.Rows().Get(id), old} {
			if candidate != nil && candidate.Value.FieldByIndex(field.Index).String() == rootID {
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
			et := d.records.Type(typ)
			if et == nil {
				return true
			}
			field, ok := et.Info().Field(query.By)
			return !ok || matchField(field)
		}
		link := definition.LinkType
		if link == nil {
			return true
		}
		if link.Storage == "edge" {
			et := d.records.Type(platform.OwnedRelationType(info.App))
			if et == nil {
				return true
			}
			candidates := []*records.Row{}
			if typ == et.Info().Type {
				candidates = append(candidates, et.Rows().Get(id), old)
			} else {
				for _, edge := range et.Rows().All() {
					candidates = append(candidates, edge)
				}
				for ref, write := range d.constraintWrites {
					if strings.HasPrefix(ref, et.Info().Type+"/") {
						candidates = append(candidates, write.before)
					}
				}
			}
			for _, edge := range candidates {
				if edge == nil {
					continue
				}
				var value struct{ Binding, Parent, Child string }
				_ = json.Unmarshal(platform.Raw(edge.Value.Interface()), &value)
				if value.Binding != link.Name {
					continue
				}
				from, to := value.Parent, value.Child
				if set.Direction == "reverse" {
					from, to = to, from
				}
				if from == rootRef && (typ == et.Info().Type || to == typ+"/"+id) {
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
			return !ok || root.Value.FieldByIndex(field.Index).String() == id
		}
		et := d.records.Type(typ)
		if et == nil {
			return true
		}
		field, ok := et.Info().Field(link.Via)
		return !ok || matchField(field)
	}
	if set.Direction == "reference" {
		field, ok := info.Field(set.From)
		return !ok || root.Value.FieldByIndex(field.Index).String() == id
	}
	et := d.records.Type(typ)
	if et == nil {
		return true
	}
	for _, field := range et.Info().Fields {
		if field.Inverse == set.From && field.Ref == info.Type {
			return matchField(field)
		}
	}
	return true
}
