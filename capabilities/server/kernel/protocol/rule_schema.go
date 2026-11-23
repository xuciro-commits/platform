package protocol

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	ruleexpr "platformserver/expr"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

func RuleCollectionSubjectField(object, of string, lookup func(string) (platform.RecordTypeInfo, bool)) (platform.FieldInfo, bool, error) {
	name, selected := strings.CutPrefix(of, "$subject.")
	if !selected {
		return platform.FieldInfo{}, false, nil
	}
	info, ok := lookup(object)
	field, found := info.Field(name)
	if name == "id" && ok {
		field, found = platform.FieldInfo{Name: "id", Type: "text", ReadOnly: true}, true
	}
	if !ok || !found || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, field.Type) {
		return platform.FieldInfo{}, true, fmt.Errorf("collection subject key must be a declared text or record-reference field")
	}
	return field, true, nil
}

func RuleSchema(expr platform.Expr, spec platform.RuleReadSpec, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) (platform.ExprSchema, error) {
	schema := platform.ExprSchema{Refs: map[string]platform.ExprType{"caller.id": {Kind: "text"}, "caller.tenant": {Kind: "text"}, "observation.ready": {Kind: "boolean"}, "observation.source": {Kind: "text"}, "observation.object": {Kind: "text"}, "observation.subject": {Kind: "text"}}, Sets: map[string]platform.ExprSetType{}}
	for key, typ := range spec.Inputs {
		name := "input." + key
		if strings.HasPrefix(key, "step.") || strings.HasPrefix(key, "row.") {
			name = key
		}
		schema.Refs[name] = typ
		if spec.Object == "" {
			schema.Refs[key] = typ
		}
	}
	var record platform.RecordTypeInfo
	if spec.Object != "" {
		var ok bool
		record, ok = lookup(spec.Object)
		if !ok {
			return schema, fmt.Errorf("rule object %s is undeclared", spec.Object)
		}
		for _, f := range record.Fields {
			schema.Refs[f.Name] = platform.ExprType{Kind: platform.RuleFieldKind(f.Type), Object: f.Ref}
		}
		schema.Refs["id"] = platform.ExprType{Kind: "text"}
	}
	for name, set := range spec.Collections {
		if _, _, err := RuleCollectionSubjectField(spec.Object, set.Of, lookup); err != nil {
			return schema, err
		}
		if set.App == "" || set.Query == "" && set.Set == "" || set.Set != "" && set.Query != "" || set.Version < 0 || set.Policy != "visible" && set.Policy != "complete" {
			return schema, fmt.Errorf("rule collection %s needs a declared query/version and scope policy", name)
		}
		if set.Set != "" && (set.App != record.App || set.Version != 0 || set.Of != "" && set.Of != "$subject" || !slices.ContainsFunc(record.Sets, func(declaration platform.RecordSet) bool {
			return declaration.Name == set.Set && declaration.Object == set.Object
		})) {
			return schema, fmt.Errorf("rule collection %s must bind its object's declared set", name)
		}
		info, ok := lookup(set.Object)
		if !ok {
			return schema, fmt.Errorf("rule collection object %s is undeclared", set.Object)
		}
		fields := map[string]platform.ExprType{}
		for _, f := range info.Fields {
			fields[f.Name] = platform.ExprType{Kind: platform.RuleFieldKind(f.Type), Object: f.Ref}
		}
		schema.Sets[name] = platform.ExprSetType{Element: platform.ExprType{Kind: "reference", Object: set.Object, Fields: fields}, Policy: set.Policy}
	}
	var visit func(platform.Expr, string) error
	merge := func(dst, src platform.ExprType) platform.ExprType { return ruleexpr.MergeTypes(dst, src) }
	visit = func(e platform.Expr, itemSet string) error {
		child := itemSet
		if e.Agg != nil {
			if _, ok := spec.Collections[e.Agg.Source]; !ok {
				return fmt.Errorf("rule collection %s is undeclared", e.Agg.Source)
			}
			child = e.Agg.Source
		}
		expand := func(ref string, path []string) error {
			typ := spec.Object
			fieldName := ref
			if strings.HasPrefix(ref, "item.") {
				typ = spec.Collections[child].Object
				fieldName = strings.TrimPrefix(ref, "item.")
			}
			if strings.HasPrefix(ref, "input.") || strings.HasPrefix(ref, "step.") || strings.HasPrefix(ref, "row.") {
				input := spec.Inputs[strings.TrimPrefix(ref, "input.")]
				if input.Kind != "reference" || input.Object == "" {
					return fmt.Errorf("rule path input is not a declared reference")
				}
				final, err := platform.RecordPathField(input.Object, path, lookup)
				if err != nil {
					return err
				}
				leaf := platform.ExprType{Kind: platform.RuleFieldKind(final.Type), Object: final.Ref}
				for i := len(path) - 1; i >= 0; i-- {
					leaf = platform.ExprType{Kind: "reference", Fields: map[string]platform.ExprType{path[i]: leaf}}
				}
				schema.Refs[ref] = merge(schema.Refs[ref], leaf)
				return nil
			}
			info, ok := lookup(typ)
			field, found := info.Field(fieldName)
			if !ok || !found || field.Type != "reference" || field.Ref == "" {
				return fmt.Errorf("rule path base %s is not a declared reference", ref)
			}
			final, err := platform.RecordPathField(field.Ref, path, lookup)
			if err != nil {
				return err
			}
			leaf := platform.ExprType{Kind: platform.RuleFieldKind(final.Type)}
			for i := len(path) - 1; i >= 0; i-- {
				leaf = platform.ExprType{Kind: "reference", Fields: map[string]platform.ExprType{path[i]: leaf}}
			}
			if strings.HasPrefix(ref, "item.") {
				d := schema.Sets[child]
				d.Element.Fields[fieldName] = merge(d.Element.Fields[fieldName], leaf)
				schema.Sets[child] = d
			} else {
				schema.Refs[ref] = merge(schema.Refs[ref], leaf)
			}
			return nil
		}
		if e.Op == "path" && len(e.Args) == 1 {
			if err := expand(e.Args[0].Ref, e.Path); err != nil {
				return err
			}
		}
		if e.Agg != nil && len(e.Agg.Field) > 1 {
			if err := expand("item."+e.Agg.Field[0], e.Agg.Field[1:]); err != nil {
				return err
			}
		}
		for _, arg := range e.Args {
			if err := visit(arg, child); err != nil {
				return err
			}
		}
		if e.In != nil {
			return visit(*e.In, child)
		}
		return nil
	}
	if err := visit(expr, ""); err != nil {
		return schema, err
	}
	if _, err := platform.RuleEngineOf(engines).CompileExpr(expr, schema); err != nil {
		return schema, err
	}
	return schema, nil
}

