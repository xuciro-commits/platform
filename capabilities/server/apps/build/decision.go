package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	buildapi "platformserver/internal/host/buildapi"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// recordMap converts a struct or pointer record into a generic map, preserving its ID.
func recordMap(record any) map[string]any {
	out := map[string]any{}
	if record == nil {
		return out
	}
	if raw, err := json.Marshal(record); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	rv := reflect.ValueOf(record)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return out
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct && rv.NumField() > 0 {
		if rec, ok := rv.Field(0).Interface().(platform.Record); ok && rec.ID != "" {
			out["id"] = rec.ID
			out[" TYPE "] = rv.Type().Field(0).Tag.Get("entity")
		}
	}
	return out
}

// resolveRecordPathValue reads a 1..N hop path starting from the in-memory record v.
func resolveRecordPathValue(c platform.Caller, o Object, record any, path []string, lookup func(string) (platform.EntityInfo, bool), now time.Time) (any, *kernel.Error) {
	if len(path) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Empty record path")
	}
	rv := reflect.ValueOf(record)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	if path[0] == "id" {
		if rv.Kind() == reflect.Struct && rv.NumField() > 0 {
			if rec, ok := rv.Field(0).Interface().(platform.Record); ok {
				return rec.ID, nil
			}
		}
		m := recordMap(record)
		return m["id"], nil
	}
	var firstVal any
	if rv.Kind() == reflect.Struct {
		f := rv.FieldByName(goName(path[0]))
		if f.IsValid() {
			firstVal = f.Interface()
		} else {
			m := recordMap(record)
			firstVal = m[path[0]]
		}
	} else {
		m := recordMap(record)
		firstVal = m[path[0]]
	}
	if len(path) == 1 {
		return firstVal, nil
	}
	refID := buildapi.ValueTextOf(firstVal)
	if refID == "" {
		return "", nil
	}
	var refType string
	for _, f := range o.Fields {
		if f.Name == path[0] && f.Type == "reference" {
			refType = f.Ref
			break
		}
	}
	if refType == "" && lookup != nil {
		if info, ok := lookup(buildapi.TypeOf(o.Name)); ok {
			if f, found := info.Field(path[0]); found && f.Type == "reference" {
				refType = f.Ref
			}
		}
	}
	if refType == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition path %s is not a reference", path[0])
	}
	raw, _, refusal := c.ReadRecordPath(refType, refID, path[1:], now)
	if refusal != nil {
		if refusal.Message == "Record input has no value" {
			return "", nil
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A related condition source is unavailable / 关联条件来源不可读")
	}
	var result any
	if json.Unmarshal(raw, &result) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid condition source")
	}
	return result, nil
}

// resolveInputPathValue reads an input value or traverses a reference input along a path.
func resolveInputPathValue(c platform.Caller, a Action, inputs map[string]any, path []string, now time.Time) (any, *kernel.Error) {
	if len(path) == 0 {
		return nil, nil
	}
	given := inputs[path[0]]
	if len(path) == 1 {
		return given, nil
	}
	refID := buildapi.ValueTextOf(given)
	if refID == "" {
		return "", nil
	}
	var refType string
	for _, in := range a.Inputs {
		if in.Name == path[0] && in.Type == "reference" {
			refType = in.Ref
			break
		}
	}
	if refType == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Input %s is not a reference", path[0])
	}
	raw, _, refusal := c.ReadRecordPath(refType, refID, path[1:], now)
	if refusal != nil {
		if refusal.Message == "Record input has no value" {
			return "", nil
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A related condition source is unavailable / 关联条件来源不可读")
	}
	var result any
	if json.Unmarshal(raw, &result) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid condition source")
	}
	return result, nil
}

// parseStepExpr parses "step:first(collection,orderField)" or "step:next(collection,orderField,currentSource)".
func parseStepExpr(expr string) (mode, collection, orderField, currentSource string, ok bool) {
	for _, prefix := range []string{"step:first(", "step:next("} {
		if strings.HasPrefix(expr, prefix) && strings.HasSuffix(expr, ")") {
			mode = strings.TrimSuffix(strings.TrimPrefix(prefix, "step:"), "(")
			inner := strings.TrimSuffix(strings.TrimPrefix(expr, prefix), ")")
			parts := strings.Split(inner, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			if mode == "first" && len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return mode, parts[0], parts[1], "", true
			}
			if mode == "next" && len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
				return mode, parts[0], parts[1], parts[2], true
			}
		}
	}
	return "", "", "", "", false
}

