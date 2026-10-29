package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
)

// AssetKind identifies the existing executor behind a code-defined asset.
// More kinds can be added as their canonical platform owner is built.
type AssetKind string

const (
	AssetObject       AssetKind = "object"
	AssetLinkType     AssetKind = "link-type"
	AssetPropertyType AssetKind = "property-type"
	AssetAction       AssetKind = "action"
	AssetPage         AssetKind = "page"
	// AssetApp is an application a tenant hands to its people: a name, an icon
	// and the pages it holds.
	AssetApp AssetKind = "app"
	// AssetQuery is a named, pure query an app declares once:
	// pages and agent tools run the same declaration.
	AssetQuery AssetKind = "query"
	// AssetFlow is a versioned definition executed by the existing flow app.
	AssetFlow       AssetKind = "flow"
	AssetFunction   AssetKind = "function"
	AssetAgent      AssetKind = "agent"
	AssetEvaluation AssetKind = "evaluation-set"
	AssetTestPlan   AssetKind = "test-plan"
	AssetCompute    AssetKind = "compute"
	// Integration assets describe existing owner configuration. They never
	// include target credentials, cursors, data or running-work state.
	AssetConnection  AssetKind = "connection"
	AssetSource      AssetKind = "source"
	AssetDataset     AssetKind = "dataset"
	AssetPipeline    AssetKind = "pipeline"
	AssetWriteback   AssetKind = "writeback"
	AssetMatching    AssetKind = "matching"
	AssetSampleSet   AssetKind = "sample-set"
	AssetProtocol    AssetKind = "protocol"
	AssetInterface   AssetKind = "interface"
	AssetObservation AssetKind = "observation"
	AssetAlert       AssetKind = "alert"
)

// NamedQuery reads records of one object: fixed conditions, and optionally
// the record it is run for through a reference field (By). It runs through
// the reader's own read of Object, so it never widens what anyone sees.
type NamedQuery struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Object      string `json:"object"`
	Interface   string `json:"interface,omitempty"`
	// InterfaceShape and Implementations are frozen at publication. They are
	// query metadata, never a fabricated entity or a grant to read its members.
	InterfaceShape  *Interface      `json:"interfaceShape,omitempty"`
	Implementations []string        `json:"implementations,omitempty"`
	By              string          `json:"by,omitempty"`
	Domain          json.RawMessage `json:"domain,omitempty"`
	Sort            []string        `json:"sort,omitempty"`
	Limit           int             `json:"limit,omitempty"`
}

// AssetRef is the stable identity shared by code and construction surfaces.
// Its three parts avoid collisions between apps and between kinds. A published
// revision will bind this identity to an immutable definition in a later batch.
type AssetRef struct {
	App  string    `json:"app"`
	Kind AssetKind `json:"kind"`
	Name string    `json:"name"`
}

func (r AssetRef) String() string { return r.App + "/" + string(r.Kind) + "/" + r.Name }

func (r AssetRef) Check() error {
	if r.App == "" || r.Name == "" || strings.Contains(r.App, "/") || strings.Contains(r.Name, "/") || !slices.Contains([]AssetKind{AssetPropertyType, AssetLinkType, AssetObject, AssetAction, AssetPage, AssetApp, AssetQuery, AssetFlow, AssetFunction, AssetCompute,
		AssetConnection, AssetSource, AssetDataset, AssetPipeline, AssetWriteback, AssetMatching, AssetAgent, AssetEvaluation, AssetTestPlan, AssetSampleSet, AssetProtocol, AssetInterface, AssetObservation, AssetAlert}, r.Kind) {
		return fmt.Errorf("asset reference %q needs an app, supported kind and name", r.String())
	}
	return nil
}

// Widgets are the widget kinds a composed page may hold.
// A filter outputs the page's second variable — the records it narrows to —
// which the table, chart and metric over the same object read (16b).
// Filterable are the field types a filter widget offers: values that repeat.
var Filterable = []string{"choice", "boolean", "reference"}

// Application is what someone in this tenant hands to its people: a
// name, an icon from the platform's set, and the pages it holds in order. It
// grants nothing; each page is offered to whoever may read what it shows.
type Application struct {
	Languages    Languages                    `json:"languages,omitempty"`
	Profile      *ApplicationProfile          `json:"profile,omitempty"`
	Header       *ApplicationHeader           `json:"header,omitempty"`
	UIProfile    string                       `json:"uiProfile,omitempty"`
	Variables    map[string]PageVariable      `json:"variables,omitempty"`
	Queries      map[string]PageQuery         `json:"queries,omitempty"`
	Extends      *AssetRef                    `json:"extends,omitempty"`
	Entry        string                       `json:"entry,omitempty"`
	Replacements []ApplicationPageReplacement `json:"replacements,omitempty"`
	Name         string                       `json:"name"`
	Title        string                       `json:"title"`
	Description  string                       `json:"description,omitempty"`
	// Icon is one of Icons: the workspace draws it in the launcher and the navigation.
	Icon  string   `json:"icon,omitempty"`
	Pages []string `json:"pages"` // page names, in the order people see them
	// Groups are headings in its navigation, each over some of
	// Pages in its own order; a page in no group sits under the application's name.
	Groups []AppGroup `json:"groups,omitempty"`
	// Resources are shared owner references included in the release closure.
	// Pages remain the sole navigation list; membership grants no permissions.
	Resources []AssetRef `json:"resources,omitempty"`
	// Concepts organize the studio tree only. They never add dependencies,
	// change asset ownership, or grant access to a grouped asset.
	Concepts []AppConcept `json:"concepts,omitempty"`
}

