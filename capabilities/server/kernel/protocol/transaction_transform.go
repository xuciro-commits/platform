package protocol

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
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
	FormulaExpr *platform.Expr     `json:"formulaExpr,omitempty" type:"json" field:"aside"`
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
	Entities map[string]platform.RecordType
	Ledger   platform.Authority
	Lookup   func(string) (platform.RecordTypeInfo, bool)
	Prepare  func(platform.Caller, *pb.ChangeRecord, *platform.BusinessTxn, time.Time) (TransactionProposal, *platform.Error)
	Link     func(platform.AssetBinding) (platform.LinkType, error)
	Effect   func(platform.Caller, string) (TransactionEffectDefinition, *platform.Error)
	Dispatch func(platform.Caller, *pb.ChangeRecord, *platform.BusinessTxn, platform.RuleReadSpec, map[string]any, map[string]any, time.Time) *platform.Error
	Confirm  func(platform.Caller, *pb.ChangeRecord, *platform.BusinessTxn, time.Time) *platform.Error
}

type TransactionProposal struct {
	Apply  func(platform.RecordType, platform.RecordTypeInfo, map[string]any, []string) (platform.RecordType, map[string]any)
	Finish func(platform.Caller, *pb.ChangeRecord, time.Time) *platform.Error
}

type TransactionEffectDefinition struct {
	Request func(string, json.RawMessage) (json.RawMessage, *platform.Error)
}

type TransactionExecutor interface {
	Transact(platform.Caller, *pb.ChangeRecord, *platform.BusinessTxn, time.Time) *platform.Error
}
