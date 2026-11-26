package authoring

import (
	"errors"
	"slices"
	"sort"
	"unicode/utf16"

	"platformserver/services/ontology"
)

type FieldExpressionCheck struct {
	Fields []ontology.Field `json:"fields"`
	Field  ontology.Field   `json:"field"`
}

type ExpressionIssue struct {
	Slot    string `json:"slot"`
	From    int    `json:"from"`
	To      int    `json:"to"`
	Message string `json:"message"`
}

type ExpressionOption struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

type FieldExpressionDiagnostics struct {
	Valid    bool               `json:"valid"`
	Issues   []ExpressionIssue  `json:"issues"`
	Operands []ExpressionOption `json:"operands"`
	Defaults []ExpressionOption `json:"defaults"`
}

func CheckFieldExpressions(q FieldExpressionCheck) FieldExpressionDiagnostics {
	f := q.Field
	fields := append(slices.DeleteFunc(slices.Clone(q.Fields), func(other ontology.Field) bool { return other.Name == f.Name }), f)
	answer := FieldExpressionDiagnostics{Valid: true, Issues: []ExpressionIssue{}, Operands: []ExpressionOption{}, Defaults: []ExpressionOption{}}
	titles := map[string]string{}
	for _, field := range fields {
		titles[field.Name] = field.Title
	}
	for name, kind := range ontology.FormulaOperands(fields) {
		if name != f.Name {
			answer.Operands = append(answer.Operands, ExpressionOption{Label: name, Detail: titles[name] + " · " + kind})
		}
	}
	sort.Slice(answer.Operands, func(i, j int) bool { return answer.Operands[i].Label < answer.Operands[j].Label })
	for _, value := range ontology.DefaultOptions(f) {
		answer.Defaults = append(answer.Defaults, ExpressionOption{Label: value})
	}
	if f.Formula != "" {
		if f.Type != "integer" && f.Type != "decimal" {
			answer.add("formula", f.Formula, errors.New("a formula needs an integer or decimal field"))
		} else if _, err := ontology.FormulaExpr(fields, f); err != nil {
			answer.add("formula", f.Formula, err)
		}
	}
	if f.Default != "" {
		if err := ontology.CheckDefaults(ontology.Object{Fields: []ontology.Field{f}}); err != nil {
			answer.add("default", f.Default, err)
		}
	}
	return answer
}

func (a *FieldExpressionDiagnostics) add(slot, text string, err error) {
	issue := ExpressionIssue{Slot: slot, To: utf16Units(text, len(text)), Message: err.Error()}
	var at interface{ Span() (int, int) }
	if errors.As(err, &at) {
		from, to := at.Span()
		issue.From, issue.To, issue.Message = utf16Units(text, from), utf16Units(text, to), at.(error).Error()
	}
	a.Valid = false
	a.Issues = append(a.Issues, issue)
}

func utf16Units(text string, offset int) int {
	return len(utf16.Encode([]rune(text[:min(max(offset, 0), len(text))])))
}