// CheckResources checks explicit non-navigation resource membership. Assets
// keep their existing owner and may be shared by several applications.
func (a Application) CheckResources() error {
	if err := a.CheckLanguages(); err != nil {
		return err
	}
	if err := a.CheckExtension(); err != nil {
		return err
	}
	if a.Profile != nil {
		if err := a.Profile.Check(); err != nil {
			return err
		}
	}
	if len(a.Resources) > 1000 {
		return fmt.Errorf("application resources exceed 1000 assets")
	}
	seen := map[AssetRef]bool{}
	for _, ref := range a.Resources {
		if err := ref.Check(); err != nil {
			return err
		}
		if ref.Kind == AssetPage || ref.Kind == AssetApp {
			return fmt.Errorf("application resources use non-navigation assets; pages belong in Pages")
		}
		if seen[ref] {
			return fmt.Errorf("application resource %s is declared twice", ref)
		}
		seen[ref] = true
	}
	return a.CheckConcepts()
}

// Dependencies is the single application closure path for discovery and
// releases. Page order affects navigation; dependency order is canonical.
func (a Application) Dependencies(owner string) []AssetRef {
	refs := append(slices.Clone(a.Resources), a.QueryPage().QueryReferences()...)
	if a.Extends != nil {
		refs = append(refs, *a.Extends)
	}
	for _, name := range a.Pages {
		refs = append(refs, AssetRef{App: owner, Kind: AssetPage, Name: name})
	}
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
}

// ApplicationPageReplacement routes an original code view to a page belonging
// to this application. It neither copies nor changes the original page.
type ApplicationPageReplacement struct {
	View string `json:"view"`
	Page string `json:"page"`
}

func (a Application) CheckExtension() error {
	if a.Extends == nil {
		if a.Entry != "" || len(a.Replacements) != 0 {
			return fmt.Errorf("an application entry or page replacement needs an extended application")
		}
		return nil
	}
	if err := a.Extends.Check(); err != nil {
		return err
	}
	if a.Extends.Kind != AssetApp {
		return fmt.Errorf("an application can only extend an application")
	}
	if len(a.Replacements) != 0 && strings.TrimSpace(a.Entry) == "" {
		return fmt.Errorf("application page replacements need an explicit entry")
	}
	views := map[string]bool{}
	for _, replacement := range a.Replacements {
		if strings.TrimSpace(replacement.View) == "" || !slices.Contains(a.Pages, replacement.Page) {
			return fmt.Errorf("application replacement %q needs a view and one of its own pages", replacement.View)
		}
		if views[replacement.View] {
			return fmt.Errorf("application view %q is replaced twice", replacement.View)
		}
		views[replacement.View] = true
	}
	return nil
}

// AppGroup is one heading in an application's navigation and the pages under it.
type AppGroup struct {
	Title string   `json:"title"`
	Pages []string `json:"pages"`
}

// CheckGroups refuses groups people could not read: a heading with no title or
// no page, a page the application does not hold, a page under two headings.
func (a Application) CheckGroups() error {
	grouped := map[string]string{}
	for _, g := range a.Groups {
		if strings.TrimSpace(g.Title) == "" {
			return fmt.Errorf("a group needs a title")
		}
		if len(g.Pages) == 0 {
			return fmt.Errorf("the group %q holds no page", g.Title)
		}
		for _, page := range g.Pages {
			if !slices.Contains(a.Pages, page) {
				return fmt.Errorf("the group %q names the page %q, which the application does not hold", g.Title, page)
			}
			if other, ok := grouped[page]; ok {
				return fmt.Errorf("the page %q is under %q and %q", page, other, g.Title)
			}
			grouped[page] = g.Title
		}
	}
	return nil
}

// IconName reports whether a name is one the kit may draw.
// The vocabulary belongs to the kit, which owns the glyphs and
// their words; the host only keeps names shaped like names, so the set can grow
// without every host having to learn it first. A client that does not know a
// name yet draws its default glyph.
func IconName(s string) bool {
	if s == "" || len(s) > 32 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return !strings.Contains(s, "--")
}

// Definition is one installed code asset as the reader may discover it.
// Entity and Action reuse the same descriptions as the existing record/action
// APIs; this registry is an index over those owners, not another executor.
type Definition struct {
	Alert       *AlertRule                 `json:"alert,omitempty"`
	Observation *ObservationDefinition     `json:"observation,omitempty"`
	SampleSet   *SampleSet                 `json:"sampleSet,omitempty"`
	Interface   *Interface                 `json:"interface,omitempty"`
	Protocol    *ProtocolReleaseDescriptor `json:"protocol,omitempty"`
	// OwnerRecord is the internal metadata read boundary for assets whose
	// descriptor is not itself a record type. It never enters discovery JSON.
	OwnerRecord      string                   `json:"-"`
	ContentVersion   string                   `json:"contentVersion,omitempty"`
	PropertyType     *PropertyType            `json:"propertyType,omitempty"`
	PropertyVersions map[string]PropertyType  `json:"propertyVersions,omitempty"`
	LinkType         *LinkType                `json:"linkType,omitempty"`
	LinkVersions     map[string]LinkType      `json:"linkVersions,omitempty"`
	Ref              AssetRef                 `json:"ref"`
	Source           string                   `json:"source"`  // code, until published definitions exist
	Version          string                   `json:"version"` // installed app manifest version, not a published revision
	ContractVersion  int                      `json:"contractVersion"`
	Requires         []AssetRef               `json:"requires"`
	Provides         []Capability             `json:"provides"`
	Entity           *EntityInfo              `json:"entity,omitempty"`
	Action           *Action                  `json:"action,omitempty"`
	Page             *Page                    `json:"page,omitempty"`
	Application      *Application             `json:"application,omitempty"`
	Query            *NamedQuery              `json:"query,omitempty"`
	QueryVersions    map[string]NamedQuery    `json:"queryVersions,omitempty"`
	Flow             *FlowDefinition          `json:"flow,omitempty"`
	Function         *AIFunction              `json:"function,omitempty"`
	Evaluation       *EvaluationSetDescriptor `json:"evaluation,omitempty"`
	TestPlan         *TestPlanDescriptor      `json:"testPlan,omitempty"`
	Agent            *AgentReleaseDescriptor  `json:"agent,omitempty"`
	Operation        *Operation               `json:"operation,omitempty"`
}

