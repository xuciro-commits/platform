package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"strings"
	"time"
)

// MigrationProgram contains only programs compiled by ADR-0101. The host
// supplies captured source values and commits the resulting images separately.
type MigrationProgram struct {
	Plan        MigrationPlan
	predicates  map[string]*ExprProgram
	conversions map[string]*ExprProgram
	sources     map[string]EntityInfo
	targets     map[string]EntityInfo
}

type MigrationRecordResult struct {
	Values    map[string]json.RawMessage
	Aliases   map[string]string
	Retired   []string
	Extracted map[string]json.RawMessage
	Changed   bool
}

// CompileMigrationPlan validates actual source and target shapes, not names
// supplied by a client. Every conversion resolves against the source release.
func CompileMigrationPlan(plan MigrationPlan, sources, targets map[string]EntityInfo) (*MigrationProgram, error) {
	if err := plan.Check(); err != nil {
		return nil, err
	}
	program := &MigrationProgram{Plan: plan, sources: sources, targets: targets, predicates: map[string]*ExprProgram{}, conversions: map[string]*ExprProgram{}}
	for index, step := range plan.Steps {
		authority := ""
		for _, typ := range step.Types {
			source, sourceOK := sources[typ]
			target, targetOK := targets[typ]
			if !sourceOK || !targetOK {
				return nil, fmt.Errorf("migration step %d: type %s is absent from a sealed release", index+1, typ)
			}
			if source.App != target.App {
				return nil, fmt.Errorf("migration cannot change a record authority")
			}
			if source.App == "" || authority != "" && authority != source.App {
				return nil, fmt.Errorf("a migration batch cannot cross authorities")
			}
			authority = source.App
			for _, field := range source.Fields {
				if _, retained := target.Field(field.Name); !retained {
					return nil, fmt.Errorf("source field %s cannot be physically deleted", field.Name)
				}
			}
			schema := ExprSchema{Refs: map[string]ExprType{}}
			for _, f := range source.Fields {
				schema.Refs[f.Name] = ExprType{Kind: RuleFieldKind(f.Type), Object: f.Ref}
			}
			if plan.Scope.Where != nil {
				if plan.Scope.Where.Kind != "boolean" {
					return nil, fmt.Errorf("migration predicate must be boolean")
				}
				predicate, err := CompileExpr(*plan.Scope.Where, schema)
				if err != nil {
					return nil, err
				}
				program.predicates[typ] = predicate
			}
			if step.Kind == "retire-action" {
				continue
			}
			if step.Kind == "extract" {
				var found bool
				target, found = targets[step.Target]
				if !found || source.App != target.App {
					return nil, fmt.Errorf("extraction needs a target of the same authority")
				}
			}
			for fi, change := range step.Fields {
				from, fromOK := source.Field(change.From)
				to, toOK := target.Field(change.To)
				if change.From != "" && !fromOK || !toOK {
					return nil, fmt.Errorf("migration field %s/%s is absent from its sealed shape", change.From, change.To)
				}
				if slices.Contains([]string{"rename", "split", "merge", "retire"}, step.Kind) {
					retained, exists := targets[typ].Field(change.From)
					if !exists || retained.Type != from.Type || retained.Ref != from.Ref || !retained.ReadOnly {
						return nil, fmt.Errorf("source field %s must remain as a compatible read-only field", change.From)
					}
				}
				if step.Kind == "rename" && (from.Type != to.Type || from.Ref != to.Ref) {
					return nil, fmt.Errorf("rename cannot silently convert values or references")
				}
				if step.Kind == "rebind" && (from.Type != "reference" || to.Type != "reference" || from.Ref == to.Ref || from.Ref == "" || to.Ref == "") {
					return nil, fmt.Errorf("rebind needs two explicit reference target types")
				}
				if step.Kind == "retype" && step.OnFailure != "quarantine" && slices.ContainsFunc(source.Fields, func(f FieldInfo) bool { return f.Ref == typ }) {
					return nil, fmt.Errorf("referenced type conversion needs an explicit quarantine strategy")
				}
				if step.Kind == "add" && fromOK {
					return nil, fmt.Errorf("add cannot overwrite an existing source field")
				}
				expression := change.Expr
				if expression == nil {
					expression = step.Expr
				}
				if expression == nil {
					continue
				}
				if expression.Kind != RuleFieldKind(to.Type) {
					return nil, fmt.Errorf("conversion result does not match target field %s", to.Name)
				}
				compiled, err := CompileExpr(*expression, schema)
				if err != nil {
					return nil, err
				}
				program.conversions[fmt.Sprintf("%d/%s/%d", index, typ, fi)] = compiled
			}
		}
	}
	return program, nil
}

