package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// AIFunction is a bounded, declarative inference over explicit scalar fields
// of one object (ADR-0043). It has no tools, code or business writes; the
// application's ordinary Reply action decides whether to adopt the answer.
type AIFunction struct {
	Name           string   `json:"name"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Object         string   `json:"object"`
	Fields         []string `json:"fields"`
	Instructions   string   `json:"instructions"`
	Output         []Field  `json:"output"`
	Model          string   `json:"model,omitempty"`
	MaxInputBytes  int      `json:"maxInputBytes"`
	MaxOutputBytes int      `json:"maxOutputBytes"`
	MaxTokens      int      `json:"maxTokens"`
	Roles          []string `json:"roles"`
}

// FunctionCall identifies the exact accepted input without returning private
// prompt bytes. Apps retain Sources alongside derived answer fields so their
// Entity.Derived declarations enforce current read permissions (ADR-0033).
type FunctionCall struct {
	Definition   string   `json:"definition"`
	Dependencies string   `json:"dependencies"`
	Model        string   `json:"model"`
	InputHash    string   `json:"inputHash"`
	Sources      []string `json:"sources"`
}

// RequestFunction plans this application's declared function over the target
// of its accepted decision. It uses the same model effect and Reply path as
// Request; a refusal also invalidates the enclosing staged decision.
func (c Caller) RequestFunction(r *pb.ChangeRecord, name, reply string) (FunctionCall, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		RequestFunction(Caller, *pb.ChangeRecord, string, string) (FunctionCall, *kernel.Error)
	}); ok {
		return rt.RequestFunction(c, r, name, reply)
	}
	return FunctionCall{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "AI functions require an accepted decision")
}

var functionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var functionFieldName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

func (f AIFunction) Check() error {
	if !functionName.MatchString(f.Name) || f.Title == "" || len(f.Title) > 256 || f.Description == "" || len(f.Description) > 1024 || f.Object == "" ||
		len(f.Instructions) == 0 || len(f.Instructions) > 8192 || len(f.Fields) == 0 || len(f.Fields) > 32 ||
		len(f.Output) == 0 || len(f.Output) > 16 || len(f.Roles) == 0 ||
		f.MaxInputBytes < 1 || f.MaxInputBytes > 32<<10 || f.MaxOutputBytes < 1 || f.MaxOutputBytes > 32<<10 ||
		f.MaxTokens < 1 || f.MaxTokens > 4096 {
		return fmt.Errorf("function %s needs named, typed inputs/outputs, roles and bounded instructions/bytes/tokens", f.Name)
	}
	seen := map[string]bool{}
	for _, name := range f.Fields {
		if !functionFieldName.MatchString(name) || seen[name] {
			return fmt.Errorf("function %s needs unique input fields", f.Name)
		}
		seen[name] = true
	}
	seen = map[string]bool{}
	for _, field := range f.Output {
		if !functionFieldName.MatchString(field.Name) || seen[field.Name] || field.Description == "" || len(field.Description) > 1024 ||
			field.Type != "string" && field.Type != "boolean" && field.Type != "integer" && field.Type != "decimal" ||
			field.Ref != "" || field.From != "" || field.Key != "" || field.Label != "" ||
			len(field.Choices) > 0 && field.Type != "string" {
			return fmt.Errorf("function %s needs unique scalar output fields", f.Name)
		}
		seen[field.Name] = true
		choices := map[string]bool{}
		for _, choice := range field.Choices {
			if choice == "" || choices[choice] {
				return fmt.Errorf("function %s needs unique nonempty choices", f.Name)
			}
			choices[choice] = true
		}
	}
	seen = map[string]bool{}
	for _, role := range f.Roles {
		if role == "" || seen[role] {
			return fmt.Errorf("function %s needs unique nonempty roles", f.Name)
		}
		seen[role] = true
	}
	return nil
}

// ValidateOutput is the portable output guarantee, independent of a model
// vendor's schema feature: exactly one JSON object, no duplicate/unknown keys,
// no null/type coercion, bounded bytes and declared scalar types/choices.
func (f AIFunction) ValidateOutput(raw []byte) error {
	if err := f.Check(); err != nil {
		return err
	}
	if len(raw) > f.MaxOutputBytes || !utf8.Valid(raw) {
		return fmt.Errorf("function output exceeds its byte bound or is not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("function output must be one JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		i := slices.IndexFunc(f.Output, func(x Field) bool { return x.Name == name })
		if err != nil || !ok || seen[name] || i < 0 {
			return fmt.Errorf("function output has an unknown or duplicate field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("function output has an invalid value")
		}
		field := f.Output[i]
		switch field.Type {
		case "string":
			var s string
			if json.Unmarshal(value, &s) != nil || len(field.Choices) > 0 && !slices.Contains(field.Choices, s) {
				return fmt.Errorf("function output has an invalid string or choice")
			}
		case "boolean":
			var b bool
			if json.Unmarshal(value, &b) != nil {
				return fmt.Errorf("function output has an invalid boolean")
			}
		case "integer":
			var n int64
			if json.Unmarshal(value, &n) != nil {
				return fmt.Errorf("function output has an invalid integer")
			}
		case "decimal":
			var n float64
			if json.Unmarshal(value, &n) != nil {
				return fmt.Errorf("function output has an invalid decimal")
			}
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return fmt.Errorf("function output is incomplete")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("function output has trailing data")
	}
	for _, field := range f.Output {
		if field.Required && !seen[field.Name] {
			return fmt.Errorf("function output omits a required field")
		}
	}
	return nil
}

// RecordAdvice is the first shared typed inference result. It is a suggestion,
// never an authority to execute a business action.
type RecordAdvice struct {
	Summary  string `json:"summary"`
	Category string `json:"category"`
	Review   bool   `json:"review"`
}

func RecordAdviceFunction(object string, fields, roles []string) AIFunction {
	return AIFunction{Name: "record-advice", Title: "Record summary and review advice",
		Description: "Summarise the selected record and suggest whether a person should review it.",
		Object:      object, Fields: fields, Roles: roles, MaxInputBytes: 4096, MaxOutputBytes: 1024, MaxTokens: 256,
		Instructions: "Summarise only the provided record. Use category routine or review. Flag review when the record needs human attention; do not invent facts or propose executing actions.",
		Output: []Field{{Name: "summary", Type: "string", Required: true, Description: "A short factual summary"},
			{Name: "category", Type: "string", Required: true, Description: "Routine or needs review", Choices: []string{"routine", "review"}},
			{Name: "review", Type: "boolean", Required: true, Description: "Whether a person should review it"}}}
}