func RuleContext(c platform.Caller, expr platform.Expr, spec platform.RuleReadSpec, lookup func(string) (platform.RecordTypeInfo, bool), record any, inputs map[string]any, now time.Time) (platform.ExprContext, error) {
	ctx := platform.ExprContext{Refs: map[string]platform.ExprValue{"caller.id": platform.ExprPresent("text", c.ID), "caller.tenant": platform.ExprPresent("text", c.Tenant)}, Sets: map[string]platform.ExprCollection{}, Me: c.ID, Now: now.UTC(), Today: c.Today(now)}
	for name, fact := range observationRuleFacts(c) {
		ctx.Refs[name] = fact
	}
	if c.HasRuntime() && usesOp(expr, "workdays") {
		calendar := c.Calendar(now)
		ctx.Workdays, ctx.Holidays = calendar.Workdays, calendar.Holidays
	}
	for name, typ := range spec.Inputs {
		v, ok := inputs[name]
		if ok {
			if fact, captured := v.(platform.ExprValue); captured && strings.HasPrefix(name, "row.") {
				ctx.Refs[ruleInputName(name)] = fact
			} else {
				ctx.Refs[ruleInputName(name)] = platform.ExprPresent(typ.Kind, v)
			}
			if spec.Object == "" {
				ctx.Refs[name] = ctx.Refs[ruleInputName(name)]
			}
		} else {
			ctx.Refs[ruleInputName(name)] = platform.ExprValue{Kind: typ.Kind, Status: "missing"}
			if spec.Object == "" {
				ctx.Refs[name] = ctx.Refs[ruleInputName(name)]
			}
		}
	}
	var values map[string]json.RawMessage
	if record != nil {
		raw, err := json.Marshal(record)
		if err != nil {
			return ctx, err
		}
		if json.Unmarshal(raw, &values) != nil {
			return ctx, fmt.Errorf("rule record must be an object")
		}
	}
	info, known := lookup(spec.Object)
	if spec.Object != "" && !known {
		return ctx, fmt.Errorf("rule record object is undeclared")
	}
	rv := reflect.ValueOf(record)
	if rv.IsValid() && rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.Kind() == reflect.Struct {
		for i := 0; i < rv.NumField(); i++ {
			sf := rv.Type().Field(i)
			name := strings.Split(sf.Tag.Get("json"), ",")[0]
			if name == "" {
				name = strings.ToLower(sf.Name[:1]) + sf.Name[1:]
			}
			if _, known := info.Field(name); known {
				fv := rv.Field(i)
				if fv.CanInterface() {
					raw, err := json.Marshal(fv.Interface())
					if err != nil {
						return ctx, err
					}
					values[name] = raw
				}
			}
		}
	}
	for _, field := range info.Fields {
		value := platform.ExprValue{Kind: platform.RuleFieldKind(field.Type), Status: "missing"}
		if !field.ReadsAny(c.RolesIn(info.Type), c.Rules()) {
			value.Status = "withheld"
		} else if raw, ok := values[field.Name]; ok {
			value.Status = "value"
			value.Data = raw
			if string(raw) == "null" || string(raw) == `""` && (value.Kind == "date" || value.Kind == "datetime") {
				value.Status = "null"
			}
		}
		ctx.Refs[field.Name] = value
	}
	if spec.Object != "" {
		ctx.Refs["id"] = platform.ExprValue{Kind: "text", Status: "missing"}
		if raw, present := values["id"]; present {
			ctx.Refs["id"] = platform.ExprValue{Kind: "text", Status: "value", Data: raw}
		}
	}
	var visit func(platform.Expr) error
	visit = func(e platform.Expr) error {
		if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") && !strings.HasPrefix(e.Ref, "input.") && e.Ref != "caller.id" && e.Ref != "caller.tenant" && !strings.HasPrefix(e.Ref, "step.") && !strings.HasPrefix(e.Ref, "row.") {
			var id string
			_ = json.Unmarshal(values["id"], &id)
			if id != "" {
				ctx.Sources = append(ctx.Sources, spec.Object+"/"+id)
				if e.Ref != "id" {
					ctx.Sources = append(ctx.Sources, spec.Object+"/"+id+"#"+e.Ref)
				}
			}
		}
		if e.Op == "path" && len(e.Args) == 1 && !strings.HasPrefix(e.Args[0].Ref, "item.") {
			name := e.Args[0].Ref
			field, ok := info.Field(name)
			if strings.HasPrefix(name, "input.") || strings.HasPrefix(name, "step.") || strings.HasPrefix(name, "row.") {
				t := spec.Inputs[strings.TrimPrefix(name, "input.")]
				field = platform.FieldInfo{Type: t.Kind, Ref: t.Object}
				ok = t.Kind == "reference" && t.Object != ""
			}
			if !ok {
				return fmt.Errorf("rule reference %s is undeclared", name)
			}
			value := platform.ExprValue{Kind: e.Kind, Status: "missing"}
			base := ctx.Refs[name]
			if base.Status == "withheld" {
				value.Status = "withheld"
			} else {
				var id string
				if json.Unmarshal(base.Data, &id) == nil && id != "" {
					raw, sources, err := c.ReadRecordPath(field.Ref, id, e.Path, now)
					if err != nil {
						value.Status = rulePathStatus(err)
						if value.Status == "missing" {
							ctx.Sources = append(ctx.Sources, sources...)
						}
					} else {
						value.Status = "value"
						value.Data = raw
						ctx.Sources = append(ctx.Sources, sources...)
					}
				}
			}
			ctx.Refs[name+"."+strings.Join(e.Path, ".")] = value
		}
		for _, arg := range e.Args {
			if err := visit(arg); err != nil {
				return err
			}
		}
		if e.In != nil {
			return visit(*e.In)
		}
		return nil
	}
	if err := visit(expr); err != nil {
		return ctx, err
	}
	used := map[string][]platform.Expr{}
	needed := map[string]map[string]bool{}
	need := func(source, field string) {
		if needed[source] == nil {
			needed[source] = map[string]bool{}
		}
		needed[source][field] = true
	}
	var collect func(platform.Expr, string)
	collect = func(e platform.Expr, scope string) {
		if e.Agg != nil {
			scope = e.Agg.Source
			if _, ok := used[scope]; !ok {
				used[scope] = nil
			}
			if len(e.Agg.Field) > 1 {
				used[scope] = append(used[scope], platform.Expr{Op: "path", Kind: e.Kind, Args: []platform.Expr{platform.ExprReference("reference", "item."+e.Agg.Field[0])}, Path: e.Agg.Field[1:]})
			}
			if len(e.Agg.Field) > 0 {
				need(scope, e.Agg.Field[0])
			}
		}
		if strings.HasPrefix(e.Ref, "item.") {
			need(scope, strings.TrimPrefix(e.Ref, "item."))
		}
		if e.Op == "path" && len(e.Args) == 1 && strings.HasPrefix(e.Args[0].Ref, "item.") {
			used[scope] = append(used[scope], e)
		}
		for _, a := range e.Args {
			collect(a, scope)
		}
		if e.In != nil {
			collect(*e.In, scope)
		}
	}
	collect(expr, "")
	for name, paths := range used {
		set := spec.Collections[name]
		of := ""
		if set.Of != "" {
			if set.Of == "$subject" {
				var id string
				_ = json.Unmarshal(values["id"], &id)
				of = id
			} else if field, selected, err := RuleCollectionSubjectField(spec.Object, set.Of, lookup); selected {
				if err != nil {
					return ctx, err
				}
				value := ctx.Refs[field.Name]
				if value.Status == "withheld" {
					return ctx, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
				}
				if value.Status != "value" || json.Unmarshal(value.Data, &of) != nil || of == "" {
					return ctx, fmt.Errorf("collection %s subject key is unavailable", name)
				}
				var id string
				_ = json.Unmarshal(values["id"], &id)
				if id != "" {
					ctx.Sources = append(ctx.Sources, spec.Object+"/"+id)
					if field.Name != "id" {
						ctx.Sources = append(ctx.Sources, spec.Object+"/"+id+"#"+field.Name)
					}
				}
			} else if value, ok := inputs[set.Of].(string); ok {
				of = value
			} else {
				return ctx, fmt.Errorf("collection %s input is unavailable", name)
			}
		}
		queryInputs := map[string]any{}
		if of != "" {
			queryInputs["for"] = of
		}
		var raw json.RawMessage
		var readErr *platform.Error
		if set.Set != "" {
			var subjectID string
			_ = json.Unmarshal(values["id"], &subjectID)
			if subjectID == "" {
				raw = platform.Raw(map[string]any{"records": []any{}, "total": 0, "sources": []string{}, "partial": false})
			} else {
				page, err := ruleRecordSet(c, spec.Object, subjectID, set.Set, record, now)
				readErr = err
				if err == nil {
					raw = platform.Raw(map[string]any{"records": page.Records, "total": len(page.Records), "sources": page.Sources, "partial": page.Partial})
				}
			}
		} else {
			raw, readErr = c.ReadRuleQuery(set, platform.Raw(queryInputs), now)
		}
		if readErr != nil {
			if readErr.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && readErr.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
				return ctx, fmt.Errorf("collection %s: %s", name, readErr.Message)
			}
			ctx.Sets[name] = platform.ExprCollection{Status: "withheld"}
			continue
		}
		var page struct {
			Records []map[string]json.RawMessage `json:"records"`
			Total   int                          `json:"total"`
			Sources []string                     `json:"sources"`
			Partial bool                         `json:"partial"`
		}
		if json.Unmarshal(raw, &page) != nil {
			return ctx, fmt.Errorf("collection %s returned an invalid record page", name)
		}
		ctx.Sources = append(ctx.Sources, page.Sources...)
		collection := platform.ExprCollection{Complete: page.Total == len(page.Records), Withheld: page.Partial, Items: []platform.ExprItem{}}
		object, ok := lookup(set.Object)
		if !ok {
			return ctx, fmt.Errorf("collection object is unavailable")
		}
		for _, row := range page.Records {
			var id string
			_ = json.Unmarshal(row["id"], &id)
			item := platform.ExprItem{ID: id, Fields: map[string]platform.ExprValue{}}
			for _, field := range object.Fields {
				if !needed[name][field.Name] {
					continue
				}
				v := platform.ExprValue{Kind: platform.RuleFieldKind(field.Type), Status: "missing"}
				if !field.ReadsAny(c.RolesIn(object.Type), c.Rules()) {
					v.Status = "withheld"
					collection.Withheld = true
				} else if raw, ok := row[field.Name]; ok {
					v.Status = "value"
					v.Data = raw
				}
				item.Fields[field.Name] = v
			}
			for _, path := range paths {
				fieldName := strings.TrimPrefix(path.Args[0].Ref, "item.")
				field, _ := object.Field(fieldName)
				v := platform.ExprValue{Kind: path.Kind, Status: "missing"}
				base := item.Fields[fieldName]
				if base.Status == "withheld" {
					v.Status = "withheld"
					collection.Withheld = true
				} else {
					var ref string
					if json.Unmarshal(base.Data, &ref) == nil && ref != "" {
						raw, sources, err := c.ReadRecordPath(field.Ref, ref, path.Path, now)
						if err != nil {
							v.Status = rulePathStatus(err)
							collection.Withheld = collection.Withheld || v.Status == "withheld"
							if v.Status == "missing" {
								ctx.Sources = append(ctx.Sources, sources...)
							}
						} else {
							v.Status = "value"
							v.Data = raw
							ctx.Sources = append(ctx.Sources, sources...)
						}
					}
				}
				item.Fields[fieldName+"."+strings.Join(path.Path, ".")] = v
			}
			collection.Items = append(collection.Items, item)
		}
		ctx.Sets[name] = collection
		raw, budgetErr := json.Marshal(ctx)
		if budgetErr != nil || len(raw) > platform.ExprMaxContextBytes {
			return ctx, fmt.Errorf("captured rule context is %d bytes; limit is %d", len(raw), platform.ExprMaxContextBytes)
		}
	}
	return ctx, nil
}