// resolveCollectionMaps resolves a collection specification against the current record/inputs and builder state.
func (b *Build) resolveCollectionMaps(c platform.Caller, o Object, a Action, collection string, inputs map[string]any, record any, overlay []any, now time.Time) ([]map[string]any, error) {
	ownMap := recordMap(record)
	ownType := buildapi.TypeOf(o.Name)
	trimmed := strings.TrimPrefix(collection, "record.")

	// 1. Direct embedded lines field on the current record.
	for _, f := range o.Fields {
		if f.Name == trimmed && f.Type == "lines" {
			return extractLinesSlice(ownMap[f.Name]), nil
		}
	}

	// 2. Explicit object type "build.<name>".
	if strings.HasPrefix(trimmed, ID+".") {
		if b == nil {
			return nil, nil
		}
		entity, known := b.installed[trimmed]
		if !known {
			return nil, fmt.Errorf("collection object %s is not installed", trimmed)
		}
		return b.findWithOverlay(c, trimmed, entity, "", "", overlay)
	}

	// 3. Path ending in an inverse relation or embedded lines field (e.g. "sfcs", "order.sfcs", "routing.operations", "input.product.routing.operations").
	parts := strings.Split(collection, ".")
	parentType := ownType
	parentID := buildapi.ValueTextOf(ownMap["id"])
	parentRecordMap := ownMap
	relName := parts[len(parts)-1]

	if len(parts) > 1 {
		prefix := parts[:len(parts)-1]
		if prefix[0] == "record" {
			prefix = prefix[1:]
		}
		if len(prefix) > 0 {
			if prefix[0] == "input" {
				if len(prefix) < 2 {
					return nil, fmt.Errorf("invalid input collection path %s", collection)
				}
				val, refusal := resolveInputPathValue(c, a, inputs, prefix[1:], now)
				if refusal != nil {
					return nil, fmt.Errorf("%s", refusal.Message)
				}
				parentID = buildapi.ValueTextOf(val)
				var err error
				parentType, err = b.resolvePathTargetType(o, a, append([]string{"input"}, prefix[1:]...))
				if err != nil {
					return nil, err
				}
			} else {
				val, refusal := resolveRecordPathValue(c, o, record, prefix, b.conditionLookup, now)
				if refusal != nil {
					return nil, fmt.Errorf("%s", refusal.Message)
				}
				parentID = buildapi.ValueTextOf(val)
				var err error
				parentType, err = b.resolvePathTargetType(o, a, prefix)
				if err != nil {
					return nil, err
				}
			}
			parentRecordMap = nil
			if b != nil && b.host != nil && parentType != "" && parentID != "" {
				if rec, ok := b.host.Record(parentType + "/" + parentID); ok {
					parentRecordMap = recordMap(rec)
				}
			}
		}
	}

	// Check if parent has an embedded lines field named relName.
	if parentRecordMap != nil {
		if rawLines, exists := parentRecordMap[relName]; exists {
			if lines := extractLinesSlice(rawLines); lines != nil {
				return lines, nil
			}
		}
	}

	if b == nil || parentID == "" {
		return nil, nil
	}
	// Search installed objects for a reference field pointing to parentType with inverse == relName (or object name matching relName).
	for _, typ := range sortedTypes(b.installed) {
		entity := b.installed[typ]
		info, err := platform.Describe(ID, entity, func(reflect.Type) string { return "" })
		if err != nil {
			continue
		}
		for _, f := range info.Fields {
			if f.Type == "reference" && f.Ref == parentType && (f.Inverse == relName || strings.TrimPrefix(typ, ID+".")+"s" == relName || strings.TrimPrefix(typ, ID+".") == relName || f.Name == relName) {
				return b.findWithOverlay(c, typ, entity, f.Name, parentID, overlay)
			}
		}
	}
	return nil, nil
}