// Transform is pure and returns a fresh image. Missing/invalid conversion and
// unmapped reference identities return an error with no partial result.
func (p *MigrationProgram) Transform(index int, typ string, raw json.RawMessage, captured ExprContext) (MigrationRecordResult, error) {
	if index < 0 || index >= len(p.Plan.Steps) || !slices.Contains(p.Plan.Steps[index].Types, typ) {
		return MigrationRecordResult{}, fmt.Errorf("record is outside this migration step")
	}
	if _, err := DecodeValue(raw, ExprMaxContextBytes); err != nil {
		return MigrationRecordResult{}, err
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return MigrationRecordResult{}, err
	}
	values := make(map[string]json.RawMessage, len(source))
	for name, value := range source {
		values[name] = slices.Clone(value)
	}
	captured.Refs = make(map[string]ExprValue, len(p.sources[typ].Fields))
	for _, field := range p.sources[typ].Fields {
		kind := RuleFieldKind(field.Type)
		value := ExprValue{Kind: kind, Status: "missing"}
		if raw, exists := source[field.Name]; exists {
			value.Status, value.Data = "value", slices.Clone(raw)
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				value.Status = "null"
			}
		}
		captured.Refs[field.Name] = value
	}
	out := MigrationRecordResult{Values: values, Aliases: map[string]string{}, Extracted: map[string]json.RawMessage{}}
	if predicate := p.predicates[typ]; predicate != nil {
		result, err := predicate.Eval(captured)
		if err != nil {
			return MigrationRecordResult{}, err
		}
		if result.Value.Status != "value" {
			return MigrationRecordResult{}, fmt.Errorf("migration predicate is missing or withheld")
		}
		var selected bool
		if json.Unmarshal(result.Value.Data, &selected) != nil {
			return MigrationRecordResult{}, fmt.Errorf("migration predicate is not boolean")
		}
		if !selected {
			return out, nil
		}
	}
	step := p.Plan.Steps[index]
	for fi, change := range step.Fields {
		if step.Kind == "retire" {
			out.Retired = append(out.Retired, change.From)
			continue
		}
		if step.Kind == "default" {
			if value, exists := values[change.To]; exists && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				continue
			}
		}
		var value json.RawMessage
		switch step.Kind {
		case "rename":
			value = slices.Clone(source[change.From])
			out.Aliases[change.From] = change.To
		case "rebind":
			var id string
			if json.Unmarshal(source[change.From], &id) != nil {
				return MigrationRecordResult{}, fmt.Errorf("reference %s is missing or invalid", change.From)
			}
			mapped, found := step.Mapping[id]
			if !found {
				return MigrationRecordResult{}, fmt.Errorf("reference %s has no declared identity mapping", id)
			}
			value = Raw(mapped)
		default:
			conversion := p.conversions[fmt.Sprintf("%d/%s/%d", index, typ, fi)]
			if conversion == nil {
				if step.Kind == "add" {
					continue
				}
				return MigrationRecordResult{}, fmt.Errorf("conversion program is missing")
			}
			result, err := conversion.Eval(captured)
			if err != nil {
				return MigrationRecordResult{}, err
			}
			if result.Value.Status != "value" {
				return MigrationRecordResult{}, fmt.Errorf("conversion for %s is missing, null or withheld", change.To)
			}
			value = slices.Clone(result.Value.Data)
		}
		if len(value) == 0 {
			return MigrationRecordResult{}, fmt.Errorf("source field %s has no value", change.From)
		}
		if step.Kind == "extract" {
			out.Extracted[change.To] = value
		} else {
			if prior, exists := values[change.To]; exists && step.Kind == "rename" && !bytes.Equal(prior, value) {
				return MigrationRecordResult{}, fmt.Errorf("rename would overwrite a different target value")
			}
			out.Changed = out.Changed || !bytes.Equal(values[change.To], value)
			values[change.To] = value
		}
		if step.Kind == "split" || step.Kind == "merge" {
			out.Retired = append(out.Retired, change.From)
		}
	}
	return out, nil
}