func rulePathStatus(err *platform.Error) string {
	if err.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		return "missing"
	}
	return "withheld"
}

func ExprGoValue(v platform.ExprValue) (any, error) {
	if v.Status != "value" {
		return nil, fmt.Errorf("rule value is %s", v.Status)
	}
	if v.Kind == "integer" || v.Kind == "decimal" || v.Kind == "number" {
		var text string
		if json.Unmarshal(v.Data, &text) == nil {
			return json.Number(text), nil
		}
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(v.Data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if v.Kind == "money" {
		if m, ok := value.(map[string]any); ok {
			if amount, ok := m["amount"].(string); ok {
				m["amount"] = json.Number(amount)
			}
		}
	}
	return value, nil
}

func ruleInputName(name string) string {
	if strings.HasPrefix(name, "step.") || strings.HasPrefix(name, "row.") {
		return name
	}
	return "input." + name
}

func observationRuleFacts(c platform.Caller) map[string]platform.ExprValue {
	out := map[string]platform.ExprValue{"observation.ready": platform.ExprPresent("boolean", false), "observation.source": {Kind: "text", Status: "missing"}, "observation.object": {Kind: "text", Status: "missing"}, "observation.subject": {Kind: "text", Status: "missing"}}
	if proof := c.ObservationContext(); proof != nil && proof.Intervals != nil && proof.Intervals.Ready {
		object, subject, ok := strings.Cut(proof.Record, "/")
		if !ok {
			return out
		}
		out["observation.ready"] = platform.ExprPresent("boolean", true)
		out["observation.source"] = platform.ExprPresent("text", proof.Definition.Ref.String())
		out["observation.object"] = platform.ExprPresent("text", object)
		out["observation.subject"] = platform.ExprPresent("text", subject)
	}
	return out
}

func ruleRecordSet(c platform.Caller, object, id, name string, record any, at time.Time) (platform.RecordSetResult, *platform.Error) {
	if runtime := c.StagedDecision(); runtime != nil {
		return runtime.ReadDecisionRecordSet(c, object, id, name, platform.Raw(record), at)
	}
	return c.ReadRecordSet(object, id, name, at)
}

func usesOp(e platform.Expr, op string) bool {
	if e.Op == op {
		return true
	}
	for _, arg := range e.Args {
		if usesOp(arg, op) {
			return true
		}
	}
	return e.In != nil && usesOp(*e.In, op)
}
