package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

type BuildHost interface {
	TransactionRuntime(BusinessRuntimeContext) TransactionExecutor
	SystemDefinitionReadOnly(AssetRef) bool
	Action(schema string) (app string, a Action, ok bool)
	Automation(parent Caller, app string) Caller
	BindInterfaceQuery(NamedQuery, ...EntityInfo) (NamedQuery, error)
	Caller(parent Caller, m Member, app string) Caller
	Declares(name string) bool
	Entity(typ string) (EntityInfo, bool)
	EntitySource(typ string) (AssetBinding, bool)
	Function(app, name string, version int) (AIFunction, int, bool)
	Install(c Caller, e Entity, actions []Action, pages ...Page) error
	InstallApplication(c Caller, a Application) error
	InstallFunction(c Caller, f AIFunction, version int) error
	InstallLinkType(c Caller, l LinkType, version int) error
	InstallOperation(c Caller, o Operation, version int) error
	InstallPage(c Caller, p Page) error
	InstallPropertyType(c Caller, p PropertyType, version int) error
	InstallValueType(c Caller, v ValueType, version int) error
	InstallQuery(c Caller, q NamedQuery, version int) error
	Interfaces() []Interface
	Member(id string) (Member, bool)
	Operation(app, name string, version int) (Operation, int, bool)
	RuleQuery(app, name string, version int) (NamedQuery, int, bool)
	BuildProcesses() ProcessDefinitions
	BuildProcessConsumptions() []Consumption
	Record(ref string) (any, bool)
	ResolvePropertyType(binding AssetBinding) (PropertyType, bool)
	ResolveValueType(binding AssetBinding) (ValueType, bool)
	ValidateInstallApplication(a Application) error
	ValidateInstallDependents(c Caller, e Entity, actions []Action, generatedPage string, pages ...Page) error
	ValidateInstallFunction(f AIFunction) error
	ValidateInstallLinkType(l LinkType) error
	ValidateInstallOperation(c Caller, o Operation) error
	ValidateInstallPage(p Page) error
	ValidateInstallPropertyType(p PropertyType) error
	ValidateInstallValueType(v ValueType) error
	ValidateObjectPresentation(info EntityInfo) error
	ValidateInstallQuery(q NamedQuery) error
	ValidateCodeAsset(ReleaseAsset) error
}

type RecordSetHost interface {
	ValidateRecordSet(EntityInfo, RecordSet) error
}

type ModelPageHost interface {
	CheckPageModel(Caller, Page) error
	ModelQueryBinding(AssetRef) (AssetBinding, bool)
}

type AttachedBuild interface{ AttachBuild(BuildHost) }

type ApplicationAIEvaluation struct {
	ID        string  `json:"id"`
	Candidate string  `json:"candidate"`
	State     string  `json:"state"`
	Model     string  `json:"model"`
	Quality   float64 `json:"quality"`
	CostUSD   float64 `json:"costUsd"`
}

type ApplicationRunPage struct {
	Application AssetRef              `json:"application"`
	Title       string                `json:"title"`
	Runs        []ApplicationRun      `json:"runs"`
	Total       int                   `json:"total"`
	Summary     ApplicationRunSummary `json:"summary"`
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
	Ref      AssetRef `json:"ref"`
	Records  int      `json:"records"`
	Expected int64    `json:"expected,omitempty"`
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
	BusinessRefusals   int                      `json:"businessRefusals"`
	RefusalCodes       []RefusalCodeCount       `json:"refusalCodes"`
	Accepted           int                      `json:"accepted"`
	Refused            int                      `json:"refused"`
	ConstraintFailures int                      `json:"constraintFailures"`
	RefusalRate        float64                  `json:"refusalRate"`
	Runs               ApplicationRunSummary    `json:"runs"`
	MemberQuota        *AIQuota                 `json:"memberQuota,omitempty"`
	AgentBudgets       []ApplicationAgentBudget `json:"agentBudgets"`
}

type ApplicationAgentBudget struct {
	Resource AssetBinding `json:"resource"`
	Budget   Budget       `json:"budget"`
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
	Resource AssetBinding  `json:"resource"`
	Base     string        `json:"base"`
	Source   *AssetBinding `json:"source,omitempty"`
}