// MigrationPlan binds explicit transformations to two immutable releases.
// It is a declaration, never an executable tenant script (ADR-0106).
type MigrationPlan struct {
	From  string         `json:"from"`
	To    string         `json:"to"`
	Scope MigrationScope `json:"scope"`
	Steps []MigrateStep  `json:"steps"`
}

const MigrationRunType = "build.migration"

// MigrationRun is the accepted progress record, in the same authority as the
// customer objects a builder owns. A batch advances its cursor with its rows.
type MigrationRun struct {
	Record
	Plan         MigrationPlan      `json:"plan" type:"json" field:"readonly"`
	Review       string             `json:"review" field:"readonly" source:"record"`
	State        string             `json:"state" choices:"pending,running,blocked,complete,cancelling,cancelled" field:"readonly"`
	Step         int                `json:"step" field:"readonly"`
	Cursor       string             `json:"cursor,omitempty" field:"readonly" source:"record"`
	Processed    int                `json:"processed" field:"readonly"`
	Migrated     int                `json:"migrated" field:"readonly"`
	Failures     []MigrationFailure `json:"failures,omitempty" field:"readonly"`
	StartedBy    string             `json:"startedBy" field:"readonly" source:"record"`
	EvaluationAt time.Time          `json:"evaluationAt" field:"readonly"`
	SourceShapes map[string]string  `json:"sourceShapes" type:"json" field:"readonly"`
	TargetShapes map[string]string  `json:"targetShapes" type:"json" field:"readonly"`
	Targets      []string           `json:"targets" field:"readonly"`
}

type MigrationFailure struct {
	Type   string `json:"type" source:"record"`
	Record string `json:"record" source:"record"`
	Step   int    `json:"step"`
	Reason string `json:"reason"`
}

type MigrationRuntime interface {
	BeginMigration(Caller, *pb.ChangeRecord, MigrationPlan, string, time.Time, time.Time) (MigrationRun, *kernel.Error)
	AdvanceMigration(Caller, *pb.ChangeRecord, MigrationRun, time.Time) (MigrationRun, *kernel.Error)
}