// ApplicationVisibility is the original member-facing metadata projection.
// Binding labels do not confer permission to open another application.
type ApplicationVisibility interface{ VisibleApplications(Caller) []AssetRef }

func (c Caller) VisibleApplications() []AssetRef {
	if rt, ok := c.rt.(ApplicationVisibility); ok {
		return rt.VisibleApplications(c)
	}
	return nil
}

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
	InstallQuery(c Caller, q NamedQuery, version int) error
	Interfaces() []Interface
	Member(id string) (Member, bool)
	Operation(app, name string, version int) (Operation, int, bool)
	RuleQuery(app, name string, version int) (NamedQuery, int, bool)
	BuildProcesses() ProcessDefinitions
	Record(ref string) (any, bool)
	ResolvePropertyType(binding AssetBinding) (PropertyType, bool)
	ValidateInstallApplication(a Application) error
	ValidateInstallDependents(c Caller, e Entity, actions []Action, generatedPage string, pages ...Page) error
	ValidateInstallFunction(f AIFunction) error
	ValidateInstallLinkType(l LinkType) error
	ValidateInstallOperation(c Caller, o Operation) error
	ValidateInstallPage(p Page) error
	ValidateInstallPropertyType(p PropertyType) error
	ValidateInstallQuery(q NamedQuery) error
	ValidateCodeAsset(ReleaseAsset) error
}

type RecordSetHost interface {
	ValidateRecordSet(EntityInfo, RecordSet) error
}

// ModelPageHost checks a publisher's model reads through the original member
// projection. It adds no installation or business-decision authority.

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

// ApplicationAI is a projection of the original function, action, evaluation
// and usage owners. It carries no prompts, grants, quotas or second AI state.
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

// ApplicationRun is a projection of original authorized runtime reads. Its
// relationship describes resource use, never exclusive ownership by an app.
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

// ApplicationPage is one page of an application, as its navigation lists it.
type ApplicationPage struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Object      string `json:"object,omitempty"`
}

// ApplicationView is one application the caller may open: its identity, how it
// came to the tenant, and the pages it offers them.
type ApplicationView struct {
	App         string            `json:"app"`  // the owner whose registry holds it
	Name        string            `json:"name"` // the application's own name
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	Source      string            `json:"source"` // code | tenant
	Pages       []ApplicationPage `json:"pages"`
}

// PageView is one page of an application resolved for the caller: the
// declaration, the object it runs on with the caller's own field projection,
// and the actions of that page the caller may take.
type PageView struct {
	Application ApplicationView `json:"application"`
	Page        Page            `json:"page"`
	Entity      *EntityInfo     `json:"entity,omitempty"`
	Actions     []Action        `json:"actions"`
}

// ApplicationProfile is a declared operating envelope, not a performance or
// recovery guarantee. Original owners retain and recover their own data.
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

// ApplicationArtifact is the original release owner's closed candidate rooted
// at one application. Bindings carry immutable owner versions; no second
// publication, asset registry or runtime state is kept here.
type ApplicationArtifact struct {
	Samples      []AssetBinding       `json:"samples"`
	Ref          AssetRef             `json:"ref"`
	Version      string               `json:"version"`
	Title        string               `json:"title"`
	Profile      *ApplicationProfile  `json:"profile,omitempty"`
	Model        []AssetBinding       `json:"model"`
	Rules        []AssetBinding       `json:"rules"`
	Pages        []AssetBinding       `json:"pages"`
	Flows        []AssetBinding       `json:"flows"`
	AI           []AssetBinding       `json:"ai"`
	Integrations []AssetBinding       `json:"integrations"`
	Roles        []ApplicationRole    `json:"roles"`
	Depends      []AssetBinding       `json:"depends"`
	Environment  []ApplicationBinding `json:"environment"`
}

// Roles project the frozen owners' declarations; they never create grants.
type ApplicationRole struct {
	Resource    AssetBinding `json:"resource"`
	Role        string       `json:"role"`
	Scope       string       `json:"scope,omitempty"`
	ReadFields  []string     `json:"readFields,omitempty"`
	WriteFields []string     `json:"writeFields,omitempty"`
}