type ApplicationRetention struct {
	Category string `json:"category"`
	Duration string `json:"duration"`
	State    string `json:"state"`
}

type ApplicationDecisionTrace struct {
	Change     string               `json:"change"`
	Schema     string               `json:"schema"`
	At         time.Time            `json:"at"`
	Content    *SchemaVersion       `json:"content,omitempty"`
	Versions   []ApplicationVersion `json:"versions"`
	Definition *DecisionDefinition  `json:"definition,omitempty"`
}

type ApplicationDiagnostic struct {
	Application AssetRef                   `json:"application"`
	Type        string                     `json:"type"`
	Record      string                     `json:"record"`
	Decisions   []ApplicationDecisionTrace `json:"decisions"`
	Runs        []ApplicationRun           `json:"runs"`
}

type ApplicationAI struct {
	Resource    AssetBinding              `json:"resource"`
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

type ApplicationRun struct {
	Applications []AssetBinding `json:"applications,omitempty"`
	Subject      string         `json:"subject,omitempty"`
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Title        string         `json:"title"`
	Resource     *AssetRef      `json:"resource,omitempty"`
	ResourceName string         `json:"resourceName"`
	State        string         `json:"state"`
	Version      string         `json:"version,omitempty"`
	Release      string         `json:"release,omitempty"`
	Dependencies string         `json:"dependencies,omitempty"`
	Module       string         `json:"module,omitempty"`
	Node         string         `json:"node,omitempty"`
	Error        string         `json:"error,omitempty"`
	Association  string         `json:"association"`
	Shared       []string       `json:"shared"`
	Created      time.Time      `json:"created"`
	AITokens     int            `json:"aiTokens,omitempty"`
	AIUSD        float64        `json:"aiUsd,omitempty"`
	CostReported bool           `json:"costReported,omitempty"`
}

type ApplicationPage struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Object      string `json:"object,omitempty"`
}

type ApplicationView struct {
	App         string            `json:"app"`
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	Source      string            `json:"source"`
	Pages       []ApplicationPage `json:"pages"`
}

type PageView struct {
	Application ApplicationView `json:"application"`
	Page        Page            `json:"page"`
	Entity      *EntityInfo     `json:"entity,omitempty"`
	Actions     []Action        `json:"actions"`
}

type ApplicationProfile struct {
	ExpectedRecords map[string]int64  `json:"expectedRecords,omitempty"`
	ExpectedRuns    int64             `json:"expectedRuns,omitempty"`
	Retention       map[string]string `json:"retention,omitempty"`
	RPO             string            `json:"rpo,omitempty"`
	RTO             string            `json:"rto,omitempty"`
}

func (p *ApplicationProfile) UnmarshalJSON(raw []byte) error {
	type declaration ApplicationProfile
	var decoded declaration
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if err := reader.Decode(&decoded); err != nil {
		return err
	}
	if reader.Decode(new(any)) != io.EOF {
		return fmt.Errorf("application profile has trailing data")
	}
	*p = ApplicationProfile(decoded)
	return p.Check()
}

func (p ApplicationProfile) Check() error {
	if len(p.ExpectedRecords) > 1000 || p.ExpectedRuns < 0 || len(p.Retention) > 16 {
		return fmt.Errorf("application profile exceeds its declaration budget")
	}
	for typ, count := range p.ExpectedRecords {
		if count < 1 || !strings.Contains(typ, ".") || len(typ) > 256 {
			return fmt.Errorf("expected records need a type and a positive count")
		}
	}
	for name, duration := range p.Retention {
		if !slices.Contains([]string{"runs", "logs", "effects", "ai", "compute"}, name) {
			return fmt.Errorf("unknown application retention category")
		}
		if value, err := time.ParseDuration(duration); err != nil || value <= 0 {
			return fmt.Errorf("application retention needs a positive duration")
		}
	}
	for _, duration := range []string{p.RPO, p.RTO} {
		if duration == "" {
			continue
		}
		if value, err := time.ParseDuration(duration); err != nil || value <= 0 {
			return fmt.Errorf("application recovery declarations need positive durations")
		}
	}
	return nil
}
