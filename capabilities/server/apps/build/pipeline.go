package build

import (
	"encoding/json"
	"fmt"
	construction "platformserver/internal/host/buildapi"
	"regexp"
	"slices"

	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// StepKinds are the transformations a pipeline may declare, in the order a
// builder usually needs them.
var StepKinds = []string{"select", "rename", "cast", "filter", "compute", "lookup", "join", "dedupe", "aggregate", "sort"}

func (b *Build) pipelineEntity() platform.Entity {
	return platform.Entity{Type: construction.PipelineType, Title: "Pipeline", Plural: "Pipelines", Model: construction.Pipeline{}, Display: "title",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			p := record.(*construction.Pipeline)
			if !c.Replaying {
				if err := p.NormalizeFormulaRules(); err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
			}
			if p.State == "published" {
				fixed, err := b.ExecutionPipeline(*p)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				return b.checkPipeline(c, fixed)
			}
			return nil
		},
		Description: "Declared steps from a dataset to a dataset or an object, with expectations that quarantine bad rows.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{construction.Builder: platform.ScopeTenant, construction.Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the pipeline and let the host run it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishPipeline},
				{Name: "run", Title: "Run now", Description: "Ask for one run at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: requestRun},
				{Name: "pause", Title: "Pause", Description: "Stop running; the definition stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func pipelineActions() []platform.Action {
	return []platform.Action{{Schema: construction.SchemaPipelineRan, Target: construction.PipelineType, Capability: "integrations", Title: "Keep run result", Description: "Retain what one run of a pipeline did.", Roles: []string{construction.Builder, construction.Integrator},
		Payload: []platform.Field{{Name: "run", Type: "json", Required: true, Description: "The run's counts, quarantine and failures"}}}}
}

func requestRun(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*construction.Pipeline)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	p.Requested = true
	return nil
}

func (b *Build) publishPipeline(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*construction.Pipeline)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if err := p.NormalizeFormulaRules(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if err := b.checkPipeline(c, *p); err != nil {
		return err
	}
	p.Runner, p.Requested = c.ID, true
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetPipeline, *p)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*p = fixed.(construction.Pipeline)
	}
	return nil
}

// CheckPipeline shares publication validation with the read-only draft trial.
func (b *Build) CheckPipeline(c platform.Caller, p construction.Pipeline) *kernel.Error {
	return b.checkPipeline(c, p)
}

func (b *Build) checkPipeline(c platform.Caller, p construction.Pipeline) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(p.Name, p.ID); err != nil {
		return refuse(err.Error())
	}
	if _, ok := platform.Get[construction.Dataset](c, p.Input); !ok {
		return refuse("The input dataset does not exist")
	}
	marking, markErr := b.inputMarking(c, p)
	if markErr != nil {
		return refuse(markErr.Error())
	}
	for i, s := range p.Steps {
		if err := checkStep(c, s); err != nil {
			return refuse("Step {n} ({kind}): {why}", i+1, s.Kind, err.Error())
		}
	}
	for _, e := range p.Expectations {
		if e.Column == "" || !slices.Contains([]string{"notnull", "unique", "in", "matches", "range"}, e.Rule) {
			return refuse("An expectation names a column and one of notnull, unique, in, matches, range")
		}
		if e.Rule == "matches" {
			if _, err := regexp.Compile(e.Value); err != nil {
				return refuse("The expectation on {column} has an invalid pattern", e.Column)
			}
		}
	}
	outputs := 0
	for _, set := range []bool{p.OutputDataset != "", p.OutputObject != "", p.OutputEnterprise != nil} {
		if set {
			outputs++
		}
	}
	switch {
	case outputs != 1:
		return refuse("A pipeline writes either a dataset, an object or the enterprise model")
	case p.OutputEnterprise != nil:
		if construction.Guarded(marking) {
			return refuse("The enterprise model is tenant-readable; confidential or restricted rows cannot land there")
		}
		if err := p.OutputEnterprise.Check(); err != nil {
			return refuse(err.Error())
		}
	case p.OutputDataset != "":
		if p.OutputDataset == p.Input {
			return refuse("A pipeline does not write its own input")
		}
		if _, ok := platform.Get[construction.Dataset](c, p.OutputDataset); !ok {
			return refuse("The output dataset does not exist")
		}
	default:
		info, ok := b.lookupEntity(p.OutputObject)
		if !ok {
			return refuse("The output object {object} is not installed", p.OutputObject)
		}
		if p.Key == "" {
			return refuse("Name the column that is the record id")
		}
		if construction.Guarded(marking) {
			if open := construction.UnguardedFields(info, nil); len(open) > 0 {
				return refuse("The input is {marking}; name who reads {object}'s fields {fields} first, or lower the marking", marking, p.OutputObject, strings.Join(open, ", "))
			}
		}
	}
	if p.Every != "" {
		every, err := time.ParseDuration(p.Every)
		if err != nil || every < time.Minute || every > 366*24*time.Hour {
			return refuse("A pipeline runs every period between 1m and a year, such as 15m, 1h or 24h")
		}
	}
	return nil
}