func (c Caller) BeginMigration(r *pb.ChangeRecord, p MigrationPlan, review string, reviewedAt, at time.Time) (MigrationRun, *kernel.Error) {
	runtime, ok := c.rt.(MigrationRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return MigrationRun{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration requires a private accepted decision")
	}
	return runtime.BeginMigration(c, r, p, review, reviewedAt, at)
}

func (c Caller) AdvanceMigration(r *pb.ChangeRecord, run MigrationRun, at time.Time) (MigrationRun, *kernel.Error) {
	runtime, ok := c.rt.(MigrationRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return MigrationRun{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration requires a private accepted decision")
	}
	return runtime.AdvanceMigration(c, r, run, at)
}

type MigrationScope struct {
	Type  string `json:"type"`
	Where *Expr  `json:"where,omitempty"`
}

type MigrateStep struct {
	Kind      string            `json:"kind" enum:"add,default,rename,split,merge,retype,extract,retire,retire-action,rebind"`
	Types     []string          `json:"types"`
	Fields    []MigrateField    `json:"fields,omitempty"`
	Expr      *Expr             `json:"expr,omitempty"`
	Batch     int               `json:"batch"`
	OnFailure string            `json:"onFailure,omitempty" enum:"reject,quarantine"`
	Mapping   map[string]string `json:"mapping,omitempty"`
	Action    string            `json:"action,omitempty"`
	Redirect  string            `json:"redirect,omitempty"`
	Target    string            `json:"target,omitempty"`
}

type MigrateField struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
	Expr *Expr  `json:"expr,omitempty"`
}

func (p *MigrationPlan) UnmarshalJSON(raw []byte) error {
	type plain MigrationPlan
	return strictTxn(raw, (*plain)(p))
}

func (s *MigrateStep) UnmarshalJSON(raw []byte) error {
	type plain MigrateStep
	return strictTxn(raw, (*plain)(s))
}

func (f *MigrateField) UnmarshalJSON(raw []byte) error {
	type plain MigrateField
	return strictTxn(raw, (*plain)(f))
}

func (s *MigrationScope) UnmarshalJSON(raw []byte) error {
	type plain MigrationScope
	return strictTxn(raw, (*plain)(s))
}

// Check rejects ambiguous instructions before an owner plans any writes.
// Definition/type checks are performed against the two sealed releases.
func (p MigrationPlan) Check() error {
	if !strings.HasPrefix(p.From, "sha256-v1:") || !strings.HasPrefix(p.To, "sha256-v1:") || p.From == p.To {
		return fmt.Errorf("migration needs two different immutable release IDs")
	}
	if p.Scope.Type == "" || len(p.Steps) == 0 || len(p.Steps) > TxnMaxSteps {
		return fmt.Errorf("migration needs a record scope and bounded steps")
	}
	for i, step := range p.Steps {
		fail := func(reason string) error { return fmt.Errorf("migration step %d: %s", i+1, reason) }
		if !slices.Contains([]string{"add", "default", "rename", "split", "merge", "retype", "extract", "retire", "retire-action", "rebind"}, step.Kind) {
			return fail("unsupported step kind")
		}
		if len(step.Types) == 0 || len(step.Types) > TxnMaxSteps || step.Batch < 1 || step.Batch > TxnMaxSteps {
			return fail("types and batch must be bounded and nonempty")
		}
		if step.OnFailure != "" && step.OnFailure != "reject" && step.OnFailure != "quarantine" {
			return fail("unsupported failure handling")
		}
		seenTypes := map[string]bool{}
		for _, typ := range step.Types {
			if typ == "" || seenTypes[typ] {
				return fail("record types must be named once")
			}
			seenTypes[typ] = true
		}
		if step.Kind == "retire-action" {
			if step.Action == "" || len(step.Fields) > 0 || step.Expr != nil || len(step.Mapping) > 0 || step.Target != "" {
				return fail("action retirement names an action and an optional redirect only")
			}
			continue
		}
		if step.Action != "" || step.Redirect != "" || len(step.Fields) == 0 || len(step.Fields) > TxnMaxSteps {
			return fail("field steps need field changes and cannot retire actions")
		}
		if step.Kind == "split" && len(step.Fields) < 2 || step.Kind == "merge" && len(step.Fields) < 2 {
			return fail("split/merge needs at least two field changes")
		}
		if step.Kind == "rebind" && len(step.Mapping) == 0 {
			return fail("reference rebinding needs an explicit identity mapping")
		}
		if step.Kind != "rebind" && len(step.Mapping) > 0 {
			return fail("identity mapping is only valid for reference rebinding")
		}
		for from, to := range step.Mapping {
			if from == "" || to == "" {
				return fail("identity mapping cannot drop a referenced record")
			}
		}
		if (step.Kind == "extract") != (step.Target != "") {
			return fail("only extraction names a new target object")
		}
		seenFields := map[string]bool{}
		for _, field := range step.Fields {
			if field.To == "" || strings.ContainsAny(field.To, ".#/") {
				return fail("a change names one direct target field")
			}
			key := field.To
			if step.Kind == "merge" {
				key = field.From
			}
			if key == "" || seenFields[key] {
				return fail("field changes are missing or repeated")
			}
			seenFields[key] = true
			if slices.Contains([]string{"rename", "split", "merge", "retype", "retire", "rebind"}, step.Kind) && field.From == "" {
				return fail("this change needs a source field")
			}
			if step.Kind == "rename" && (field.From == field.To || field.Expr != nil || step.Expr != nil) {
				return fail("rename maps one old name to one new name without conversion")
			}
			if step.Kind == "retire" && (field.From != field.To || field.Expr != nil || step.Expr != nil) {
				return fail("retirement retains the original field and its values")
			}
			if slices.Contains([]string{"default", "split", "merge", "retype", "extract"}, step.Kind) && field.Expr == nil && step.Expr == nil {
				return fail("conversion requires an explicit ADR-0101 expression")
			}
		}
		if step.Kind == "retype" && step.OnFailure == "" {
			return fail("type conversion must declare reject or quarantine")
		}
	}
	return nil
}
