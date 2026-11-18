package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type Constraint struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind" enum:"required,unique,range,pattern,expression,cardinality,immutable,temporal"`
	Fields       []string          `json:"fields"`
	Expr         *Expr             `json:"expr,omitempty"`
	Message      string            `json:"message"`
	Translations map[string]string `json:"translations,omitempty"`
	Scope        string            `json:"scope,omitempty"`
	Level        string            `json:"level,omitempty" enum:"record,txn"`
	Reads        *RuleReadSpec     `json:"reads,omitempty"`
}

func (c *Constraint) UnmarshalJSON(raw []byte) error {
	type plain Constraint
	return strictTxn(raw, (*plain)(c))
}

func (c Caller) RefuseConstraint(constraint Constraint, record string, values map[string]json.RawMessage, reason string) *Error {
	issue := FieldIssue{Code: "constraint_violation", Constraint: constraint.Name, Record: record, Message: constraint.Message, Translations: constraint.Translations, Values: values, Reason: reason, Path: []string{constraint.Fields[0]}}
	for _, field := range constraint.Fields[1:] {
		issue.RelatedPaths = append(issue.RelatedPaths, []string{field})
	}
	err := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Constraint {name}: {message}", constraint.Name, constraint.Message)
	if c.inputRefused != nil {
		c.inputRefused(err, []FieldIssue{issue})
	}
	if c.rt != nil {
		c.rt.InputRefused(err, []FieldIssue{issue})
	}
	return err
}