// A connection's local address, credentials and readiness stay outside the
// artifact; it declares only the owner identity and required connection kind.
type ApplicationBinding struct {
	Ref        AssetRef `json:"ref"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}

func ArtifactOfApplication(root AssetRef, available []ReleaseAsset) (ApplicationArtifact, error) {
	artifact := ApplicationArtifact{Ref: root, Model: []AssetBinding{}, Rules: []AssetBinding{}, Pages: []AssetBinding{}, Flows: []AssetBinding{}, AI: []AssetBinding{}, Integrations: []AssetBinding{}, Samples: []AssetBinding{}, Roles: []ApplicationRole{}, Depends: []AssetBinding{}, Environment: []ApplicationBinding{}}
	if root.Kind != AssetApp {
		return artifact, fmt.Errorf("an application artifact needs its application root")
	}
	candidate, err := Candidate([]AssetRef{root}, available)
	if err != nil {
		return artifact, err
	}
	artifact.Version = candidate.ID
	for _, asset := range candidate.Assets {
		if asset.Ref == root {
			var declaration Application
			if err := json.Unmarshal(asset.Body, &declaration); err != nil {
				return artifact, err
			}
			if err := declaration.CheckResources(); err != nil {
				return artifact, err
			}
			artifact.Title, artifact.Profile = declaration.Title, declaration.Profile
			continue
		}
		binding := AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}
		roles, err := applicationAssetRoles(asset)
		if err != nil {
			return artifact, err
		}
		artifact.Roles = append(artifact.Roles, roles...)
		switch asset.Ref.Kind {
		case AssetObject, AssetLinkType, AssetPropertyType, AssetQuery:
			artifact.Model = append(artifact.Model, binding)
		case AssetAction:
			artifact.Rules = append(artifact.Rules, binding)
		case AssetPage:
			artifact.Pages = append(artifact.Pages, binding)
		case AssetFlow:
			artifact.Flows = append(artifact.Flows, binding)
		case AssetFunction, AssetAgent, AssetEvaluation:
			artifact.AI = append(artifact.AI, binding)
		case AssetConnection, AssetSource, AssetDataset, AssetPipeline, AssetWriteback, AssetMatching, AssetObservation:
			artifact.Integrations = append(artifact.Integrations, binding)
		case AssetSampleSet, AssetTestPlan:
			artifact.Samples = append(artifact.Samples, binding)
		case AssetCompute, AssetAlert:
			artifact.Rules = append(artifact.Rules, binding)
		}
		if asset.Ref.Kind == AssetConnection {
			var contract struct {
				Kind       string `json:"kind"`
				Credential bool   `json:"credential"`
			}
			if err := json.Unmarshal(asset.Body, &contract); err != nil {
				return artifact, err
			}
			artifact.Environment = append(artifact.Environment, ApplicationBinding{Ref: asset.Ref, Kind: contract.Kind, Credential: contract.Credential})
		}
		if asset.Ref.Kind == AssetEvaluation {
			set, err := ReadEvaluationSet(asset.Body)
			if err != nil {
				return artifact, err
			}
			artifact.Environment = append(artifact.Environment, ApplicationBinding{Ref: asset.Ref, Kind: "ai-model", Name: set.Model})
		}
		if asset.Ref.App != root.App {
			artifact.Depends = append(artifact.Depends, binding)
		}
	}
	if artifact.Profile != nil {
		if err := checkApplicationProfileClosure(root, candidate.Assets); err != nil {
			return artifact, err
		}
	}
	return artifact, nil
}

func applicationAssetRoles(asset ReleaseAsset) ([]ApplicationRole, error) {
	var descriptor struct {
		Entity EntityInfo `json:"entity"`
		Access []struct {
			Role string `json:"role"`
			Read string `json:"read"`
		} `json:"access"`
		Fields      json.RawMessage `json:"fields"`
		Owners      []string        `json:"owners"`
		HandoffRole string          `json:"handoffRole"`
		Definition  struct {
			Owners []string `json:"owners"`
		} `json:"definition"`
		Scope struct {
			Levels map[string]string `json:"levels"`
		} `json:"scope"`
		Standard  struct{ Roles []string }                         `json:"standard"`
		Roles     []string                                         `json:"roles"`
		Action    Action                                           `json:"action"`
		Lifecycle struct{ Transitions []struct{ Roles []string } } `json:"lifecycle"`
	}
	if !slices.Contains([]AssetKind{AssetObject, AssetAction, AssetFunction, AssetCompute, AssetFlow, AssetAgent, AssetObservation}, asset.Ref.Kind) {
		return nil, nil
	}
	if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
		return nil, err
	}
	roles := map[string]*ApplicationRole{}
	add := func(role string) *ApplicationRole {
		if value := roles[role]; value != nil {
			return value
		}
		value := &ApplicationRole{Resource: AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Role: role}
		roles[role] = value
		return value
	}
	for _, access := range descriptor.Access {
		add(access.Role).Scope = access.Read
	}
	for _, role := range append(slices.Clone(descriptor.Owners), descriptor.Definition.Owners...) {
		add(role)
	}
	if descriptor.HandoffRole != "" {
		add(descriptor.HandoffRole)
	}
	for role, scope := range descriptor.Scope.Levels {
		add(role).Scope = scope
	}
	for _, role := range append(append(slices.Clone(descriptor.Roles), descriptor.Standard.Roles...), descriptor.Action.Roles...) {
		add(role)
	}
	for _, transition := range descriptor.Lifecycle.Transitions {
		for _, role := range transition.Roles {
			add(role)
		}
	}
	var fields []FieldInfo
	if asset.Ref.Kind == AssetObject {
		info, err := queryObjectDescriptor(asset.Body)
		if err != nil {
			return nil, err
		}
		fields = info.Fields
	}
	for _, field := range fields {
		for _, role := range field.Read {
			value := add(role)
			value.ReadFields = append(value.ReadFields, field.Name)
		}
		for _, role := range field.Write {
			value := add(role)
			value.WriteFields = append(value.WriteFields, field.Name)
		}
	}
	out := make([]ApplicationRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, *role)
	}
	slices.SortFunc(out, func(a, b ApplicationRole) int { return strings.Compare(a.Role, b.Role) })
	return out, nil
}

func checkApplicationProfileClosure(root AssetRef, assets []ReleaseAsset) error {
	lookup := map[AssetRef]ReleaseAsset{}
	for _, asset := range assets {
		lookup[asset.Ref] = asset
	}
	var application Application
	if err := json.Unmarshal(lookup[root].Body, &application); err != nil {
		return err
	}
	if application.Profile == nil {
		return nil
	}
	objects := map[string]bool{}
	seen := map[AssetRef]bool{}
	var visit func(AssetRef)
	visit = func(ref AssetRef) {
		if seen[ref] {
			return
		}
		seen[ref] = true
		asset, exists := lookup[ref]
		if !exists {
			return
		}
		if ref.Kind == AssetObject {
			objects[ref.Name] = true
		}
		for _, dependency := range asset.Requires {
			visit(dependency)
		}
	}
	visit(root)
	for typ := range application.Profile.ExpectedRecords {
		if !objects[typ] {
			return fmt.Errorf("expected record type %s is outside the application artifact", typ)
		}
	}
	return nil
}

// AppConcept is a builder-named business concept in the application studio.
// Its stable ID survives renames; its assets retain their original identity.
type AppConcept struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Assets []AssetRef `json:"assets"`
}

// CheckConcepts validates navigation metadata, independently of compilation.
// A historical group can still name a removed member: only current application
// members are drawn, so removing membership never requires rewriting old groups.
func (a Application) CheckConcepts() error {
	if len(a.Concepts) > 100 {
		return fmt.Errorf("application concepts exceed 100 groups")
	}
	ids, assets := map[string]bool{}, map[AssetRef]bool{}
	count := 0
	for _, concept := range a.Concepts {
		if strings.TrimSpace(concept.ID) == "" || len(concept.ID) > 128 || ids[concept.ID] {
			return fmt.Errorf("application concepts need unique nonempty IDs of at most 128 bytes")
		}
		ids[concept.ID] = true
		if strings.TrimSpace(concept.Title) == "" || len(concept.Title) > 512 {
			return fmt.Errorf("application concepts need a title of at most 512 bytes")
		}
		for _, ref := range concept.Assets {
			if err := ref.Check(); err != nil {
				return err
			}
			if ref.Kind == AssetApp || assets[ref] {
				return fmt.Errorf("concept assets must be unique resources, not application roots")
			}
			assets[ref] = true
			count++
		}
	}
	if count > 1000 {
		return fmt.Errorf("application concepts exceed 1000 grouped assets")
	}
	return nil
}

// CheckFilterSchema reads the original field descriptors, including the member
// projection used by discovery. It introduces no alternative query language.
func CheckFilterSchema(source PageResourceSource, object EntityInfo) error {
	if source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App {
		return fmt.Errorf("filter object is unavailable")
	}
	for _, name := range source.Fields {
		field, ok := object.Field(name)
		if !ok || field.Type != "choice" && field.Type != "boolean" && field.Type != "reference" {
			return fmt.Errorf("filter field %s is unavailable or unsupported", name)
		}
	}
	return nil
}

type ApplicationHeader struct {
	Variant   string                  `json:"variant"`
	Title     string                  `json:"title"`
	Logo      string                  `json:"logo,omitempty"`
	Collapsed bool                    `json:"collapsed,omitempty"`
	Items     []ApplicationHeaderItem `json:"items"`
}
type ApplicationHeaderItem struct {
	Kind   string   `json:"kind"`
	Pages  []string `json:"pages,omitempty"`
	Label  string   `json:"label,omitempty"`
	Text   string   `json:"text,omitempty"`
	Action string   `json:"action,omitempty"`
}

// CheckHeader checks source bounds and declared navigation references. Layout
// and appearance belong to the shared Web authoring/runtime validator.
func (a Application) CheckHeader() error {
	h := a.Header
	if h == nil {
		return nil
	}
	if !PageUIProfileSupports(a.UIProfile, "platform.page.v2.105") || len(h.Title) > 1024 || len(h.Items) == 0 || len(h.Items) > 24 {
		return fmt.Errorf("application header exceeds its retained profile or content bound")
	}
	if h.Logo != "" && !ValidPageImageURL(h.Logo) {
		return fmt.Errorf("application header logo needs a safe static image URL")
	}
	seen := map[string]bool{}
	for _, item := range h.Items {
		if len(item.Label) > 1024 || len(item.Text) > 1024 || len(item.Pages) > 32 {
			return fmt.Errorf("application header item exceeds its budget")
		}
		for _, name := range item.Pages {
			if !slices.Contains(a.Pages, name) || seen[name] {
				return fmt.Errorf("application header navigation needs unique application pages")
			}
			seen[name] = true
		}
	}
	return nil
}

func (a Application) VisibleHeader() *ApplicationHeader {
	if a.Header == nil {
		return nil
	}
	h := *a.Header
	h.Items = nil
	for _, item := range a.Header.Items {
		item.Pages = slices.DeleteFunc(slices.Clone(item.Pages), func(p string) bool { return !slices.Contains(a.Pages, p) })
		if item.Kind == "tabs" && len(item.Pages) == 0 {
			continue
		}
		h.Items = append(h.Items, item)
	}
	return &h
}

func (v PageVariable) IsWritable() bool { return v.Mode == "state" || v.Mode == "shared" && v.Writable }

func (a Application) CheckVariables() error {
	if a.UIProfile != "" && !SupportsPageUIProfile(a.UIProfile) {
		return fmt.Errorf("unsupported application UI profile %s", a.UIProfile)
	}
	if len(a.Variables) == 0 && len(a.Queries) == 0 {
		return nil
	}
	if !PageUIProfileSupports(a.UIProfile, "platform.page.v2.8") {
		return fmt.Errorf("application variables require UI profile v2.8")
	}
	if (len(a.Queries) > 0 || a.hasResources()) && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.12") {
		return fmt.Errorf("application query resources require v2.12")
	}
	for id, v := range a.Variables {
		if v.Mode == "property" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.17") {
			return fmt.Errorf("application property %s requires v2.17", id)
		}
		if v.Type == "decimal" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.16") {
			return fmt.Errorf("application decimal %s requires v2.16", id)
		}
		if v.Type == "filter" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.15") {
			return fmt.Errorf("application filter %s requires v2.15", id)
		}
		if v.Type == "record-set" && !PageUIProfileSupports(a.UIProfile, pageWidgets.Runtime.RecordSelection.SharedUIProfile) {
			return fmt.Errorf("application record-set %s requires its shared profile", id)
		}
		if v.Type == "record" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.14") {
			return fmt.Errorf("application record %s requires v2.14", id)
		}
		if v.Scope != "application" || !slices.Contains(pageWidgets.Runtime.Application.Modes, v.Mode) {
			return fmt.Errorf("application variable %s needs its own pure scalar declaration", id)
		}
	}
	d := a.QueryPage().InputSchema
	if err := d.CheckApplicationQueries(); err != nil {
		return err
	}
	return d.CheckVariables()
}

// Shared bindings are typed requirements on the application which holds the
// page. They introduce no reverse membership edge or permission grant.
func (a Application) CheckPageVariables(p Page) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	for id, v := range safe.InputSchema.Variables {
		if v.Scope != "application" {
			continue
		}
		if v.Source == nil {
			return fmt.Errorf("page %s shared binding %s has no source", p.Name, id)
		}
		source, ok := a.Variables[v.Source.Variable]
		matches := true
		if v.Type == "object-set" {
			matches = source.Mode == "resource" && source.Source != nil && source.Source.Kind == "plan" && v.Source.Object != nil && a.Queries[source.Source.Query].Object == *v.Source.Object
		}
		if v.Type == "record" || v.Type == "filter" || v.Type == "record-set" {
			matches = source.Mode == "resource" && source.Source != nil && source.Source.Kind == v.Type && source.Source.Object != nil && v.Source.Object != nil && *source.Source.Object == *v.Source.Object
		}
		if !ok || !matches || source.Type != v.Type || v.Writable && source.Mode != "state" && v.Type != "record" && v.Type != "filter" && v.Type != "record-set" {
			return fmt.Errorf("application %s does not satisfy page %s shared binding %s", a.Name, p.Name, id)
		}
		for _, binding := range safe.DataBindings {
			if binding.Ports["filterVariable"] != id {
				continue
			}
			for _, field := range binding.Fields {
				if source.Source == nil || !slices.Contains(source.Source.Fields, field.Key()) {
					return fmt.Errorf("page filter field is not allowed by application")
				}
			}
		}

	}
	return nil
}

// QueryPage adapts application declarations to the shared schema checker; no
// page state, navigation or layout is created for execution.
func (a Application) QueryPage() Page {
	return Page{Format: 3, InputSchema: PageInputSchema{profile: a.UIProfile, Variables: a.Variables, Queries: a.Queries}}
}
func (a Application) hasResources() bool {
	for _, v := range a.Variables {
		if v.Mode == "resource" {
			return true
		}
	}
	return false
}

// Binding is a typed data reference, never executable source text. Control
// edges determine scheduling; bindings only read values already in scope.
type Binding struct {
	Source string          `json:"source" enum:"literal,input,subject,step,item,index,answer"`
	Step   string          `json:"step,omitempty"`
	Path   []string        `json:"path,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

func (b Binding) Check() error {
	if !slices.Contains([]string{"literal", "input", "subject", "step", "item", "index", "answer"}, b.Source) || len(b.Path) > 16 || len(b.Value) > 64<<10 {
		return fmt.Errorf("binding needs a supported source and bounded path/value")
	}
	if b.Source == "literal" {
		v, err := DecodeValue(b.Value, 64<<10)
		if err != nil {
			return fmt.Errorf("literal binding: %w", err)
		}
		if err := boundedPredicateValue(v); err != nil {
			return err
		}
	}
	if b.Source == "literal" && (!json.Valid(b.Value) || len(b.Path) > 0 || b.Step != "") {
		return fmt.Errorf("literal binding needs one JSON value")
	}
	if b.Source == "step" && b.Step == "" {
		return fmt.Errorf("step binding needs a node")
	}
	if b.Source != "literal" && len(b.Value) > 0 || b.Source != "step" && b.Step != "" {
		return fmt.Errorf("binding configuration does not match its source")
	}
	for _, part := range b.Path {
		if part == "" || len(part) > 128 || strings.Contains(part, "\x00") {
			return fmt.Errorf("binding path needs bounded field names")
		}
	}
	return nil
}

// Resolve reads the current token's durable frame. The definition owner must
// supply a permission-filtered subject projection, not a private record.
func (b Binding) Resolve(r *Run, subject json.RawMessage) (json.RawMessage, error) {
	if err := b.Check(); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	switch b.Source {
	case "literal":
		raw = b.Value
	case "input":
		raw = r.Data
	case "subject":
		raw = subject
	case "step":
		raw = r.Outputs[b.Step]
	case "item":
		if len(r.Frames) > 0 {
			raw = r.Frames[len(r.Frames)-1].Item
		}
	case "index":
		if len(r.Frames) > 0 {
			raw, _ = json.Marshal(r.Frames[len(r.Frames)-1].Index)
		}
	case "answer":
		raw, _ = json.Marshal(r.Answer)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("binding source %s is unavailable in this scope", b.Source)
	}
	for _, part := range b.Path {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, fmt.Errorf("binding path %s does not name an object field", strings.Join(b.Path, "."))
		}
		value, ok := object[part]
		if !ok {
			return nil, fmt.Errorf("binding field %s is unavailable", part)
		}
		raw = value
	}
	return slices.Clone(raw), nil
}

