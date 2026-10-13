package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strings"
	"time"
)

// RuleReadSpec binds rules to existing object/query declarations. It confers no
// read grant and never accepts a caller-supplied query or record image as fact.
type RuleReadSpec struct {
	Object      string                    `json:"object,omitempty"`
	Inputs      map[string]ExprType       `json:"inputs,omitempty"`
	Collections map[string]RuleCollection `json:"collections,omitempty"`
}
type RuleCollection struct {
	Set     string `json:"set,omitempty"`
	App     string `json:"app"`
	Query   string `json:"query"`
	Version int    `json:"version"`
	Object  string `json:"object"`
	Of      string `json:"of,omitempty"`
	Policy  string `json:"policy"` // visible: declared scoped set; complete: reject withheld fields
}

func RuleFieldKind(kind string) string {
	switch kind {
	case "longtext", "text", "string":
		return "text"
	case "number":
		return "number"
	default:
		return kind
	}
}

// RuleSchema checks every reference path against the original object owner.
// Only paths actually named in the immutable expression are expanded, so
// cyclic object references do not create an unbounded schema tree.
func RuleSchema(expr Expr, spec RuleReadSpec, lookup func(string) (EntityInfo, bool)) (ExprSchema, error) {
	schema := ExprSchema{Refs: map[string]ExprType{"caller.id": {Kind: "text"}}, Sets: map[string]ExprSetType{}}
	for key, typ := range spec.Inputs {
		name := "input." + key
		if strings.HasPrefix(key, "step.") {
			name = key
		}
		schema.Refs[name] = typ
		if spec.Object == "" {
			schema.Refs[key] = typ
		}
	}
	var record EntityInfo
	if spec.Object != "" {
		var ok bool
		record, ok = lookup(spec.Object)
		if !ok {
			return schema, fmt.Errorf("rule object %s is undeclared", spec.Object)
		}
		for _, f := range record.Fields {
			schema.Refs[f.Name] = ExprType{Kind: RuleFieldKind(f.Type), Object: f.Ref}
		}
	}
	for name, set := range spec.Collections {
		if set.App == "" || set.Query == "" && set.Set == "" || set.Set != "" && set.Query != "" || set.Version < 0 || set.Policy != "visible" && set.Policy != "complete" {
			return schema, fmt.Errorf("rule collection %s needs a declared query/version and scope policy", name)
		}
		if set.Set != "" && (set.App != record.App || set.Version != 0 || set.Of != "" && set.Of != "$subject" || !slices.ContainsFunc(record.Sets, func(declaration RecordSet) bool {
			return declaration.Name == set.Set && declaration.Object == set.Object
		})) {
			return schema, fmt.Errorf("rule collection %s must bind its object's declared set", name)
		}
		info, ok := lookup(set.Object)
		if !ok {
			return schema, fmt.Errorf("rule collection object %s is undeclared", set.Object)
		}
		fields := map[string]ExprType{}
		for _, f := range info.Fields {
			fields[f.Name] = ExprType{Kind: RuleFieldKind(f.Type), Object: f.Ref}
		}
		schema.Sets[name] = ExprSetType{Element: ExprType{Kind: "reference", Object: set.Object, Fields: fields}, Policy: set.Policy}
	}
	var visit func(Expr, string) error
	merge := func(dst, src ExprType) ExprType { return mergeRuleTypes(dst, src) }
	visit = func(e Expr, itemSet string) error {
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
			if strings.HasPrefix(ref, "input.") || strings.HasPrefix(ref, "step.") {
				input := spec.Inputs[strings.TrimPrefix(ref, "input.")]
				if input.Kind != "reference" || input.Object == "" {
					return fmt.Errorf("rule path input is not a declared reference")
				}
				final, err := RecordPathField(input.Object, path, lookup)
				if err != nil {
					return err
				}
				leaf := ExprType{Kind: RuleFieldKind(final.Type), Object: final.Ref}
				for i := len(path) - 1; i >= 0; i-- {
					leaf = ExprType{Kind: "reference", Fields: map[string]ExprType{path[i]: leaf}}
				}
				schema.Refs[ref] = merge(schema.Refs[ref], leaf)
				return nil
			}
			info, ok := lookup(typ)
			field, found := info.Field(fieldName)
			if !ok || !found || field.Type != "reference" || field.Ref == "" {
				return fmt.Errorf("rule path base %s is not a declared reference", ref)
			}
			final, err := RecordPathField(field.Ref, path, lookup)
			if err != nil {
				return err
			}
			leaf := ExprType{Kind: RuleFieldKind(final.Type)}
			for i := len(path) - 1; i >= 0; i-- {
				leaf = ExprType{Kind: "reference", Fields: map[string]ExprType{path[i]: leaf}}
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
	if _, err := CompileExpr(expr, schema); err != nil {
		return schema, err
	}
	return schema, nil
}

func mergeRuleTypes(dst, src ExprType) ExprType {
	if dst.Kind == "" {
		return src
	}
	if dst.Fields == nil {
		dst.Fields = map[string]ExprType{}
	}
	for name, t := range src.Fields {
		dst.Fields[name] = mergeRuleTypes(dst.Fields[name], t)
	}
	return dst
}

// RuleContext captures caller-filtered, immutable read values inside the
// existing private view. The evaluator itself neither reads nor writes state.
func (c Caller) RuleContext(expr Expr, spec RuleReadSpec, lookup func(string) (EntityInfo, bool), record any, inputs map[string]any, now time.Time) (ExprContext, error) {
	ctx := ExprContext{Refs: map[string]ExprValue{"caller.id": ExprPresent("text", c.ID)}, Sets: map[string]ExprCollection{}, Me: c.ID, Now: now.UTC(), Today: c.Today(now)}
	for name, typ := range spec.Inputs {
		v, ok := inputs[name]
		if ok {
			ctx.Refs[ruleInputName(name)] = ExprPresent(typ.Kind, v)
			if spec.Object == "" {
				ctx.Refs[name] = ctx.Refs[ruleInputName(name)]
			}
		} else {
			ctx.Refs[ruleInputName(name)] = ExprValue{Kind: typ.Kind, Status: "missing"}
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
	// omitempty is only transport: zero/false on initialized scalar fields is
	// a real value. Optional pointers retain their missing/null distinction.
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
		value := ExprValue{Kind: RuleFieldKind(field.Type), Status: "missing"}
		if !field.ReadsAny(c.RolesIn(info.App)) {
			value.Status = "withheld"
		} else if raw, ok := values[field.Name]; ok {
			value.Status = "value"
			value.Data = raw
			if string(raw) == "null" {
				value.Status = "null"
			}
		}
		ctx.Refs[field.Name] = value
	}
	var visit func(Expr) error
	visit = func(e Expr) error {
		if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") && !strings.HasPrefix(e.Ref, "input.") && e.Ref != "caller.id" && !strings.HasPrefix(e.Ref, "step.") {
			var id string
			_ = json.Unmarshal(values["id"], &id)
			if id != "" {
				ctx.Sources = append(ctx.Sources, spec.Object+"/"+id, spec.Object+"/"+id+"#"+e.Ref)
			}
		}
		if e.Op == "path" && len(e.Args) == 1 && !strings.HasPrefix(e.Args[0].Ref, "item.") {
			name := e.Args[0].Ref
			field, ok := info.Field(name)
			if strings.HasPrefix(name, "input.") || strings.HasPrefix(name, "step.") {
				t := spec.Inputs[strings.TrimPrefix(name, "input.")]
				field = FieldInfo{Type: t.Kind, Ref: t.Object}
				ok = t.Kind == "reference" && t.Object != ""
			}
			if !ok {
				return fmt.Errorf("rule reference %s is undeclared", name)
			}
			value := ExprValue{Kind: e.Kind, Status: "missing"}
			base := ctx.Refs[name]
			if base.Status == "withheld" {
				value.Status = "withheld"
			} else {
				var id string
				if json.Unmarshal(base.Data, &id) == nil && id != "" {
					raw, sources, err := c.ReadRecordPath(field.Ref, id, e.Path, now)
					if err != nil {
						value.Status = "withheld"
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
	used := map[string][]Expr{}
	var collect func(Expr, string)
	collect = func(e Expr, scope string) {
		if e.Agg != nil {
			scope = e.Agg.Source
			if _, ok := used[scope]; !ok {
				used[scope] = nil
			}
			if len(e.Agg.Field) > 1 {
				used[scope] = append(used[scope], Expr{Op: "path", Kind: e.Kind, Args: []Expr{ExprReference("reference", "item."+e.Agg.Field[0])}, Path: e.Agg.Field[1:]})
			}
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
		var readErr *kernel.Error
		if set.Set != "" {
			var subjectID string
			_ = json.Unmarshal(values["id"], &subjectID)
			page, err := c.ReadRecordSet(spec.Object, subjectID, set.Set, now)
			readErr = err
			if err == nil {
				raw = Raw(map[string]any{"records": page.Records, "total": len(page.Records), "sources": page.Sources, "partial": page.Partial})
			}
		} else {
			raw, readErr = c.readRuleQuery(set, Raw(queryInputs), now)
		}
		if readErr != nil {
			ctx.Sets[name] = ExprCollection{Status: "withheld"}
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
		collection := ExprCollection{Complete: page.Total == len(page.Records), Withheld: page.Partial, Items: []ExprItem{}}
		object, ok := lookup(set.Object)
		if !ok {
			return ctx, fmt.Errorf("collection object is unavailable")
		}
		for _, row := range page.Records {
			var id string
			_ = json.Unmarshal(row["id"], &id)
			item := ExprItem{ID: id, Fields: map[string]ExprValue{}}
			for _, field := range object.Fields {
				v := ExprValue{Kind: RuleFieldKind(field.Type), Status: "missing"}
				if !field.ReadsAny(c.RolesIn(object.App)) {
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
				v := ExprValue{Kind: path.Kind, Status: "missing"}
				base := item.Fields[fieldName]
				if base.Status == "withheld" {
					v.Status = "withheld"
					collection.Withheld = true
				} else {
					var ref string
					if json.Unmarshal(base.Data, &ref) == nil && ref != "" {
						raw, sources, err := c.ReadRecordPath(field.Ref, ref, path.Path, now)
						if err != nil {
							v.Status = "withheld"
							collection.Withheld = true
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
		if budgetErr != nil || len(raw) > 256<<10 {
			return ctx, fmt.Errorf("captured rule context exceeds 256 KiB")
		}
	}
	return ctx, nil
}

// ExprGoValue converts one evaluated scalar for the existing record assignment
// owner. It is transport conversion, not another expression implementation.
func ExprGoValue(v ExprValue) (any, error) {
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
	if strings.HasPrefix(name, "step.") {
		return name
	}
	return "input." + name
}

func (c Caller) readRuleQuery(set RuleCollection, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	reader, ok := c.rt.(RuleReadRuntime)
	if !ok {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Scoped rule collections require the host")
	}
	return reader.ReadRuleQuery(c, set, inputs, now)
}
