package platform

import (
	"encoding/json"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type DecisionRuntime interface {
	Decide(Caller, Service, *pb.Submission, time.Time) (*pb.ChangeRecord, *Error)
	Attempt(func() (*pb.ChangeRecord, *Error)) (*pb.ChangeRecord, *Error)
	ProbeDecision(Caller, func(Caller) *Error) *Error
}

type ReadRuntime interface {
	ReadQuery(Caller, string, string, json.RawMessage, time.Time) (json.RawMessage, *Error)
	ReadQueryVersion(Caller, string, string, int, json.RawMessage, time.Time) (json.RawMessage, *Error)
	ReadRecord(Caller, string, string, time.Time) (json.RawMessage, *Error)
	ReadRecordPath(Caller, string, string, []string, time.Time) (json.RawMessage, []string, *Error)
}

type OperationRuntime interface {
	RequestOperation(Caller, *pb.ChangeRecord, OperationRequest) (OperationCall, *Error)
	OperationResult(Caller, string) (OperationResult, *Error)
	CancelOperation(Caller, *pb.ChangeRecord, string) *Error
	RequestCompilation(Caller, *pb.ChangeRecord, string, string, CodeBuildRequest) (OperationCall, *Error)
}

type FunctionRuntime interface {
	RequestFunction(Caller, *pb.ChangeRecord, FunctionRequest) (FunctionCall, *Error)
}

type ApprovalRuntime interface {
	RequestApproval(Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *Error)
}

type NotificationRuntime interface {
	Notify(Caller, Notification, time.Time, []Recipient) []string
	Seen(Caller, ...string)
}

type InputDiagnosticRuntime interface {
	InputRefused(*Error, []FieldIssue)
}

type ControlRuntime interface {
	ControlDecision(Caller, *pb.Submission, time.Time) (func(*pb.ChangeRecord), *Error)
}

type EffectRuntime interface {
	Publish(Caller, *pb.ChangeRecord)
	Probe(Caller, string, string, string, []byte, time.Time) *Error
	Request(Caller, *pb.ChangeRecord, Request)
	Query(Caller, string, string, ...string) ([]ProviderResult, *Error)
	BoundProvider(Caller, string) (string, *Error)
	Emit(Caller, string, string, string, any, time.Time) (int, *Error)
}

type SettingRuntime interface {
	Setting(Caller, string) string
}

type OrganizationRuntime interface {
	Units(Caller, string, time.Time) []string
	Calendar(Caller, time.Time) Calendar
	Element(Caller, string, time.Time) (ElementInfo, bool)
	Related(Caller, string, string, bool, time.Time) []string
	Links(Caller, string) []string
	Link(Caller, *pb.EntityRef, *pb.EntityRef, string, time.Time) *Error
}

type RecordRuntime interface {
	Put(Caller, *pb.ChangeRecord, any) *Error
	Get(Caller, reflect.Type, string) (any, bool)
	Find(Caller, reflect.Type, Query) ([]any, int, *Error)
	Check(Caller, any) *Error
	Assign(Caller, *pb.ChangeRecord, Assignment) *Error
	Readable(Caller, string) bool
	Next(Caller, *pb.ChangeRecord, string, time.Time) (string, *Error)
	Rules() RuleEngine
	ReadRuleQuery(Caller, RuleCollection, json.RawMessage, time.Time) (json.RawMessage, *Error)
	ReadRecordSet(Caller, string, string, string, time.Time) (RecordSetResult, *Error)
	ResolveValueType(AssetBinding) (ValueType, bool)
}

type AuthorizationRuntime interface {
	Authorize(Caller, AuthzRequest, time.Time) AuthzDecision
	AuthorizationEngine() AuthorizationEngine
}

type ApplicationViewRuntime interface {
	VisibleApplications(Caller) []AssetRef
	TraceEnabled() bool
	RecordDecisionTrace(DecisionTrace)
	StagedDecision() StagedDecision
	ReadScoped() bool
	Simulating() bool
}

type RelationRuntime interface {
	OwnedRelations(Caller, string) []RelationLink
	LegacyRelationActive(Caller, RelationLink) bool
	LegacyRelationWritable(Caller, string, string) bool
}

type EnterpriseRuntime interface {
	CheckEnterpriseUsage(Caller, string, string, bool, time.Time) *Error
	CheckEnterpriseTenantConnection(Caller, string, time.Time) *Error
}

type Runtime interface {
	DecisionRuntime
	ReadRuntime
	OperationRuntime
	FunctionRuntime
	ApprovalRuntime
	NotificationRuntime
	InputDiagnosticRuntime
	ControlRuntime
	EffectRuntime
	SettingRuntime
	OrganizationRuntime
	RecordRuntime
	AuthorizationRuntime
	ApplicationViewRuntime
	RelationRuntime
	EnterpriseRuntime
}
