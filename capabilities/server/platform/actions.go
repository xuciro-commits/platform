package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"strings"
	"time"
)

func ActionInputSchema(action Action, entity *EntityInfo, roles []string) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, field := range action.Payload {
		var columns []FieldInfo
		if entity != nil && entity.Type == action.Target && field.Type == "lines" {
			if original, ok := entity.Field(field.Name); ok && original.Type == "lines" {
				columns = original.Fields
			}
		}
		properties[field.Name] = inputFieldSchema(field, columns, roles)
		if field.Required {
			required = append(required, field.Name)
		}
	}
	if entity != nil && action.Target == entity.Type && slices.Contains(entity.Standard, action.Schema) &&
		(action.Schema == entity.Type+".create" || action.Schema == entity.Type+".edit") {
		properties, required = map[string]any{}, []string{}
		create := action.Schema == entity.Type+".create"
		for _, field := range entity.Fields {
			if field.ReadOnly || roles != nil && (!field.ReadsAny(roles) || !field.WritesAny(roles)) {
				continue
			}
			input := Field{Name: field.Name, Type: field.Type, Description: field.Help, Required: create && field.Required && field.When == nil,
				Choices: field.Choices, Ref: field.Ref, Stereotype: field.Stereotype, Stereotypes: field.Stereotypes, Constraints: field.Constraints, ValueType: field.ValueType}
			if i := slices.IndexFunc(action.Payload, func(p Field) bool { return p.Name == field.Name }); i >= 0 {
				original := action.Payload[i]
				input.Required, input.Description, input.Default = original.Required, original.Description, original.Default
				input.From, input.Key, input.Label = original.From, original.Key, original.Label
			}
			properties[field.Name] = inputFieldSchema(input, field.Fields, roles)
			if field.When != nil {
				properties[field.Name].(map[string]any)["x-platform-when"] = field.When
			}
			if input.Required {
				required = append(required, field.Name)
			}
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func inputFieldSchema(field Field, columns []FieldInfo, roles []string) map[string]any {
	schema := map[string]any{}
	switch field.Type {
	case "integer":
		schema["type"] = "integer"
	case "number", "decimal":
		schema["type"] = "number"
	case "boolean":
		schema["type"] = "boolean"
	case "string[]", "tags", "references":
		schema["type"], schema["items"] = "array", map[string]any{"type": "string"}
	case "money":
		schema = map[string]any{"type": "object", "properties": map[string]any{"amount": map[string]any{"type": "integer"}, "currency": map[string]any{"type": "string"}}, "required": []string{"amount", "currency"}, "additionalProperties": false}
	case "lines":
		row, required := map[string]any{}, []string{}
		for _, column := range columns {
			if column.ReadOnly || roles != nil && (!column.ReadsAny(roles) || !column.WritesAny(roles)) {
				continue
			}
			row[column.Name] = inputFieldSchema(Field{Name: column.Name, Type: column.Type, Description: column.Help,
				Choices: column.Choices, Ref: column.Ref, Stereotype: column.Stereotype, Stereotypes: column.Stereotypes, Constraints: column.Constraints}, column.Fields, roles)
			if column.When != nil {
				row[column.Name].(map[string]any)["x-platform-when"] = column.When
			}
			if column.Required && column.When == nil {
				required = append(required, column.Name)
			}
		}
		items := map[string]any{}
		if len(columns) > 0 {
			items = map[string]any{"type": "object", "properties": row, "required": required, "additionalProperties": false}
		}
		schema["type"], schema["items"] = "array", items
	case "json":
	default:
		schema["type"] = "string"
	}
	if field.Description != "" {
		schema["description"] = field.Description
	}
	if json.Valid(field.Default) {
		schema["default"] = slices.Clone(field.Default)
	}
	if len(field.Choices) > 0 {
		if schema["type"] == "array" {
			schema["items"].(map[string]any)["enum"] = slices.Clone(field.Choices)
		} else {
			schema["enum"] = slices.Clone(field.Choices)
		}
	}
	if field.Type == "date" && (field.Constraints == nil || !field.Constraints.DateTime) {
		schema["format"] = "date"
	} else if field.Type == "datetime" {
		schema["format"] = "date-time"
	}
	if field.Ref != "" {
		schema["x-platform-reference"] = field.Ref
	}
	if field.Stereotype != "" {
		schema["x-platform-stereotype"] = field.Stereotype
	}
	if len(field.Stereotypes) > 0 {
		schema["x-platform-stereotypes"] = slices.Clone(field.Stereotypes)
	}
	if field.From != "" {
		schema["x-platform-source"] = map[string]string{"read": field.From, "key": field.Key, "label": field.Label}
	}
	if c := field.Constraints; c != nil {
		for _, bound := range []struct {
			name  string
			value *float64
		}{{map[bool]string{false: "minimum", true: "exclusiveMinimum"}[c.ExclusiveMin], c.Min}, {map[bool]string{false: "maximum", true: "exclusiveMaximum"}[c.ExclusiveMax], c.Max}} {
			if bound.value != nil {
				schema[bound.name] = *bound.value
			}
		}
		if c.MinLength != nil {
			schema["minLength"] = *c.MinLength
		}
		if c.MaxLength != nil {
			schema["maxLength"] = *c.MaxLength
		}
		if c.Pattern != "" {
			schema["pattern"] = c.Pattern
		}
		if c.Before != "" || c.After != "" || c.DateTime || c.Earliest != "" || c.Latest != "" {
			schema["x-platform-constraints"] = c
		}
	}
	if field.ValueType != nil {
		schema["x-platform-value-type"] = field.ValueType
	}
	return schema
}

type Action struct {
	recordEdit       bool
	Adoption         *ProposalAdoption `json:"adoption,omitempty"`
	SchemaVersion    *SchemaVersion    `json:"schemaVersion,omitempty"`
	Schema           string            `json:"schema"`
	Target           string            `json:"target"`
	Capability       string            `json:"capability"`
	Title            string            `json:"title"`
	Description      string            `json:"description"`
	Payload          []Field           `json:"payload"`
	Roles            []string          `json:"-"`
	Uses             []string          `json:"uses,omitempty"`
	Approval         *Approval         `json:"-"`
	NeedsApproval    bool              `json:"needsApproval,omitempty"`
	New              bool              `json:"new,omitempty"`
	IDPrefix         string            `json:"idPrefix,omitempty"`
	Automation       bool              `json:"automation,omitempty"`
	ConfirmationOnly bool              `json:"confirmationOnly,omitempty"`
	DeferRequired    bool              `json:"-"`
}

type Approval struct {
	Levels            []ApprovalLevel
	Pending, Rejected string
}

const (
	ApprovalHeld     = ".held"
	ApprovalRejected = ".rejected"
	ApprovalReturned = ".returned"
)

type ApprovalLevel struct {
	Title           string
	Structure, Role string
	AppRole         string
	Member          string
	All             bool
	When            func(c Caller, s *pb.Submission) bool
	Due             time.Duration
	WorkingDays     int
}

type Field struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Required    bool              `json:"required,omitempty"`
	Description string            `json:"description"`
	Choices     []string          `json:"choices,omitempty"`
	Ref         string            `json:"ref,omitempty"`
	Fields      []Field           `json:"fields,omitempty"`
	Stereotype  string            `json:"stereotype,omitempty"`
	Stereotypes []string          `json:"stereotypes,omitempty"`
	Default     json.RawMessage   `json:"default,omitempty"`
	From        string            `json:"from,omitempty"`
	Key         string            `json:"key,omitempty"`
	Label       string            `json:"label,omitempty"`
	Constraints *InputConstraints `json:"constraints,omitempty"`
	ValueType   *AssetBinding     `json:"valueType,omitempty"`
	Range       *DateRange        `json:"range,omitempty"`
	Group       string            `json:"group,omitempty"`
	Manual      bool              `json:"manual,omitempty"`
}

type InputConstraints struct {
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	ExclusiveMin bool     `json:"exclusiveMin,omitempty"`
	ExclusiveMax bool     `json:"exclusiveMax,omitempty"`
	MinLength    *int     `json:"minLength,omitempty"`
	MaxLength    *int     `json:"maxLength,omitempty"`
	Before       string   `json:"before,omitempty"`
	After        string   `json:"after,omitempty"`
	Earliest     string   `json:"earliest,omitempty"`
	Latest       string   `json:"latest,omitempty"`
	Inclusive    bool     `json:"inclusive,omitempty"`
	DateTime     bool     `json:"dateTime,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
}

type DateRange struct {
	End       string `json:"end"`
	Inclusive bool   `json:"inclusive,omitempty"`
}

type FieldIssue struct {
	BusinessCode string                     `json:"businessCode,omitempty"`
	Record       string                     `json:"record,omitempty"`
	Constraint   string                     `json:"constraint,omitempty"`
	Translations map[string]string          `json:"translations,omitempty"`
	Values       map[string]json.RawMessage `json:"values,omitempty"`
	Reason       string                     `json:"reason,omitempty"`
	Code         string                     `json:"code"`
	Message      string                     `json:"message"`
	Path         []string                   `json:"path"`
	RelatedPaths [][]string                 `json:"relatedPaths,omitempty"`
}

type InputOptions struct {
	Manual bool          `json:"manual,omitempty"`
	Items  []InputOption `json:"items"`
}

type InputOption struct {
	ID     string `json:"id" source:"record"`
	Name   string `json:"name" source:"record" from:"Source#name"`
	Unit   string `json:"unit,omitempty" source:"record"`
	Source string `json:"-"`
}

func StaticControlAction(action Action) bool {
	for _, prefix := range []string{"platform.", "system.", "ai.", "enterprise."} {
		if strings.HasPrefix(action.Target, prefix) {
			return true
		}
	}
	switch action.Target {
	case "agent.run", "agent.switch", "agent.evaluation", "agent.memory", "work.task", "platform.work", "platform.effect", "platform.operation", "platform.job",
		"build.object", "build.page", "build.app", "build.flow", "build.agent", "build.function", "build.code", "build.query", "build.process", "build.connection", "build.pipeline", "build.dataset", "build.datasetversion", "build.evaluation", "build.release", "build.packagesource", "build.alertrule":
		return true
	}
	return false
}

type DecisionTrace struct {
	Kind          string          `json:"kind"`
	Name          string          `json:"name,omitempty"`
	Type          string          `json:"type,omitempty"`
	ID            string          `json:"id,omitempty"`
	Operation     string          `json:"operation,omitempty"`
	Expression    *Expr           `json:"expression,omitempty"`
	Reads         *ExprContext    `json:"reads,omitempty"`
	Result        *ExprResult     `json:"result,omitempty"`
	Request       *AuthzRequest   `json:"request,omitempty"`
	Authorization *AuthzDecision  `json:"authorization,omitempty"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
	Refusal       string          `json:"refusal,omitempty"`
}

type DecisionTraceRecorder interface {
	TraceEnabled() bool
	RecordDecisionTrace(DecisionTrace)
}

func (c Caller) TraceEnabled() bool {
	r, ok := c.rt.(DecisionTraceRecorder)
	return ok && r.TraceEnabled()
}

func (c Caller) Trace(event DecisionTrace) {
	if r, ok := c.rt.(DecisionTraceRecorder); ok && r.TraceEnabled() {
		r.RecordDecisionTrace(event)
	}
}