func checkStep(c platform.Caller, s construction.Step) error {
	need := func(ok bool, what string) error {
		if !ok {
			return fmt.Errorf("needs %s", what)
		}
		return nil
	}
	switch s.Kind {
	case "select", "dedupe":
		return need(len(s.Columns) > 0, "columns")
	case "rename":
		return need(s.From != "" && s.To != "", "from and to")
	case "cast":
		return need(s.Column != "" && slices.Contains([]string{"string", "number", "boolean", "date"}, s.Type), "a column and a type")
	case "filter":
		return need(s.Column != "" && slices.Contains([]string{"=", "!=", "<", "<=", ">", ">=", "contains", "empty", "notempty"}, s.Op), "a column and an operator")
	case "compute":
		if s.To == "" {
			return fmt.Errorf("needs a target column")
		}
		if s.FormulaExpr == nil {
			_, err := construction.ValueParseFormula(s.Formula)
			return err
		}
		schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}}
		var refs func(platform.Expr)
		refs = func(e platform.Expr) {
			if e.Ref != "" {
				schema.Refs[e.Ref] = platform.ExprType{Kind: e.Kind}
			}
			for _, a := range e.Args {
				refs(a)
			}
			if e.In != nil {
				refs(*e.In)
			}
		}
		refs(*s.FormulaExpr)
		_, err := platform.CompileExpr(*s.FormulaExpr, schema)
		return err
	case "lookup", "join":
		if s.Dataset == "" || s.Column == "" || s.Match == "" {
			return fmt.Errorf("needs a dataset, a column here and a match column there")
		}
		if _, ok := platform.Get[construction.Dataset](c, s.Dataset); !ok {
			return fmt.Errorf("the dataset does not exist")
		}
		if s.Kind == "lookup" && len(s.Columns) == 0 {
			return fmt.Errorf("names the columns to take")
		}
		return nil
	case "aggregate":
		if len(s.Measures) == 0 {
			return fmt.Errorf("needs measures")
		}
		for _, m := range s.Measures {
			if m.To == "" || !slices.Contains([]string{"sum", "min", "max", "count", "avg"}, m.Fn) || (m.Fn != "count" && m.Column == "") {
				return fmt.Errorf("each measure is sum, min, max, avg of a column or count, into a named column")
			}
		}
		return nil
	case "sort":
		return need(s.Column != "", "a column")
	}
	return fmt.Errorf("is not one of %s", strings.Join(StepKinds, ", "))
}

func (b *Build) submitPipelineRan(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Run construction.PipelineRun `json:"run"`
		}
		p, ok := platform.Get[construction.Pipeline](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Run.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Run result has no pipeline")
		}
		if p.Last != nil && p.Last.Execution != nil && p.Last.Execution.Pending {
			if err := checkIntegrationExecution(p.Last.Execution, payload.Run.Execution, c.ID); err != nil {
				return nil, err
			}
			if payload.Run.DefinitionVersion != p.Last.DefinitionVersion || !payload.Run.At.Equal(p.Last.At) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution changed before its result was accepted")
			}
		} else {
			equivalent, versionErr := retainedExecutionEquivalent(p, payload.Run.DefinitionVersion)
			if versionErr != nil || !equivalent {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration definition changed before its result was accepted")
			}
			if err := checkIntegrationExecution(nil, payload.Run.Execution, c.ID); err != nil {
				return nil, err
			}
		}
		return func(r *pb.ChangeRecord) {
			run := payload.Run
			p.Last, p.Requested = &run, run.Execution != nil && run.Execution.Pending
			p.Marking = construction.HigherMarking(p.Marking, run.Marking)
			c.Put(r, p)
		}, nil
	})
}

// inputMarking is the highest marking of the datasets a pipeline reads.
func (b *Build) inputMarking(c platform.Caller, p construction.Pipeline) (string, error) {
	marking := ""
	for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
		if ds, ok := platform.Get[construction.Dataset](c, id); ok {
			fixed, err := b.ExecutionDataset(ds)
			if err != nil {
				return "", err
			}
			marking = construction.HigherMarking(marking, fixed.Marking)
		}
	}
	return marking, nil
}