func (b *Build) resolvePathTargetType(o Object, a Action, path []string) (string, error) {
	if len(path) == 0 {
		return buildapi.TypeOf(o.Name), nil
	}
	curType := buildapi.TypeOf(o.Name)
	idx := 0
	if path[0] == "input" {
		if len(path) < 2 {
			return "", fmt.Errorf("missing input name")
		}
		var found Input
		for _, in := range a.Inputs {
			if in.Name == path[1] {
				found = in
				break
			}
		}
		if found.Type != "reference" || found.Ref == "" {
			return "", fmt.Errorf("input %s is not a reference", path[1])
		}
		curType = found.Ref
		idx = 2
	}
	for ; idx < len(path); idx++ {
		name := path[idx]
		if curType == buildapi.TypeOf(o.Name) {
			matched := false
			for _, f := range o.Fields {
				if f.Name == name && f.Type == "reference" {
					curType = f.Ref
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		if b == nil {
			return "", fmt.Errorf("cannot resolve %s without builder host", name)
		}
		info, ok := b.lookupEntity(curType)
		if !ok {
			return "", fmt.Errorf("unknown entity %s", curType)
		}
		f, ok := info.Field(name)
		if !ok || f.Type != "reference" || f.Ref == "" {
			return "", fmt.Errorf("field %s.%s is not a reference", curType, name)
		}
		curType = f.Ref
	}
	return curType, nil
}

func extractLinesSlice(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	if s, ok := raw.([]map[string]any); ok {
		return s
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []map[string]any
	if json.Unmarshal(bytes, &out) != nil {
		return nil
	}
	return out
}

func (b *Build) findWithOverlay(c platform.Caller, typ string, entity platform.Entity, viaField, parentID string, overlay []any) ([]map[string]any, error) {
	var records []any
	if c.Replaying || b.host != nil {
		found, _, err := c.FindOf(reflect.TypeOf(entity.Model), platform.Query{Limit: 1000})
		if err == nil {
			records = found
		}
	}
	byID := map[string]map[string]any{}
	var order []string
	for _, rec := range records {
		m := recordMap(rec)
		if archived, _ := m["archived"].(bool); archived {
			continue
		}
		id := buildapi.ValueTextOf(m["id"])
		if id != "" {
			byID[id] = m
			order = append(order, id)
		}
	}
	for _, ov := range overlay {
		if ov == nil {
			continue
		}
		m := recordMap(ov)
		ovType, _ := m[" TYPE "].(string)
		if ovType != "" && ovType != typ {
			continue
		}
		id := buildapi.ValueTextOf(m["id"])
		if id == "" {
			continue
		}
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = m
	}
	var out []map[string]any
	for _, id := range order {
		m := byID[id]
		if viaField != "" && buildapi.ValueTextOf(m[viaField]) != parentID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// evalStep evaluates step:first(collection,orderField) or step:next(collection,orderField,currentSource).
func (b *Build) evalStep(c platform.Caller, o Object, a Action, expr string, inputs map[string]any, record any, overlay []any, targetKind string, now time.Time) (any, *kernel.Error) {
	mode, collection, orderField, currentSource, ok := parseStepExpr(expr)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid step expression %s", expr)
	}
	items, err := b.resolveCollectionMaps(c, o, a, collection, inputs, record, overlay, now)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "%s", err.Error())
	}
	if len(items) == 0 {
		if targetKind == "integer" {
			return 0, nil
		}
		return "", nil
	}
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, func(x, y map[string]any) int {
		nx, _ := buildapi.ValueNumber(x[orderField])
		ny, _ := buildapi.ValueNumber(y[orderField])
		if c := cmp.Compare(nx, ny); c != 0 {
			return c
		}
		return strings.Compare(buildapi.ValueTextOf(x["id"]), buildapi.ValueTextOf(y["id"]))
	})
	var picked map[string]any
	if mode == "first" {
		picked = sorted[0]
	} else {
		curVal, _, refusal := b.evalSource(c, o, a, Field{Type: "text"}, currentSource, inputs, record, overlay, 1, 1, nil, now)
		if refusal != nil {
			return nil, refusal
		}
		curText := buildapi.ValueTextOf(curVal)
		idx := slices.IndexFunc(sorted, func(item map[string]any) bool {
			return buildapi.ValueTextOf(item["id"]) == curText || buildapi.ValueTextOf(item[orderField]) == curText
		})
		if idx >= 0 && idx+1 < len(sorted) {
			picked = sorted[idx+1]
		}
	}
	if picked == nil {
		if targetKind == "integer" {
			return 0, nil
		}
		return "", nil
	}
	if targetKind == "integer" {
		n, _ := buildapi.ValueNumber(picked[orderField])
		return int(math.Round(n)), nil
	}
	if id := buildapi.ValueTextOf(picked["id"]); id != "" {
		return id, nil
	}
	return picked[orderField], nil
}

// evalQuantifier evaluates a quantified collection condition (all, none, any, count).
func (b *Build) evalQuantifier(c platform.Caller, o Object, a Action, cond Condition, inputs map[string]any, record any, overlay []any, now time.Time) (bool, *kernel.Error) {
	items, err := b.resolveCollectionMaps(c, o, a, cond.Collection, inputs, record, overlay, now)
	if err != nil {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "%s", err.Error())
	}
	matched := 0
	for _, item := range items {
		holdsAll := true
		for _, w := range cond.Where {
			ok, refusal := b.evalItemCondition(c, o, a, w, item, inputs, record, overlay, now)
			if refusal != nil {
				return false, refusal
			}
			if !ok {
				holdsAll = false
				break
			}
		}
		if holdsAll {
			matched++
		}
	}
	switch cond.Quantifier {
	case "none":
		return matched == 0, nil
	case "any":
		return matched > 0, nil
	case "all":
		return len(items) > 0 && matched == len(items), nil
	case "count":
		op := cond.Operator
		if op == "" {
			op = "="
		}
		return buildapi.ValueHolds(matched, "integer", op, cond.Value), nil
	}
	return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unsupported quantifier %s", cond.Quantifier)
}

func (b *Build) evalItemCondition(c platform.Caller, o Object, a Action, w Condition, item map[string]any, inputs map[string]any, record any, overlay []any, now time.Time) (bool, *kernel.Error) {
	fieldName := strings.TrimPrefix(w.Field, "item.")
	got := item[fieldName]
	kind := "text"
	switch got.(type) {
	case int, int64, float64:
		kind = "decimal"
	case bool:
		kind = "boolean"
	}
	want := w.Value
	if want == "$me" {
		want = c.ID
	} else if w.ValueField != "" {
		switch {
		case strings.HasPrefix(w.ValueField, "input."):
			val, refusal := resolveInputPathValue(c, a, inputs, strings.Split(strings.TrimPrefix(w.ValueField, "input."), "."), now)
			if refusal != nil {
				return false, refusal
			}
			want = buildapi.ValueTextOf(val)
		case strings.HasPrefix(w.ValueField, "item."):
			want = buildapi.ValueTextOf(item[strings.TrimPrefix(w.ValueField, "item.")])
		default:
			path := strings.Split(strings.TrimPrefix(w.ValueField, "record."), ".")
			val, refusal := resolveRecordPathValue(c, o, record, path, b.conditionLookupOrNil(), now)
			if refusal != nil {
				return false, refusal
			}
			want = buildapi.ValueTextOf(val)
		}
	}
	return buildapi.ValueHolds(got, kind, w.Operator, want), nil
}

func (b *Build) conditionLookupOrNil() func(string) (platform.EntityInfo, bool) {
	if b == nil {
		return nil
	}
	return b.conditionLookup
}

// evalSource evaluates a unified value source for Sets, Creates and Updates.
func (b *Build) evalSource(c platform.Caller, o Object, a Action, targetField Field, from string, inputs map[string]any, record any, overlay []any, loopIdx, loopCount int, loopItem map[string]any, now time.Time) (any, bool, *kernel.Error) {
	switch {
	case from == "$me":
		return c.ID, true, nil
	case from == "$now":
		if targetField.Type == "date" {
			return c.Today(now), true, nil
		}
		return now.UTC(), true, nil
	case from == "$index":
		if targetField.Type == "text" {
			return strconv.Itoa(loopIdx), true, nil
		}
		return loopIdx, true, nil
	case from == "$count":
		if targetField.Type == "text" {
			return strconv.Itoa(loopCount), true, nil
		}
		return loopCount, true, nil
	case strings.HasPrefix(from, "$item."):
		if loopItem == nil {
			return nil, false, nil
		}
		val, ok := loopItem[strings.TrimPrefix(from, "$item.")]
		return val, ok, nil
	case from == "=":
		switch targetField.Type {
		case "tags":
			return []string{}, true, nil
		case "lines":
			return []any{}, true, nil
		case "integer":
			return 0, true, nil
		case "decimal":
			return 0.0, true, nil
		case "boolean":
			return false, true, nil
		default:
			return "", true, nil
		}
	case strings.HasPrefix(from, "="):
		rawLit := strings.TrimPrefix(from, "=")
		if targetField.Type == "tags" {
			return buildapi.ValueChoices(rawLit), true, nil
		}
		val, err := literal(targetField.Type, rawLit)
		if err != nil {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "%s", err.Error())
		}
		return val, true, nil
	case strings.HasPrefix(from, "append:"):
		spec := strings.TrimPrefix(from, "append:")
		own := recordMap(record)
		if targetField.Type == "tags" {
			existing := buildapi.ValueItems(own[targetField.Name])
			val, ok, refusal := b.evalSource(c, o, a, Field{Type: "text"}, spec, inputs, record, overlay, loopIdx, loopCount, loopItem, now)
			if refusal != nil {
				return nil, false, refusal
			}
			if ok && buildapi.ValueTextOf(val) != "" {
				existing = append(existing, buildapi.ValueTextOf(val))
			}
			return existing, true, nil
		}
		existingLines := extractLinesSlice(own[targetField.Name])
		row := map[string]any{}
		colTypes := map[string]string{}
		for _, col := range targetField.Columns {
			colTypes[col.Name] = col.Type
		}
		for _, pair := range strings.Split(spec, ",") {
			k, vExpr, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if !ok || k == "" {
				continue
			}
			colType := colTypes[k]
			if colType == "" {
				colType = "text"
			}
			val, present, refusal := b.evalSource(c, o, a, Field{Name: k, Type: colType}, strings.TrimSpace(vExpr), inputs, record, overlay, loopIdx, loopCount, loopItem, now)
			if refusal != nil {
				return nil, false, refusal
			}
			if present {
				row[k] = val
			}
		}
		existingLines = append(existingLines, row)
		return existingLines, true, nil
	case strings.HasPrefix(from, "split:"):
		parts := strings.Split(strings.TrimPrefix(from, "split:"), ":")
		if len(parts) != 2 {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid split expression %s", from)
		}
		totalVal, _, refusal := b.evalSource(c, o, a, Field{Type: "integer"}, parts[0], inputs, record, overlay, loopIdx, loopCount, loopItem, now)
		if refusal != nil {
			return nil, false, refusal
		}
		countVal, _, refusal := b.evalSource(c, o, a, Field{Type: "integer"}, parts[1], inputs, record, overlay, loopIdx, loopCount, loopItem, now)
		if refusal != nil {
			return nil, false, refusal
		}
		totalNum, err1 := buildapi.ValueNumber(totalVal)
		countNum, err2 := buildapi.ValueNumber(countVal)
		total, count := int(math.Round(totalNum)), int(math.Round(countNum))
		if err1 != nil || err2 != nil || count <= 0 {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Split requires a positive count")
		}
		share := total / count
		if loopIdx >= 1 && loopIdx <= total%count {
			share++
		}
		return share, true, nil
	case strings.HasPrefix(from, "formula:"):
		expr := strings.TrimPrefix(from, "formula:")
		node, err := buildapi.ValueParseFormula(expr)
		if err != nil {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid formula: %s", err.Error())
		}
		own := recordMap(record)
		read := func(name string) float64 {
			if raw, ok := inputs[strings.TrimPrefix(name, "input_")]; ok {
				if n, err := buildapi.ValueNumber(raw); err == nil {
					return n
				}
			}
			if raw, ok := inputs[name]; ok {
				if n, err := buildapi.ValueNumber(raw); err == nil {
					return n
				}
			}
			if raw, ok := own[strings.TrimPrefix(name, "record_")]; ok {
				if n, err := buildapi.ValueNumber(raw); err == nil {
					return n
				}
			}
			if raw, ok := own[name]; ok {
				if n, err := buildapi.ValueNumber(raw); err == nil {
					return n
				}
			}
			return 0
		}
		res := node.Eval(read)
		if math.IsNaN(res) || math.IsInf(res, 0) {
			res = 0
		}
		if targetField.Type == "integer" {
			return int(math.Round(res)), true, nil
		}
		return res, true, nil
	case strings.HasPrefix(from, "table:"):
		rest := strings.TrimPrefix(from, "table:")
		tbName, colName, ok := strings.Cut(rest, ".")
		if !ok || b == nil {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid decision table reference %s", from)
		}
		tb, exists := b.tables[tbName]
		if !exists {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Decision table %s is not published", tbName)
		}
		env := recordMap(record)
		for k, v := range inputs {
			env[k] = v
		}
		decided, err := tb.Decide(env)
		if err != nil {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "%s", err.Error())
		}
		val, exists := decided[colName]
		return val, exists, nil
	case strings.HasPrefix(from, "step:"):
		val, refusal := b.evalStep(c, o, a, from, inputs, record, overlay, targetField.Type, now)
		if refusal != nil {
			return nil, false, refusal
		}
		return val, true, nil
	case strings.HasPrefix(from, "rollup:"):
		val, refusal := b.evalRollup(c, o, a, targetField.Type, strings.TrimPrefix(from, "rollup:"), inputs, record, overlay, now)
		if refusal != nil {
			return nil, false, refusal
		}
		return val, true, nil
	case strings.HasPrefix(from, "record."):
		path := strings.Split(strings.TrimPrefix(from, "record."), ".")
		val, refusal := resolveRecordPathValue(c, o, record, path, b.conditionLookupOrNil(), now)
		if refusal != nil {
			return nil, false, refusal
		}
		return val, val != nil, nil
	case strings.HasPrefix(from, "input."):
		path := strings.Split(strings.TrimPrefix(from, "input."), ".")
		val, refusal := resolveInputPathValue(c, a, inputs, path, now)
		if refusal != nil {
			return nil, false, refusal
		}
		if len(path) == 1 {
			_, ok := inputs[path[0]]
			return val, ok && val != nil, nil
		}
		return val, val != nil, nil
	default:
		if strings.Contains(from, ".") {
			path := strings.Split(from, ".")
			for _, in := range a.Inputs {
				if in.Name == path[0] {
					val, refusal := resolveInputPathValue(c, a, inputs, path, now)
					if refusal != nil {
						return nil, false, refusal
					}
					return val, val != nil, nil
				}
			}
		}
		given, ok := inputs[from]
		if !ok || given == nil {
			return nil, false, nil
		}
		return given, true, nil
	}
}

