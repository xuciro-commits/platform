package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type AssetKind string

const (
	AssetObject          AssetKind = "object"
	AssetObjectTypeGroup AssetKind = "object-type-group"
	AssetLinkType        AssetKind = "link-type"
	AssetPropertyType    AssetKind = "property-type"
	AssetValueType       AssetKind = "value-type"
	AssetAction          AssetKind = "action"
	AssetPage            AssetKind = "page"
	AssetApp             AssetKind = "app"
	AssetQuery           AssetKind = "query"
	AssetFlow            AssetKind = "flow"
	AssetFunction        AssetKind = "function"
	AssetAgent           AssetKind = "agent"
	AssetEvaluation      AssetKind = "evaluation-set"
	AssetTestPlan        AssetKind = "test-plan"
	AssetCompute         AssetKind = "compute"
	AssetConnection      AssetKind = "connection"
	AssetSource          AssetKind = "source"
	AssetListener        AssetKind = "listener"
	AssetDataset         AssetKind = "dataset"
	AssetMediaSet        AssetKind = "mediaset"
	AssetSchedule        AssetKind = "schedule"
	AssetPipeline        AssetKind = "pipeline"
	AssetWriteback       AssetKind = "writeback"
	AssetMatching        AssetKind = "matching"
	AssetSampleSet       AssetKind = "sample-set"
	AssetProtocol        AssetKind = "protocol"
	AssetInterface       AssetKind = "interface"
	AssetObservation     AssetKind = "observation"
	AssetProduct         AssetKind = "product"
)

type NamedQuery struct {
	Name            string          `json:"name"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Object          string          `json:"object"`
	Interface       string          `json:"interface,omitempty"`
	InterfaceShape  *Interface      `json:"interfaceShape,omitempty"`
	Implementations []string        `json:"implementations,omitempty"`
	By              string          `json:"by,omitempty"`
	Domain          json.RawMessage `json:"domain,omitempty"`
	Filter          *Expr           `json:"filter,omitempty"`
	Sort            []string        `json:"sort,omitempty"`
	Limit           int             `json:"limit,omitempty"`
}

const ObjectSetContractVersion = 1

func (q NamedQuery) ContractVersion() int { return ObjectSetContractVersion }

type AssetRef struct {
	App  string    `json:"app"`
	Kind AssetKind `json:"kind"`
	Name string    `json:"name"`
}

func (r AssetRef) String() string { return r.App + "/" + string(r.Kind) + "/" + r.Name }

func (r AssetRef) Check() error {
	if r.App == "" || r.Name == "" || strings.Contains(r.App, "/") || strings.Contains(r.Name, "/") || !slices.Contains([]AssetKind{AssetPropertyType, AssetValueType, AssetLinkType, AssetObject, AssetAction, AssetPage, AssetApp, AssetQuery, AssetFlow, AssetFunction, AssetCompute,
		AssetConnection, AssetSource, AssetListener, AssetDataset, AssetMediaSet, AssetSchedule, AssetPipeline, AssetWriteback, AssetMatching, AssetAgent, AssetEvaluation, AssetTestPlan, AssetSampleSet, AssetProtocol, AssetInterface, AssetObservation, AssetObjectTypeGroup, AssetProduct}, r.Kind) {
		return fmt.Errorf("asset reference %q needs an app, supported kind and name", r.String())
	}
	return nil
}

var Filterable = []string{"choice", "boolean", "reference"}

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
	Icon         string                       `json:"icon,omitempty"`
	Pages        []string                     `json:"pages"`
	Groups       []AppGroup                   `json:"groups,omitempty"`
	Resources    []AssetRef                   `json:"resources,omitempty"`
	Concepts     []AppConcept                 `json:"concepts,omitempty"`
}

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

type AppGroup struct {
	Title string   `json:"title"`
	Pages []string `json:"pages"`
}

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

type Definition struct {
	ObjectTypeGroup    *ObjectTypeGroup           `json:"objectTypeGroup,omitempty"`
	Observation        *ObservationDefinition     `json:"observation,omitempty"`
	SampleSet          *SampleSet                 `json:"sampleSet,omitempty"`
	Interface          *Interface                 `json:"interface,omitempty"`
	Protocol           *ProtocolReleaseDescriptor `json:"protocol,omitempty"`
	OwnerRecord        string                     `json:"-"`
	ContentVersion     string                     `json:"contentVersion,omitempty"`
	PropertyType       *PropertyType              `json:"propertyType,omitempty"`
	PropertyVersions   map[string]PropertyType    `json:"propertyVersions,omitempty"`
	ValueType          *ValueType                 `json:"valueType,omitempty"`
	ValueVersions      map[string]ValueType       `json:"valueVersions,omitempty"`
	LinkType           *LinkType                  `json:"linkType,omitempty"`
	LinkVersions       map[string]LinkType        `json:"linkVersions,omitempty"`
	Ref                AssetRef                   `json:"ref"`
	RID                string                     `json:"rid,omitempty"`
	OwningProject      string                     `json:"owningProject,omitempty"`
	OwningProjectTitle string                     `json:"owningProjectTitle,omitempty"`
	ManagedBy          string                     `json:"managedBy,omitempty"`
	Source             string                     `json:"source"`
	Version            string                     `json:"version"`
	ContractVersion    int                        `json:"contractVersion"`
	Requires           []AssetRef                 `json:"requires"`
	Provides           []Capability               `json:"provides"`
	Entity             *EntityInfo                `json:"entity,omitempty"`
	Action             *Action                    `json:"action,omitempty"`
	Page               *Page                      `json:"page,omitempty"`
	Application        *Application               `json:"application,omitempty"`
	Query              *NamedQuery                `json:"query,omitempty"`
	QueryVersions      map[string]NamedQuery      `json:"queryVersions,omitempty"`
	Flow               *FlowDefinition            `json:"flow,omitempty"`
	Function           *AIFunction                `json:"function,omitempty"`
	Evaluation         *EvaluationSetDescriptor   `json:"evaluation,omitempty"`
	TestPlan           *TestPlanDescriptor        `json:"testPlan,omitempty"`
	Agent              *AgentReleaseDescriptor    `json:"agent,omitempty"`
	Operation          *Operation                 `json:"operation,omitempty"`
}

func (c Caller) VisibleApplications() []AssetRef {
	if c.rt != nil {
		return c.rt.VisibleApplications(c)
	}
	return nil
}
