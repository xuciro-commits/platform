package kernel

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	contract "platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type ConstraintWrite struct {
	Caller platform.Caller
	Before *records.Row
}

type ConstraintCheck struct {
	Records     *records.Store
	At          time.Time
	Writes      map[string]ConstraintWrite
	Definitions []platform.Definition
	RuleQuery   func(app, name string, version int) (platform.NamedQuery, int, bool)
	Binding     func(platform.AssetBinding) (*platform.Definition, bool)
	Trace       func(platform.DecisionTrace)
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

func (k ConstraintCheck) lookup(typ string) (platform.EntityInfo, bool) {
	k.Records.Lock()
	defer k.Records.Unlock()
	et := k.Records.Type(typ)
	if et == nil {
		return platform.EntityInfo{}, false
	}
	return platform.ConstraintEntity(et.Info()), true
}

func (k ConstraintCheck) check(c platform.Caller, info platform.EntityInfo, rule platform.Constraint, after *records.Row, before *records.Row) (refusal *contract.Error) {
	if k.Trace != nil {
		defer func() {
			event := platform.DecisionTrace{Kind: "constraint", Name: rule.Name, Type: info.Type, ID: records.Header(after.Value).ID, Operation: rule.Kind}
			if refusal != nil {
				event.Refusal = refusal.Message
			}
			k.Trace(event)
		}()
	}
	values := records.ConstraintValues(info, after.Value)
	fail := func(reason string) *contract.Error {
		visible := map[string]json.RawMessage{}
		read, readErr := c.ReadRecord(info.Type, records.Header(after.Value).ID, k.At)
		var readable map[string]json.RawMessage
		if readErr == nil {
			_ = json.Unmarshal(read, &readable)
		}
		for _, name := range rule.Fields {
			field, ok := platform.ConstraintEntity(info).Field(name)
			if ok && field.ReadsAny(c.RolesIn(info.App), c.Rules()) && readErr == nil {
				raw := platform.Raw(values[name])
				if len(raw) <= 4096 && string(raw) == string(readable[name]) {
					visible[name] = raw
				}
			}
		}
		return c.RefuseConstraint(rule, info.Type+"/"+records.Header(after.Value).ID, visible, reason)
	}
	evaluate := func(subject map[string]any) (bool, *contract.Error) {
		spec := platform.ConstraintSpec(info, rule)
		lookup := func(typ string) (platform.EntityInfo, bool) {
			other, ok := k.lookup(typ)
			return other, ok && other.App == info.App
		}
		schema, err := platform.RuleSchema(*rule.Expr, spec, lookup, c.Rules())
		if err != nil {
			return false, fail("definition_unavailable")
		}
		context, err := c.RuleContext(*rule.Expr, spec, lookup, subject, nil, k.At)
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
			if _, _, err := c.ReadRecordPath(info.Type, records.Header(after.Value).ID, path, k.At); err != nil {
				return fail("scope_unavailable")
			}
		}
		ok, err := k.Records.UniqueConstraint(info, rule, after.Value)
		if err != nil {
			return fail("scope_unavailable")
		}
		if !ok {
			return fail("unique")
		}
	case "immutable":
		locked := constraintLocked(info, before)
		if before != nil && rule.Expr != nil {
			var err *contract.Error
			locked, err = evaluate(records.ConstraintValues(info, before.Value))
			if err != nil {
				return err
			}
		}
		if locked {
			old := records.ConstraintValues(info, before.Value)
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

func (k ConstraintCheck) Record(c platform.Caller, info platform.EntityInfo, after, before *records.Row) *contract.Error {
	for _, rule := range info.Constraints {
		if rule.Level == "txn" {
			continue
		}
		if err := k.check(c, info, rule, after, before); err != nil {
			return err
		}
	}
	return nil
}

func (k ConstraintCheck) Final() *contract.Error {
	if err := k.versionPins(); err != nil {
		return err
	}
	if err := k.ownedRelations(); err != nil {
		return err
	}
	refs := make([]string, 0, len(k.Writes))
	for ref := range k.Writes {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for _, et := range k.Records.SortedTypes() {
		if len(et.Info().Constraints) == 0 {
			continue
		}
		plans := make([]constraintReadPlan, len(et.Info().Constraints))
		local := true
		for i, rule := range et.Info().Constraints {
			plans[i] = k.readPlan(et.Info(), rule)
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
						for dependent := range records.ScopeDependents(k.Records.IndexImage, et.Info(), rule, typ, id).All() {
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
				write, touched := k.Writes[ref]
				if !touched {
					for _, source := range refs {
						candidate := k.Writes[source]
						if candidate.Caller.App == et.Info().App && k.depends(et.Info(), rule, plans[i], et.Rows().Get(id), source, candidate.Before) {
							write, touched = candidate, true
							write.Before = et.Rows().Get(id)
							break
						}
					}
				}
				if !touched {
					continue
				}
				if err := k.check(write.Caller, et.Info(), rule, et.Rows().Get(id), write.Before); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (k ConstraintCheck) depends(info platform.EntityInfo, rule platform.Constraint, plan constraintReadPlan, root *records.Row, changed string, old *records.Row) bool {
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
				if (typ == declaration.Object || typ == platform.OwnedRelationType(info.App)) && k.setDepends(info, root, declaration, typ, id, old) {
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
			q, _, ok := k.RuleQuery(set.App, set.Query, set.Version)
			if !ok {
				return true
			}
			et := k.Records.Type(typ)
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
			target := k.Records.Type(field.Ref)
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
			et := k.Records.Type(set.Object)
			if index < 0 || et == nil {
				return true
			}
			for itemID, item := range et.Rows().All() {
				if !k.setDepends(info, root, info.Sets[index], et.Info().Type, itemID, nil) {
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
		q, _, ok := k.RuleQuery(set.App, set.Query, set.Version)
		et := k.Records.Type(set.Object)
		if !ok || et == nil {
			return true
		}
		candidates, err := k.Records.MatchingQuery(et, platform.Query{Domain: q.Domain}, nil)
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

func (k ConstraintCheck) readPlan(info platform.EntityInfo, rule platform.Constraint) constraintReadPlan {
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
				lowered, err := platform.LowerRecordSetPredicate(set, predicate, k.lookup)
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

func (k ConstraintCheck) setDepends(info platform.EntityInfo, root *records.Row, set platform.RecordSet, typ, id string, old *records.Row) bool {
	if typ != set.Object && (set.Binding == nil || set.Binding.Ref.Kind != platform.AssetLinkType) {
		return false
	}
	rootID := records.Header(root.Value).ID
	rootRef := info.Type + "/" + rootID
	matchField := func(field platform.FieldInfo) bool {
		et := k.Records.Type(typ)
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
		definition, ok := k.Binding(*set.Binding)
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
			et := k.Records.Type(typ)
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
			et := k.Records.Type(platform.OwnedRelationType(info.App))
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
				for ref, write := range k.Writes {
					if strings.HasPrefix(ref, et.Info().Type+"/") {
						candidates = append(candidates, write.Before)
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
		et := k.Records.Type(typ)
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
	et := k.Records.Type(typ)
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

func (k ConstraintCheck) versionPins() *contract.Error {
	types := k.Records.SortedTypes()
	if !slices.ContainsFunc(types, func(et *records.Type) bool { return et.Info().Versioning != nil }) {
		return nil
	}
	pinned := map[string]bool{}
	for _, et := range k.Records.Parent().SortedTypes() {
		for _, current := range et.Rows().All() {
			for _, pin := range records.Header(current.Value).VersionPins {
				pinned[pin.Source] = true
			}
		}
	}
	refs := make([]string, 0, len(k.Writes))
	for ref := range k.Writes {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for _, et := range types {
		if et.Info().Versioning == nil {
			continue
		}
		for id, current := range et.Rows().All() {
			rootRef := et.Info().Type + "/" + id
			if !pinned[rootRef] {
				continue
			}
			if write, touched := k.Writes[rootRef]; touched && write.Before != nil {
				for _, field := range et.Info().Fields {
					if field.Name == et.Info().Versioning.ValidUntil {
						continue
					}
					if !reflect.DeepEqual(current.Value.FieldByIndex(field.Index).Interface(), write.Before.Value.FieldByIndex(field.Index).Interface()) {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Pinned business version content is retained; create another version")
					}
				}
			}
			for _, ref := range refs {
				if ref == rootRef {
					continue
				}
				write := k.Writes[ref]
				typ, changed, _ := strings.Cut(ref, "/")
				for _, set := range et.Info().Sets {
					if k.setDepends(et.Info(), current, set, typ, changed, write.Before) {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A pinned business version's declared records are retained; create another version")
					}
				}
			}
		}
	}
	return nil
}

func (k ConstraintCheck) ownedRelations() *contract.Error {
	for _, definition := range k.Definitions {
		link := definition.LinkType
		if link == nil || link.Storage != "edge" {
			continue
		}
		violations, err := records.RelationViolations(k.Records, *link)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		for _, violation := range violations {
			ref := platform.OwnedRelationType(link.Child.App) + "/" + violation.ID
			if violation.Reason == "required" {
				ref = violation.Child
			}
			write, changed := k.Writes[ref]
			if !changed && violation.Reason == "required" {
				for edgeRef, candidate := range k.Writes {
					if !strings.HasPrefix(edgeRef, platform.OwnedRelationType(link.Child.App)+"/") {
						continue
					}
					id := strings.TrimPrefix(edgeRef, platform.OwnedRelationType(link.Child.App)+"/")
					et := k.Records.Type(platform.OwnedRelationType(link.Child.App))
					for _, row := range []*records.Row{candidate.Before, et.Rows().Get(id)} {
						image := records.ReadRelation(row)
						if image.Binding == link.Name && image.Child == violation.Child {
							write, changed = candidate, true
							break
						}
					}
					if changed {
						break
					}
				}
			}
			if !changed {
				for _, endpoint := range []string{violation.Parent, violation.Child} {
					if prior, ok := k.Writes[endpoint]; ok {
						write, changed = prior, true
						break
					}
				}
			}
			if changed {
				rule := platform.Constraint{Name: "relation." + link.Name, Kind: "cardinality", Fields: []string{"archived"}, Message: "Relationship cardinality or archive policy failed.", Translations: map[string]string{"zh-CN": "关系基数或归档策略不允许这次变更。"}}
				return write.Caller.RefuseConstraint(rule, ref, nil, violation.Reason)
			}
		}
	}
	return nil
}
