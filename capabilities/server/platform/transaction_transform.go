package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
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

type BusinessRuntimeContext struct {
	App      string
	Entities map[string]Entity
	Ledger   Authority
	Lookup   func(string) (EntityInfo, bool)
	Prepare  func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) (TransactionProposal, *Error)
	Link     func(AssetBinding) (LinkType, error)
	Effect   func(Caller, string) (TransactionEffectDefinition, *Error)
	Dispatch func(Caller, *pb.ChangeRecord, *BusinessTxn, RuleReadSpec, map[string]any, map[string]any, time.Time) *Error
	Confirm  func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *Error
}

type TransactionProposal struct {
	Apply  func(Entity, EntityInfo, map[string]any, []string) (Entity, map[string]any)
	Finish func(Caller, *pb.ChangeRecord, time.Time) *Error
}

type TransactionEffectDefinition struct {
	Request func(string, json.RawMessage) (json.RawMessage, *Error)
}

type TransactionExecutor interface {
	Transact(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *Error
}
