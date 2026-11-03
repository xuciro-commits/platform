package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type TransformStep struct {
	Kind        string             `json:"kind"`
	Markings    []string           `json:"markings,omitempty"`
	Columns     []string           `json:"columns,omitempty"`
	From        string             `json:"from,omitempty"`
	To          string             `json:"to,omitempty"`
	Column      string             `json:"column,omitempty"`
	Type        string             `json:"type,omitempty"`
	Op          string             `json:"op,omitempty"`
	Value       string             `json:"value,omitempty"`
	FormulaExpr *Expr              `json:"formulaExpr,omitempty" type:"json" field:"aside"`
	Formula     string             `json:"formula,omitempty"`
	Dataset     string             `json:"dataset,omitempty"`
	Match       string             `json:"match,omitempty"`
	As          string             `json:"as,omitempty"`
	Outer       bool               `json:"outer,omitempty"`
	Desc        bool               `json:"desc,omitempty"`
	Measures    []TransformMeasure `json:"measures,omitempty"`
}

type TransformMeasure struct {
	Fn     string `json:"fn"`
	Column string `json:"column,omitempty"`
	To     string `json:"to"`
}

type TransformExpectation struct {
	Column string `json:"column"`
	Rule   string `json:"rule"`
	Value  string `json:"value,omitempty"`
}

type QuarantinedRow struct {
	Reason string         `json:"reason"`
	Row    map[string]any `json:"row"`
}

func Literal(kind, s string) (any, error) {
	switch kind {
	case "integer":
		return strconv.Atoi(s)
	case "decimal":
		return strconv.ParseFloat(s, 64)
	case "boolean":
		return strconv.ParseBool(s)
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return s, err
	case "datetime":
		return time.Parse(time.RFC3339, s)
	case "money":
		return nil, fmt.Errorf("an amount is not set to a fixed value")
	}
	return s, nil
}

func AssignmentExpr(kind string, set TxnSet, spec RuleReadSpec) (Expr, error) {
	kind = RuleFieldKind(kind)
	targetKind := kind
	if set.Expr != nil {
		if set.From != "" {
			return Expr{}, fmt.Errorf("use one assignment expression")
		}
		if !RuleAssignmentFits(kind, set.Expr.Kind) {
			return Expr{}, fmt.Errorf("assignment result type differs from field")
		}
		return *set.Expr, nil
	}
	switch {
	case set.From == "$me":
		return ExprReference("text", "caller.id"), nil
	case set.From == "$subject":
		return ExprReference("reference", "input.$subject"), nil
	case set.From == "$now":
		op := "$now"
		if kind == "date" {
			op = "$today"
		}
		return Expr{Op: op, Kind: kind}, nil
	case strings.HasPrefix(set.From, "="):
		v, err := Literal(kind, strings.TrimPrefix(set.From, "="))
		return ExprLiteralValue(kind, v), err
	default:
		name := set.From
		if !strings.HasPrefix(name, "step.") && !strings.HasPrefix(name, "row.") {
			name = "input." + name
		}
		input := strings.TrimPrefix(name, "input.")
		if typ, ok := spec.Inputs[input]; ok {
			kind = typ.Kind
		}
		if !RuleAssignmentFits(targetKind, kind) {
			return Expr{}, fmt.Errorf("assignment source differs")
		}
		return ExprReference(kind, name), nil
	}
}

func RuleAssignment(e Expr, s RuleReadSpec, c Caller, record any, inputs map[string]any, now time.Time, lookup func(string) (EntityInfo, bool), provenance ...*[]string) (any, *kernel.Error) {
	r, err := EvaluateRule(e, s, c, record, inputs, now, lookup)
	for _, refs := range provenance {
		*refs = append(*refs, r.Sources...)
	}
	if err != nil {
		return nil, err
	}
	v, failure := ExprGoValue(r.Value)
	if failure != nil {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
	}
	return v, nil
}

func RuleAssignmentFits(dst, src string) bool {
	dst = RuleFieldKind(dst)
	src = RuleFieldKind(src)
	return dst == src || dst == "text" && src == "choice" || (dst == "decimal" || dst == "number") && (src == "integer" || src == "decimal" || src == "number")
}

func DispatchParameter(set TxnSet, scope RuleReadSpec) (Expr, error) {
	kind := "text"
	if set.Expr != nil {
		kind = set.Expr.Kind
	} else if input, found := scope.Inputs[strings.TrimPrefix(set.From, "input.")]; found {
		kind = input.Kind
	}
	if set.Expr == nil && strings.HasPrefix(set.From, "input.") {
		set.From = strings.TrimPrefix(set.From, "input.")
	}
	if slices.Contains([]string{"set", "object", "json", ""}, kind) {
		return Expr{}, fmt.Errorf("Dispatch parameters require scalar expressions")
	}
	return AssignmentExpr(kind, set, scope)
}

func BalanceID(object string, values []string) string {
	sum := sha256.Sum256([]byte(object + "\x00" + strings.Join(values, "\x00")))
	return "bal-" + hex.EncodeToString(sum[:12])
}

func CastCell(cell, typ string) (any, error) {
	text := strings.TrimSpace(cell)
	switch typ {
	case "number":
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("%q is not a number", text)
		}
		return n, nil
	case "boolean":
		switch strings.ToLower(text) {
		case "true", "yes":
			return true, nil
		case "false", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false", text)
	}
	return text, nil
}

func CastValue(value any, typ string) (any, error) {
	switch x := value.(type) {
	case string:
		return CastCell(x, typ)
	case float64:
		if typ == "number" {
			return x, nil
		}
		return CastCell(strconv.FormatFloat(x, 'f', -1, 64), typ)
	case bool:
		if typ == "boolean" {
			return x, nil
		}
		return CastCell(strconv.FormatBool(x), typ)
	case json.Number:
		return CastCell(x.String(), typ)
	}
	return nil, fmt.Errorf("no value")
}