// evalRollup evaluates "sum(collection.field[?filterField=v1,v2])" or "count(collection[?filterField=v1,v2])".
func (b *Build) evalRollup(c platform.Caller, o Object, a Action, targetKind, expr string, inputs map[string]any, record any, overlay []any, now time.Time) (any, *kernel.Error) {
	mode := ""
	inner := ""
	switch {
	case strings.HasPrefix(expr, "sum(") && strings.HasSuffix(expr, ")"):
		mode = "sum"
		inner = strings.TrimSuffix(strings.TrimPrefix(expr, "sum("), ")")
	case strings.HasPrefix(expr, "count(") && strings.HasSuffix(expr, ")"):
		mode = "count"
		inner = strings.TrimSuffix(strings.TrimPrefix(expr, "count("), ")")
	default:
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unsupported rollup %s", expr)
	}
	targetPart, filterPart, _ := strings.Cut(inner, "?")
	collection := strings.TrimSpace(targetPart)
	sumField := ""
	if mode == "sum" {
		lastDot := strings.LastIndex(collection, ".")
		if lastDot <= 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "sum rollup requires <collection>.<field>")
		}
		sumField = collection[lastDot+1:]
		collection = collection[:lastDot]
	}
	var filterField string
	var filterValues []string
	if filterPart != "" {
		fName, vals, ok := strings.Cut(filterPart, "=")
		if ok {
			filterField = strings.TrimSpace(fName)
			filterValues = buildapi.ValueChoices(vals)
		}
	}
	items, err := b.resolveCollectionMaps(c, o, a, collection, inputs, record, overlay, now)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "%s", err.Error())
	}
	total := 0.0
	count := 0
	for _, item := range items {
		if filterField != "" && !slices.Contains(filterValues, buildapi.ValueTextOf(item[filterField])) {
			continue
		}
		count++
		if mode == "sum" {
			if n, err := buildapi.ValueNumber(item[sumField]); err == nil {
				total += n
			}
		}
	}
	if mode == "count" {
		return count, nil
	}
	if targetKind == "integer" {
		return int(math.Round(total)), nil
	}
	return total, nil
}

