package protocol

import (
	"platformserver/platform"
	"time"
)

type BuildHost interface {
	ModelPageHost
	CompartmentExists(string) bool
	ProjectApprovals(string) int
	CheckResourceSchema(string, *platform.SchemaVersion) *platform.Error
	TransactionRuntime(BusinessRuntimeContext) TransactionExecutor
	ManagedDefinition(platform.AssetRef) bool
	Action(schema string) (app string, a platform.Action, ok bool)
	Automation(parent platform.Caller, app string) platform.Caller
	BindInterfaceQuery(platform.NamedQuery, ...platform.RecordTypeInfo) (platform.NamedQuery, error)
	Caller(parent platform.Caller, m platform.Member, app string) platform.Caller
	Declares(name string) bool
	Entity(typ string) (platform.RecordTypeInfo, bool)
	EntitySource(typ string) (platform.AssetBinding, bool)
	Function(app, name string, version int) (platform.AIFunction, int, bool)
	Install(c platform.Caller, e platform.RecordType, actions []platform.Action, pages ...platform.Page) error
	InstallApplication(c platform.Caller, a platform.Application) error
	InstallFunction(c platform.Caller, f platform.AIFunction, version int) error
	InstallInterface(c platform.Caller, i platform.Interface, version int) error
	InstallLinkType(c platform.Caller, l platform.LinkType, version int) error
	InstallOperation(c platform.Caller, o platform.Operation, version int) error
	InstallPage(c platform.Caller, p platform.Page) error
	InstallObjectTypeGroup(c platform.Caller, g platform.ObjectTypeGroup, version int) error
	InstallPropertyType(c platform.Caller, p platform.PropertyType, version int) error
	InstallValueType(c platform.Caller, v platform.ValueType, version int) error
	InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error
	Interfaces() []platform.Interface
	Member(id string) (platform.Member, bool)
	Holding(app, role string) []string
	FreezeConfirmationProtocol(platform.ConfirmationParty) (*platform.ConfirmationProtocol, error)
	Operation(app, name string, version int) (platform.Operation, int, bool)
	RuleQuery(app, name string, version int) (platform.NamedQuery, int, bool)
	BuildProcesses() platform.ProcessDefinitions
	BuildProcessConsumptions() []platform.Consumption
	Record(ref string) (any, bool)
	ResolvePropertyType(binding platform.AssetBinding) (platform.PropertyType, bool)
	ResolveValueType(binding platform.AssetBinding) (platform.ValueType, bool)
	ValidateInstallApplication(a platform.Application) error
	ValidateInstallDependents(c platform.Caller, e platform.RecordType, actions []platform.Action, generatedPage string, pages ...platform.Page) error
	ValidateInstallFunction(f platform.AIFunction) error
	ValidateInstallInterface(i platform.Interface) error
	ValidateInstallLinkType(l platform.LinkType) error
	ValidateInstallOperation(c platform.Caller, o platform.Operation) error
	ValidateInstallPage(p platform.Page) error
	ValidateInstallObjectTypeGroup(g platform.ObjectTypeGroup) error
	ValidateInstallPropertyType(p platform.PropertyType) error
	ValidateInstallValueType(v platform.ValueType) error
	ValidateObjectPresentation(info platform.RecordTypeInfo) error
	ValidateInstallQuery(q platform.NamedQuery) error
	ValidateRecordSet(platform.RecordTypeInfo, platform.RecordSet) error
	ValidateCodeAsset(platform.ReleaseAsset) error
	ProductVersion(platform.ProductVersionRequest) (platform.FrozenProductVersion, error)
}

type ModelPageHost interface {
	CheckPageModel(platform.Caller, platform.Page) error
	ModelQueryBinding(platform.AssetRef) (platform.AssetBinding, bool)
}

type ApplicationAIEvaluation struct {
	ID        string  `json:"id"`
	Candidate string  `json:"candidate"`
	State     string  `json:"state"`
	Model     string  `json:"model"`
	Quality   float64 `json:"quality"`
	CostUSD   float64 `json:"costUsd"`
}

type ApplicationRunPage struct {
	Application platform.AssetRef         `json:"application"`
	Title       string                    `json:"title"`
	Runs        []platform.ApplicationRun `json:"runs"`
	Total       int                       `json:"total"`
	Summary     ApplicationRunSummary     `json:"summary"`
}

type ApplicationRunSummary struct {
	Failed         int     `json:"failed"`
	PendingEffects int     `json:"pendingEffects"`
	WaitingFlows   int     `json:"waitingFlows"`
	AITokens       int     `json:"aiTokens"`
	AIUSD          float64 `json:"aiUsd"`
	UnmeteredAI    int     `json:"unmeteredAI"`
}

type ApplicationRunFilter struct {
	Kind    string    `json:"kind,omitempty"`
	State   string    `json:"state,omitempty"`
	Subject string    `json:"subject,omitempty"`
	Since   time.Time `json:"since,omitzero"`
	Until   time.Time `json:"until,omitzero"`
}

type ApplicationVersion struct {
	Version string    `json:"version"`
	Release string    `json:"release,omitempty"`
	At      time.Time `json:"at,omitzero"`
	Active  bool      `json:"active"`
}