func TableInput(reads RuleReadSpec, legacy bool, values map[string]any) ExprContext {
	facts := ExprContext{Refs: map[string]ExprValue{}}
	for name, col := range reads.Inputs {
		v, ok := values[name]
		if legacy {
			v, _ = CastValue(v, col.Kind)
		}
		if ok {
			facts.Refs["input."+name] = ExprPresent(col.Kind, v)
			facts.Refs[name] = facts.Refs["input."+name]
		}
	}
	return facts
}

func ConditionValue(kind, raw string) (any, error) {
	switch kind {
	case "integer":
		return strconv.ParseInt(raw, 10, 64)
	case "decimal":
		x, err := strconv.ParseFloat(raw, 64)
		if err == nil && (math.IsInf(x, 0) || math.IsNaN(x)) {
			err = fmt.Errorf("a finite number is required")
		}
		return x, err
	case "date":
		if day, err := time.Parse(time.DateOnly, raw); err == nil {
			return day, nil
		}
		instant, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.DateOnly, instant.UTC().Format(time.DateOnly))
	case "datetime":
		return time.Parse(time.RFC3339, raw)
	case "boolean":
		return strconv.ParseBool(raw)
	default:
		return raw, nil
	}
}

func TextOf(v any) string {
	if v != nil {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return ""
			}
			rv = rv.Elem()
		}
		v = rv.Interface()
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.UTC().Format(time.RFC3339)
	case Money:
		if x.Currency == "" && x.Amount == 0 {
			return ""
		}
		return strconv.FormatInt(x.Amount, 10)
	}
	return fmt.Sprint(v)
}

type BusinessRuntimeContext struct {
	App      string
	Entities map[string]Entity
	Ledger   *Ledger
	Lookup   func(string) (EntityInfo, bool)
	Prepare  func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) (TransactionProposal, *kernel.Error)
	Link     func(AssetBinding) (LinkType, error)
	Effect   func(Caller, string) (TransactionEffectDefinition, *kernel.Error)
	Dispatch func(Caller, *pb.ChangeRecord, *BusinessTxn, RuleReadSpec, map[string]any, map[string]any, time.Time) *kernel.Error
	Confirm  func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error
}

type TransactionProposal struct {
	Apply  func(Entity, EntityInfo, map[string]any, []string) (Entity, map[string]any)
	Finish func(Caller, *pb.ChangeRecord, time.Time) *kernel.Error
}

type TransactionEffectDefinition struct {
	Request func(string, json.RawMessage) (json.RawMessage, *kernel.Error)
}

func WalkTxnSteps(steps []TxnStep, visit func(TxnStep)) {
	for _, step := range steps {
		visit(step)
		if step.ForEach != nil {
			WalkTxnSteps(step.ForEach.Steps, visit)
		}
		for _, refusal := range step.Refusals {
			if refusal.Record != nil {
				WalkTxnSteps([]TxnStep{*refusal.Record}, visit)
			}
		}
	}
}

func WalkBusinessTxn(txn *BusinessTxn, visit func(TxnStep)) {
	WalkTxnSteps(txn.Steps, visit)
	for _, refusal := range txn.Refusals {
		if refusal.Record != nil {
			WalkTxnSteps([]TxnStep{*refusal.Record}, visit)
		}
	}
}

func WithTxnRow(spec RuleReadSpec, info EntityInfo) RuleReadSpec {
	spec.Inputs = maps.Clone(spec.Inputs)
	for _, field := range ConstraintEntity(info).Fields {
		spec.Inputs["row."+field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
	}
	spec.Inputs["row.$subject"] = ExprType{Kind: "reference", Object: info.Type}
	return spec
}

func TxnResult(spec *RuleReadSpec, step TxnStep) {
	if step.Call != nil {
		spec.Inputs["step."+step.Name] = step.Call.ResultType
	}
	if step.Kind == "sequence" {
		spec.Inputs["step."+step.Name] = ExprType{Kind: "text"}
	}
	if step.Kind == "create" || step.Kind == "post" {
		spec.Inputs["step."+step.Name] = ExprType{Kind: "reference", Object: step.Target.Type}
	}
}

func IntervalFields(f TxnIntervalFields) map[string]string {
	return map[string]string{f.Subject: "reference", f.Source: "text", f.SourceVersion: "text", f.SourceRevision: "text", f.ProcessingVersion: "text", f.Since: "datetime", f.Until: "datetime", f.OpenStart: "boolean", f.OpenEnd: "boolean", f.Retired: "boolean", f.Review: "boolean"}
}

func WithTxnRange(scope RuleReadSpec, kind string) RuleReadSpec {
	scope.Inputs = maps.Clone(scope.Inputs)
	scope.Inputs["row.start"] = ExprType{Kind: kind}
	scope.Inputs["row.end"] = ExprType{Kind: kind}
	scope.Inputs["row.index"] = ExprType{Kind: "integer"}
	return scope
}

func TxnIterationSource(owner, subject, of string, lookup func(string) (EntityInfo, bool)) (EntityInfo, error) {
	typ := subject
	if of != "" {
		field, err := RecordPathField(subject, strings.Split(of, "."), lookup)
		if err != nil || field.Type != "reference" || field.Ref == "" {
			return EntityInfo{}, fmt.Errorf("Iteration source must be a declared record reference")
		}
		typ = field.Ref
	}
	info, ok := lookup(typ)
	if !ok || info.App != owner {
		return EntityInfo{}, fmt.Errorf("Iteration source must belong to its transaction owner")
	}
	return info, nil
}

type TransactionExecutor interface {
	Transact(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error
}