// Predicate keeps conditional meaning in structured definitions. Numeric
// comparison uses exact JSON decimals and never JavaScript coercion.
type Predicate struct {
	Op    string      `json:"op" enum:"eq,neq,gt,gte,lt,lte,contains,exists,all,any,not"`
	Left  *Binding    `json:"left,omitempty"`
	Right *Binding    `json:"right,omitempty"`
	Terms []Predicate `json:"terms,omitempty"`
}

func (p Predicate) Check() error { return p.check(0) }
func (p Predicate) check(depth int) error {
	if depth > 16 {
		return fmt.Errorf("predicate nesting exceeds its bound")
	}
	switch p.Op {
	case "all", "any", "not":
		if p.Left != nil || p.Right != nil || len(p.Terms) == 0 || len(p.Terms) > 32 || p.Op == "not" && len(p.Terms) != 1 {
			return fmt.Errorf("logical predicate needs bounded terms")
		}
		for _, term := range p.Terms {
			if err := term.check(depth + 1); err != nil {
				return err
			}
		}
	case "eq", "neq", "gt", "gte", "lt", "lte", "contains", "exists":
		if p.Left == nil || len(p.Terms) > 0 || p.Op != "exists" && p.Right == nil || p.Op == "exists" && p.Right != nil {
			return fmt.Errorf("predicate needs matching operands")
		}
		if err := p.Left.Check(); err != nil {
			return err
		}
		if p.Right != nil {
			return p.Right.Check()
		}
	default:
		return fmt.Errorf("unknown predicate operator %s", p.Op)
	}
	return nil
}
func (p Predicate) Test(r *Run, subject json.RawMessage) (bool, error) {
	if err := p.Check(); err != nil {
		return false, err
	}
	switch p.Op {
	case "all", "any", "not":
		result := p.Op == "all"
		for _, term := range p.Terms {
			v, err := term.Test(r, subject)
			if err != nil {
				return false, err
			}
			if p.Op == "not" {
				return !v, nil
			}
			if p.Op == "all" && !v {
				return false, nil
			}
			if p.Op == "any" && v {
				return true, nil
			}
		}
		return result, nil
	}
	left, err := p.Left.Resolve(r, subject)
	if p.Op == "exists" {
		return err == nil && !bytes.Equal(bytes.TrimSpace(left), []byte("null")), nil
	}
	if err != nil {
		return false, err
	}
	right, err := p.Right.Resolve(r, subject)
	if err != nil {
		return false, err
	}
	var a, b any
	da, db := json.NewDecoder(bytes.NewReader(left)), json.NewDecoder(bytes.NewReader(right))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&a) != nil || db.Decode(&b) != nil {
		return false, fmt.Errorf("predicate operands are invalid JSON")
	}
	if err := boundedPredicateValue(a); err != nil {
		return false, err
	}
	if err := boundedPredicateValue(b); err != nil {
		return false, err
	}
	if p.Op == "eq" || p.Op == "neq" {
		equal := jsonEqual(a, b)
		return equal == (p.Op == "eq"), nil
	}
	if p.Op == "contains" {
		switch x := a.(type) {
		case string:
			y, ok := b.(string)
			if !ok {
				return false, fmt.Errorf("contains needs two strings or an array")
			}
			return strings.Contains(x, y), nil
		case []any:
			for _, v := range x {
				if jsonEqual(v, b) {
					return true, nil
				}
			}
			return false, nil
		}
		return false, fmt.Errorf("contains needs a string or array")
	}
	var comparison int
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false, fmt.Errorf("numeric comparison needs two numbers")
		}
		an, aok := new(big.Rat).SetString(string(x))
		bn, bok := new(big.Rat).SetString(string(y))
		if !aok || !bok {
			return false, fmt.Errorf("invalid decimal comparison")
		}
		comparison = an.Cmp(bn)
	case string:
		y, ok := b.(string)
		if !ok {
			return false, fmt.Errorf("text comparison needs two strings")
		}
		comparison = strings.Compare(x, y)
	default:
		return false, fmt.Errorf("ordered comparison needs numbers or strings")
	}
	switch p.Op {
	case "gt":
		return comparison > 0, nil
	case "gte":
		return comparison >= 0, nil
	case "lt":
		return comparison < 0, nil
	case "lte":
		return comparison <= 0, nil
	}
	return false, nil
}
func jsonEqual(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		an, ao := new(big.Rat).SetString(string(x))
		bn, bo := new(big.Rat).SetString(string(y))
		return ao && bo && an.Cmp(bn) == 0
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Exact decimal comparison has an explicit arithmetic budget. In particular,
// a short JSON string such as 1e99999999 must not allocate an enormous Rat.
func boundedPredicateValue(v any) error {
	switch value := v.(type) {
	case json.Number:
		text := string(value)
		if len(text) > 128 {
			return fmt.Errorf("predicate number exceeds 128 digits")
		}
		if index := strings.IndexAny(text, "eE"); index >= 0 {
			exponent := text[index+1:]
			if len(exponent) > 5 {
				return fmt.Errorf("predicate exponent exceeds its arithmetic bound")
			}
			n, err := strconv.Atoi(exponent)
			if err != nil || n < -308 || n > 308 {
				return fmt.Errorf("predicate exponent exceeds its arithmetic bound")
			}
		}
	case []any:
		for _, item := range value {
			if err := boundedPredicateValue(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := boundedPredicateValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// CapabilityDescriptor is a projection of an existing owner declaration for
// Logic Studio and tools, never a second registry or an execution definition.
type CapabilityDescriptor struct {
	Ref         AssetRef     `json:"ref"`
	Kind        string       `json:"kind"`
	Side        string       `json:"side"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Group       string       `json:"group"`
	Icon        string       `json:"icon"`
	Tone        string       `json:"tone"`
	Source      string       `json:"source"`
	Version     string       `json:"version"`
	Revision    int          `json:"revision,omitempty"`
	Target      string       `json:"target,omitempty"`
	Input       *ValueSchema `json:"input,omitempty"`
	Output      *ValueSchema `json:"output,omitempty"`
	Config      *ValueSchema `json:"config,omitempty"`
	// Parameters retains the owner's original fields when an opaque JSON
	// payload has no structural schema. It must not be advertised as text.
	Parameters []Field     `json:"parameters,omitempty"`
	Effects    []string    `json:"effects"`
	Ports      []BlockPort `json:"ports"`
}

type BlockPort struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Direction string `json:"direction"`
	Channel   string `json:"channel"`
	Type      string `json:"type"`
}

// ControlBlocks describes the built-in Flow grammar to authoring clients. The
// flow compiler remains the owner of each kind's config and execution rules.
func ControlBlocks() []CapabilityDescriptor {
	kinds := []struct{ name, title, description, group, icon, tone string }{
		{"payload", "Input", "Start with the workflow's typed input.", "Flow", "log-in", "neutral"},
		{"transform", "Transform", "Bind fields and constants into an output object.", "Data", "braces", "info"},
		{"branch", "Branch", "Choose a true or false control path.", "Logic", "git-branch", "warning"},
		{"switch", "Switch", "Choose a named case or the default path.", "Logic", "split", "warning"},
		{"foreach", "For each", "Run a bounded body over a collection.", "Collections", "repeat", "info"},
		{"while", "While", "Repeat a scoped body within an explicit limit.", "Collections", "repeat", "info"},
		{"fork", "Parallel paths", "Start explicit paths and wait for all or the first result.", "Logic", "split", "info"},
		{"join", "Join", "Complete the current declared parallel scope.", "Logic", "merge", "info"},
		{"ask", "Human task", "Wait for a governed human answer in the existing inbox.", "People", "user-round", "warning"},
		{"wait", "Wait", "Suspend on the platform's durable timer.", "Flow", "clock", "neutral"},
		{"subflow", "Run workflow", "Call a retained native workflow.", "Flow", "workflow", "info"},
		{"break", "Break", "Finish the enclosing loop and cancel its pending iterations.", "Collections", "circle-stop", "warning"},
		{"continue", "Continue", "Finish this iteration and move to the next item.", "Collections", "step-forward", "info"},
		{"end", "Return", "Finish with an explicit output value.", "Flow", "log-out", "success"},
		{"fail", "Fail", "Stop with a clear failure.", "Flow", "circle-alert", "danger"},
	}
	out := make([]CapabilityDescriptor, 0, len(kinds))
	for _, k := range kinds {
		ports := []BlockPort{{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"}, {ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"}}
		if k.name == "payload" {
			ports = ports[1:]
		}
		if k.name == "end" || k.name == "fail" || k.name == "break" || k.name == "continue" {
			ports = ports[:1]
		}
		if k.name == "branch" {
			ports = append(ports[:1], BlockPort{ID: "true", Title: "True", Direction: "output", Channel: "control", Type: "flow"}, BlockPort{ID: "false", Title: "False", Direction: "output", Channel: "control", Type: "flow"})
		}
		if k.name == "foreach" || k.name == "while" {
			ports = append(ports[:1], BlockPort{ID: "body", Title: "Loop", Direction: "output", Channel: "control", Type: "flow"}, BlockPort{ID: "next", Title: "Done", Direction: "output", Channel: "control", Type: "flow"})
		}
		out = append(out, CapabilityDescriptor{Ref: AssetRef{App: "flow", Kind: AssetKind("control"), Name: k.name}, Kind: k.name, Title: k.title, Description: k.description, Group: k.group, Icon: k.icon, Tone: k.tone, Source: "platform", Version: "1", Effects: []string{}, Ports: ports})
	}
	return out
}

// ProposalPolicy places a typed function's answer in the original review path.
// A nil policy remains read-only advice. It never grants a business action.
type ProposalPolicy struct {
	HandoffRole         string  `json:"handoffRole,omitempty"`
	ConfidenceField     string  `json:"confidenceField,omitempty"`
	MinimumConfidence   float64 `json:"minimumConfidence,omitempty"`
	ExpiresAfterSeconds int     `json:"expiresAfterSeconds"`
}

func (p ProposalPolicy) Check(output []Field) error {
	if p.ExpiresAfterSeconds < 1 || p.ExpiresAfterSeconds > 604800 || math.IsNaN(p.MinimumConfidence) || math.IsInf(p.MinimumConfidence, 0) || p.MinimumConfidence < 0 || p.MinimumConfidence > 1 {
		return fmt.Errorf("Proposal review needs an expiry of 1–604800 seconds and bounded confidence")
	}
	if p.ConfidenceField != "" {
		i := slices.IndexFunc(output, func(f Field) bool { return f.Name == p.ConfidenceField })
		if i < 0 || !output[i].Required || (output[i].Type != "decimal" && output[i].Type != "integer") || p.HandoffRole == "" {
			return fmt.Errorf("Confidence handover needs a required numeric output and reviewer role")
		}
	}
	if p.ConfidenceField == "" && p.MinimumConfidence != 0 {
		return fmt.Errorf("Choose a confidence field before setting its threshold")
	}
	return nil
}

// ProposalAdoption consumes a retained call record. This is not a function
// invocation: the immutable contract and version travel with that record.
type ProposalAdoption struct {
	Function FunctionRef     `json:"function"`
	Input    string          `json:"input"`
	Mapping  []ProposalInput `json:"mapping"`
}
type ProposalInput struct {
	Output string `json:"output"`
	Input  string `json:"input"`
}

func (p ProposalAdoption) Check() error {
	if !functionName.MatchString(p.Function.Name) || p.Function.Version < 0 || p.Function.Version > 64 || !functionFieldName.MatchString(p.Input) || len(p.Mapping) == 0 || len(p.Mapping) > 16 {
		return fmt.Errorf("Adoption needs a function, proposal input and 1–16 output mappings")
	}
	outputs, inputs := map[string]bool{}, map[string]bool{}
	for _, m := range p.Mapping {
		if !functionFieldName.MatchString(m.Output) || !functionFieldName.MatchString(m.Input) || m.Input == p.Input || outputs[m.Output] || inputs[m.Input] {
			return fmt.Errorf("Proposal output and action input mappings must be unique")
		}
		outputs[m.Output], inputs[m.Input] = true, true
	}
	return nil
}

// A trial may produce proposals, but cannot execute their adoption.
type SimulationRuntime interface{ Simulating() bool }

func (c Caller) Simulating() bool { r, ok := c.rt.(SimulationRuntime); return ok && r.Simulating() }

func (p ProposalAdoption) CheckMapping(f AIFunction, inputs []Field) error {
	if err := p.Check(); err != nil {
		return err
	}
	if f.Proposal == nil || f.Name != p.Function.Name {
		return fmt.Errorf("Adoption needs a declared business proposal function")
	}
	for _, m := range p.Mapping {
		output := slices.IndexFunc(f.Output, func(field Field) bool { return field.Name == m.Output })
		input := slices.IndexFunc(inputs, func(field Field) bool { return field.Name == m.Input })
		if output < 0 || input < 0 || !(f.Output[output].Type == inputs[input].Type || f.Output[output].Type == "decimal" && inputs[input].Type == "number") {
			return fmt.Errorf("Proposal mapping types do not match the business action")
		}
	}
	return nil
}