func operandValue(c platform.Caller, b *Build, o Object, a Action, name string, inputs map[string]any, record any, overlay []any, lookup func(string) (platform.EntityInfo, bool), now time.Time) (any, *kernel.Error) {
	if strings.HasPrefix(name, "step:") {
		return b.evalStep(c, o, a, name, inputs, record, overlay, "text", now)
	}
	if in, ok := strings.CutPrefix(name, "input."); ok {
		parts := strings.Split(in, ".")
		return resolveInputPathValue(c, a, inputs, parts, now)
	}
	clean := strings.TrimPrefix(name, "record.")
	if strings.Contains(clean, ".") {
		return resolveRecordPathValue(c, o, record, strings.Split(clean, "."), lookup, now)
	}
	rv := reflect.ValueOf(record)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if clean == "id" {
		return rv.Field(0).Interface().(platform.Record).ID, nil
	}
	f := rv.FieldByName(goName(clean))
	if !f.IsValid() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition field is unavailable")
	}
	return f.Interface(), nil
}

func evaluateConditionOnRecord(c platform.Caller, b *Build, o Object, a Action, cond Condition, inputs map[string]any, record any, overlay []any, lookup func(string) (platform.EntityInfo, bool), now time.Time) (bool, *kernel.Error) {
	if cond.When != nil {
		held, err := evaluateConditionOnRecord(c, b, o, a, *cond.When, inputs, record, overlay, lookup, now)
		if err != nil || !held {
			return err == nil, err
		}
	}
	if cond.Quantifier != "" {
		return b.evalQuantifier(c, o, a, cond, inputs, record, overlay, now)
	}
	if strings.HasPrefix(cond.Field, "step:") {
		got, refusal := b.evalStep(c, o, a, cond.Field, inputs, record, overlay, "text", now)
		if refusal != nil {
			return false, refusal
		}
		return buildapi.ValueHolds(got, "text", cond.Operator, cond.Value), nil
	}
	field, err := conditionField(o, a, cond.Field, lookup)
	if err != nil {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition field is unavailable")
	}
	got, refusal := operandValue(c, b, o, a, cond.Field, inputs, record, overlay, lookup, now)
	if refusal != nil {
		return false, refusal
	}
	kind, want := field.Type, cond.Value
	if cond.ValueField != "" {
		right, err := conditionField(o, a, cond.ValueField, lookup)
		if err != nil || checkConditionFields(field, right, cond.Operator) != nil {
			return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition comparison field is unavailable or incompatible")
		}
		other, refusal := operandValue(c, b, o, a, cond.ValueField, inputs, record, overlay, lookup, now)
		if refusal != nil {
			return false, refusal
		}
		want = buildapi.ValueTextOf(other)
		if want == "" && cond.Operator != "contains all" && cond.Operator != "not in" {
			return false, nil
		}
		if right.Type == "decimal" {
			kind = "decimal"
		}
	} else if want == "$me" {
		want = c.ID
	}
	return buildapi.ValueHolds(got, kind, cond.Operator, want), nil
}