type ApplicationData struct {
	Ref      platform.AssetRef `json:"ref"`
	Records  int               `json:"records"`
	Expected int64             `json:"expected,omitempty"`
}

type ApplicationWarning struct {
	Kind     string `json:"kind"`
	Resource string `json:"resource,omitempty"`
	Actual   int64  `json:"actual"`
	Expected int64  `json:"expected"`
}

type ApplicationRecovery struct {
	State          string `json:"state"`
	Predecessors   string `json:"predecessors"`
	Quarantined    bool   `json:"quarantined"`
	Scope          string `json:"scope"`
	PendingEffects int    `json:"pendingEffects"`
	FailedRuns     int    `json:"failedRuns"`
}

type ApplicationMonitoring struct {
	BusinessRefusals   int                         `json:"businessRefusals"`
	RefusalCodes       []platform.RefusalCodeCount `json:"refusalCodes"`
	Accepted           int                         `json:"accepted"`
	Refused            int                         `json:"refused"`
	ConstraintFailures int                         `json:"constraintFailures"`
	RefusalRate        float64                     `json:"refusalRate"`
	Runs               ApplicationRunSummary       `json:"runs"`
	MemberQuota        *platform.AIQuota           `json:"memberQuota,omitempty"`
	AgentBudgets       []ApplicationAgentBudget    `json:"agentBudgets"`
}

type ApplicationAgentBudget struct {
	Resource platform.AssetBinding `json:"resource"`
	Budget   platform.Budget       `json:"budget"`
}

type ApplicationRuntime struct {
	AI          []ApplicationAI        `json:"ai"`
	Artifact    ApplicationArtifact    `json:"artifact"`
	State       string                 `json:"state"`
	Scope       string                 `json:"scope"`
	Versions    []ApplicationVersion   `json:"versions"`
	Previous    string                 `json:"previous,omitempty"`
	Data        []ApplicationData      `json:"data"`
	Runs        int                    `json:"runs"`
	Warnings    []ApplicationWarning   `json:"warnings"`
	Recovery    ApplicationRecovery    `json:"recovery"`
	Monitoring  ApplicationMonitoring  `json:"monitoring"`
	Maintenance ApplicationMaintenance `json:"maintenance"`
}

type ApplicationMaintenance struct {
	Extensions []ApplicationExtension `json:"extensions"`
	Retention  []ApplicationRetention `json:"retention"`
}

type ApplicationExtension struct {
	Resource platform.AssetBinding  `json:"resource"`
	Base     string                 `json:"base"`
	Source   *platform.AssetBinding `json:"source,omitempty"`
}

type ApplicationRetention struct {
	Category string `json:"category"`
	Duration string `json:"duration"`
	State    string `json:"state"`
}

type ApplicationDecisionTrace struct {
	Change     string                       `json:"change"`
	Schema     string                       `json:"schema"`
	At         time.Time                    `json:"at"`
	Content    *platform.SchemaVersion      `json:"content,omitempty"`
	Versions   []ApplicationVersion         `json:"versions"`
	Definition *platform.DecisionDefinition `json:"definition,omitempty"`
}

type ApplicationDiagnostic struct {
	Application platform.AssetRef          `json:"application"`
	Type        string                     `json:"type"`
	Record      string                     `json:"record"`
	Decisions   []ApplicationDecisionTrace `json:"decisions"`
	Runs        []platform.ApplicationRun  `json:"runs"`
}

type ApplicationAI struct {
	Resource    platform.AssetBinding     `json:"resource"`
	Placement   string                    `json:"placement"`
	Model       string                    `json:"model,omitempty"`
	HandoffRole string                    `json:"handoffRole,omitempty"`
	Adoptions   []string                  `json:"adoptions"`
	Checkpoints []string                  `json:"checkpoints"`
	Evaluations []ApplicationAIEvaluation `json:"evaluations"`
	Calls       int                       `json:"calls"`
	SharedCalls int                       `json:"sharedCalls"`
	Input       int                       `json:"input"`
	Output      int                       `json:"output"`
	Cost        float64                   `json:"cost"`
	CostUnknown int                       `json:"costUnknown"`
}

type ApplicationPage struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Object      string `json:"object,omitempty"`
}

type ApplicationMenuGroup struct {
	Title   string            `json:"title,omitempty"`
	Entries []ApplicationPage `json:"entries"`
}

type ApplicationView struct {
	Menu        []ApplicationMenuGroup `json:"menu"`
	App         string                 `json:"app"`
	Name        string                 `json:"name"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Icon        string                 `json:"icon,omitempty"`
	Source      string                 `json:"source"`
}

type PageView struct {
	Application ApplicationView          `json:"application"`
	Page        platform.Page            `json:"page"`
	Entity      *platform.RecordTypeInfo `json:"entity,omitempty"`
	Actions     []platform.Action        `json:"actions"`
}

type PageRequest struct {
	App   string `json:"app"`
	Owner string `json:"owner,omitempty"`
	Page  string `json:"page"`
}

type RetainedRecordLink struct {
	Object string `json:"object"`
}
