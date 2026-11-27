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
		AssetConnection, AssetSource, AssetListener, AssetDataset, AssetMediaSet, AssetSchedule, AssetPipeline, AssetWriteback, AssetMatching, AssetAgent, AssetEvaluation, AssetTestPlan, AssetSampleSet, AssetProtocol, AssetInterface, AssetObservation, AssetObjectTypeGroup, AssetProduct, "contract", "implementation"}, r.Kind) {
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
	Surface      string                       `json:"surface,omitempty"`
	Pages        []string                     `json:"pages"`
	Groups       []AppGroup                   `json:"groups,omitempty"`
	Dashboard    json.RawMessage              `json:"dashboard,omitempty"`
	Resources    []AssetRef                   `json:"resources,omitempty"`
	Concepts     []AppConcept                 `json:"concepts,omitempty"`
}

type ApplicationPageReplacement struct {
	View string `json:"view"`
	Page string `json:"page"`
}
type AppMenuEntry struct {
	Kind   string `json:"kind,omitempty"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	Page   string `json:"page,omitempty"`
	Object string `json:"object,omitempty"`
	Report string `json:"report,omitempty"`
}

type AppGroup struct {
	ID        string         `json:"id,omitempty"`
	Title     string         `json:"title"`
	Roles     []string       `json:"roles,omitempty"`
	Pages     []string       `json:"pages,omitempty"`
	Entries   []AppMenuEntry `json:"entries,omitempty"`
	Groups    []AppGroup     `json:"groups,omitempty"`
	Collapsed bool           `json:"collapsed,omitempty"`
	Layout    string         `json:"layout,omitempty"`
}

func IconName(s string) bool {
	return s != "" && len(s) <= 32 && s[0] != '-' && s[len(s)-1] != '-' && !strings.Contains(s, "--") &&
		strings.IndexFunc(s, func(r rune) bool { return r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') }) < 0
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
	Entity             *RecordTypeInfo            `json:"entity,omitempty"`
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
