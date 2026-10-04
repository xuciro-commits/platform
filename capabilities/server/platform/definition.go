package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
	// and the pages it holds (ADR-0036).
	AssetApp AssetKind = "app"
	// AssetQuery is a named, pure query an app declares once (ADR-0040 21c):
	// pages and agent tools run the same declaration.
	AssetQuery AssetKind = "query"
	// AssetFlow is a versioned definition executed by the existing flow app.
	AssetFlow     AssetKind = "flow"
	AssetFunction AssetKind = "function"
	AssetCompute  AssetKind = "compute"
)

// NamedQuery reads records of one object: fixed conditions, and optionally
// the record it is run for through a reference field (By). It runs through
// the reader's own read of Object, so it never widens what anyone sees.
type NamedQuery struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Object      string          `json:"object"`
	By          string          `json:"by,omitempty"`
	Domain      json.RawMessage `json:"domain,omitempty"`
	Sort        []string        `json:"sort,omitempty"`
	Limit       int             `json:"limit,omitempty"`
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
	if r.App == "" || r.Name == "" || strings.Contains(r.App, "/") || strings.Contains(r.Name, "/") || r.Kind != AssetPropertyType && r.Kind != AssetLinkType && r.Kind != AssetObject && r.Kind != AssetAction && r.Kind != AssetPage && r.Kind != AssetApp && r.Kind != AssetQuery && r.Kind != AssetFlow && r.Kind != AssetFunction && r.Kind != AssetCompute {
		return fmt.Errorf("asset reference %q needs an app, supported kind and name", r.String())
	}
	return nil
}

// Page is the first bounded page composition: one object, a list, a detail and
// an explicit action set. It binds existing records/actions; it runs no rules.
// Its field choices shape presentation, never grant access to hidden fields.
type Page struct {
	Name         string     `json:"name"`
	Title        string     `json:"title"`
	Description  string     `json:"description,omitempty"`
	Object       AssetRef   `json:"object"`
	Layout       string     `json:"layout"` // list-detail in 13b
	ListFields   []string   `json:"listFields"`
	DetailFields []string   `json:"detailFields"`
	Actions      []AssetRef `json:"actions"`
	// Sections are the widgets a composed page is laid out from (ADR-0035);
	// with sections, Layout is "composed" and the fields above are unused.
	Sections []Section `json:"sections,omitempty"`
	// Document orders these sections in a stable, nested presentation tree.
	// The sections above remain the authoritative business bindings.
	Document *PageDocument `json:"document,omitempty"`
	// Selections are named record variables; Object fixes each record's type.
	// Sections without a binding share the original selection for their object.
	Selections []SelectionVariable `json:"selections,omitempty"`
}

type SelectionVariable struct {
	Name   string   `json:"name"`
	Object AssetRef `json:"object"`
}

// Section is one place on a composed page: a widget, what it is bound to, and
// how wide it sits (ADR-0035). A table outputs the record someone selects; a
// detail and the actions read it.
type PageFacet struct {
	Field    string `json:"field"`
	Variable string `json:"variable"`
	Kind     string `json:"kind"`
}

type PageInlineEdit struct {
	Action AssetRef `json:"action"`
	Fields []string `json:"fields"`
}

type Section struct {
	CollectionBuilder             *PageCollectionBuilder `json:"collectionBuilder,omitempty"`
	CollectionOutputVariable      string                 `json:"collectionOutputVariable,omitempty"`
	Map                           *PageRecordMap         `json:"map,omitempty"`
	Scene                         *PageSceneConfig       `json:"scene,omitempty"`
	SceneSampleCollectionVariable string                 `json:"sceneSampleCollectionVariable,omitempty"`
	SceneSampleVariable           string                 `json:"sceneSampleVariable,omitempty"`
	ScenePartVariable             string                 `json:"scenePartVariable,omitempty"`

	AI                           *PageAI                 `json:"ai,omitempty"`
	ExternalFrame                *PageExternalFrame      `json:"externalFrame,omitempty"`
	Embedding                    *PageEmbedding          `json:"embedding,omitempty"`
	Observation                  *PageObservation        `json:"observation,omitempty"`
	ObservationHistoryVariable   string                  `json:"observationHistoryVariable,omitempty"`
	ObservationContextVariable   string                  `json:"observationContextVariable,omitempty"`
	ObservationSignalVariable    string                  `json:"observationSignalVariable,omitempty"`
	ObservationThresholdVariable string                  `json:"observationThresholdVariable,omitempty"`
	ObservationRowsVariable      string                  `json:"observationRowsVariable,omitempty"`
	ObservationCountVariable     string                  `json:"observationCountVariable,omitempty"`
	ObservationMeanVariable      string                  `json:"observationMeanVariable,omitempty"`
	ActionTable                  *PageActionTable        `json:"actionTable,omitempty"`
	NotepadVariable              string                  `json:"notepadVariable,omitempty"`
	Analysis                     *PageCollectionAnalysis `json:"analysis,omitempty"`
	AnalysisXVariable            string                  `json:"analysisXVariable,omitempty"`
	AnalysisYVariable            string                  `json:"analysisYVariable,omitempty"`
	AnalysisCountVariable        string                  `json:"analysisCountVariable,omitempty"`
	AnalysisMeanVariable         string                  `json:"analysisMeanVariable,omitempty"`
	ResourceList                 *PageResourceList       `json:"resourceList,omitempty"`
	AssetDirectory               *PageAssetDirectory     `json:"assetDirectory,omitempty"`
	GraphExplorer                *PageGraphExplorer      `json:"graphExplorer,omitempty"`
	VertexGraph                  *PageVertexGraph        `json:"vertexGraph,omitempty"`
	Breadcrumb                   *PageBreadcrumb         `json:"breadcrumb,omitempty"`
	Avatar                       *PageAvatarStack        `json:"avatar,omitempty"`
	Image                        *PageStaticImage        `json:"image,omitempty"`
	HistoryLimit                 int                     `json:"historyLimit,omitempty"`
	CommentDraftVariable         string                  `json:"commentDraftVariable,omitempty"`
	FileVariable                 string                  `json:"fileVariable,omitempty"`
	PdfPageVariable              string                  `json:"pdfPageVariable,omitempty"`
	RecordGantt                  *PageRecordGantt        `json:"recordGantt,omitempty"`
	RecordCalendar               *PageRecordCalendar     `json:"recordCalendar,omitempty"`
	RecordEvents                 *PageRecordEvents       `json:"recordEvents,omitempty"`
	RecordComparison             *PageRecordComparison   `json:"recordComparison,omitempty"`
	RecordSetVariable            string                  `json:"recordSetVariable,omitempty"`
	RecordCard                   *PageRecordCard         `json:"recordCard,omitempty"`
	Sparkline                    *PageSparkline          `json:"sparkline,omitempty"`
	SparklineDecimalVariable     string                  `json:"sparklineDecimalVariable,omitempty"`
	SparklineNumberVariable      string                  `json:"sparklineNumberVariable,omitempty"`
	GroupValueVariable           string                  `json:"groupValueVariable,omitempty"`
	GroupSetVariable             string                  `json:"groupSetVariable,omitempty"`
	RowValueVariable             string                  `json:"rowValueVariable,omitempty"`
	RowSetVariable               string                  `json:"rowSetVariable,omitempty"`
	ColumnValueVariable          string                  `json:"columnValueVariable,omitempty"`
	ColumnSetVariable            string                  `json:"columnSetVariable,omitempty"`
	Scatter                      *PageRecordScatter      `json:"scatter,omitempty"`
	RecordChart                  *PageRecordChart        `json:"recordChart,omitempty"`
	RecordList                   *PageRecordList         `json:"recordList,omitempty"`
	HeadingLevel                 string                  `json:"headingLevel,omitempty"`
	Histogram                    *PageHistogram          `json:"histogram,omitempty"`
	InputKind                    string                  `json:"inputKind,omitempty"`
	PickerValueVariable          string                  `json:"pickerValueVariable,omitempty"`
	RecordPicker                 *PageRecordPicker       `json:"recordPicker,omitempty"`
	Spacer                       *PageSpacer             `json:"spacer,omitempty"`
	Separator                    *PageSeparator          `json:"separator,omitempty"`
	Notice                       *PageNotice             `json:"notice,omitempty"`
	AlertValueVariable           string                  `json:"alertValueVariable,omitempty"`
	AlertBanner                  *PageAlertBanner        `json:"alertBanner,omitempty"`
	DateKind                     string                  `json:"dateKind,omitempty"`
	DateOffset                   string                  `json:"dateOffset,omitempty"`
	DateVariable                 string                  `json:"dateVariable,omitempty"`
	DateLabel                    *string                 `json:"dateLabel,omitempty"`
	ChoiceSetVariable            string                  `json:"choiceSetVariable,omitempty"`
	ChoiceVariable               string                  `json:"choiceVariable,omitempty"`
	ChoiceInput                  *PageChoiceInput        `json:"choiceInput,omitempty"`
	BooleanVariant               string                  `json:"booleanVariant,omitempty"`
	BooleanVariable              string                  `json:"booleanVariable,omitempty"`
	BooleanLabel                 *string                 `json:"booleanLabel,omitempty"`
	RangeInput                   *PageRangeInput         `json:"rangeInput,omitempty"`
	RangeMinVariable             string                  `json:"rangeMinVariable,omitempty"`
	RangeMaxVariable             string                  `json:"rangeMaxVariable,omitempty"`
	Leaderboard                  *PageLeaderboard        `json:"leaderboard,omitempty"`
	SummaryField                 string                  `json:"summaryField,omitempty"`
	StatisticsVariable           string                  `json:"statisticsVariable,omitempty"`
	Gauge                        *PageGauge              `json:"gauge,omitempty"`
	GaugeValueVariable           string                  `json:"gaugeValueVariable,omitempty"`
	ProgressLabel                string                  `json:"progressLabel,omitempty"`
	ProgressValueVariable        string                  `json:"progressValueVariable,omitempty"`
	ProgressTotalVariable        string                  `json:"progressTotalVariable,omitempty"`
	ProgressTotal                string                  `json:"progressTotal,omitempty"`
	CountVariable                string                  `json:"countVariable,omitempty"`
	MetricPresentation           *PageMetricPresentation `json:"metricPresentation,omitempty"`
	StatusTracker                *PageStatusTracker      `json:"statusTracker,omitempty"`
	RecordLinks                  []PageRecordLink        `json:"recordLinks,omitempty"`
	Buttons                      []PageButton            `json:"buttons,omitempty"`
	RecordView                   *PageRecordView         `json:"recordView,omitempty"`
	DetailPresentation           *PageDetailPresentation `json:"detailPresentation,omitempty"`
	TableColumns                 []PageTableColumn       `json:"tableColumns,omitempty"`
	ShowSearch                   *bool                   `json:"showSearch,omitempty"`
	SelectionSetVariable         string                  `json:"selectionSetVariable,omitempty"`
	InlineEdit                   *PageInlineEdit         `json:"inlineEdit,omitempty"`
	Facets                       []PageFacet             `json:"facets,omitempty"`
	FilterSearchVariable         string                  `json:"filterSearchVariable,omitempty"`
	// ID is required when Document references this section; older pages omit it.
	ID            string `json:"id,omitempty"`
	ConfigVersion int    `json:"configVersion,omitempty"`
	// Widget is table, detail, actions, chart, metric, text, filter, form,
	// timeline, tasks or a published function.
	Widget string `json:"widget"`
	Title  string `json:"title,omitempty"`
	// Width is full or half, in the order the sections are laid out.
	Width string `json:"width,omitempty"`
	// Object is what it shows; empty: the page's own object.
	Object AssetRef `json:"object,omitempty"`
	// Selection is the record variable a table writes or detail/actions read.
	Selection          string `json:"selection,omitempty"`
	CollectionVariable string `json:"collectionVariable,omitempty"`
	RecordVariable     string `json:"recordVariable,omitempty"`
	SelectionVariable  string `json:"selectionVariable,omitempty"`
	FilterVariable     string `json:"filterVariable,omitempty"`
	// ParentSelection supplies a typed parent record to a related section;
	// empty uses the page object's shared selection.
	ParentSelection string `json:"parentSelection,omitempty"`
	// Relation is the named inverse (FieldInfo.Inverse) of Object's reference to
	// the parent selection's object: the section shows its related records
	// through it, or a form supplies that reference when creating a related
	// record (ADR-0040 21b D3). Empty: no declared relation binding.
	Relation string `json:"relation,omitempty"`
	// Query is a named query (AssetQuery) the section lists instead of all of
	// Object: its conditions, and the selected record when it takes one (21c).
	Query        AssetRef           `json:"query,omitempty"`
	Fields       []string           `json:"fields,omitempty"`  // table, detail, filter, form
	Actions      []AssetRef         `json:"actions,omitempty"` // actions
	Group        string             `json:"group,omitempty"`   // chart: the field it groups by, or "<field>:month"
	ChartVariant string             `json:"chartVariant,omitempty"`
	Mark         string             `json:"mark,omitempty"`
	ColumnGroup  string             `json:"columnGroup,omitempty"`
	TimeStart    string             `json:"timeStart,omitempty"`
	TimeEnd      string             `json:"timeEnd,omitempty"`
	TimeLabel    string             `json:"timeLabel,omitempty"`
	TimeGroup    string             `json:"timeGroup,omitempty"`
	CardLabel    string             `json:"cardLabel,omitempty"`
	Measure      string             `json:"measure,omitempty"`   // chart, metric: count, sum:<field>, avg:<field>, min:<field>, max:<field>
	Text         string             `json:"text,omitempty"`      // text
	Function     *AssetBinding      `json:"function,omitempty"`  // exact published function
	Operation    *AssetBinding      `json:"operation,omitempty"` // exact compute owner revision
	Inputs       map[string]Binding `json:"inputs,omitempty"`    // compute inputs or form fields supplied by constants/parent record paths
}

// Widgets are the widget kinds a composed page may hold (ADR-0035 D2).
// A filter outputs the page's second variable — the records it narrows to —
// which the table, chart and metric over the same object read (16b).
// Filterable are the field types a filter widget offers: values that repeat.
var Filterable = []string{"choice", "boolean", "reference"}

// Application is what someone in this tenant hands to its people (ADR-0036): a
// name, an icon from the platform's set, and the pages it holds in order. It
// grants nothing; each page is offered to whoever may read what it shows.
type Application struct {
	UIProfile   string                  `json:"uiProfile,omitempty"`
	Variables   map[string]PageVariable `json:"variables,omitempty"`
	Queries     map[string]PageQuery    `json:"queries,omitempty"`
	Name        string                  `json:"name"`
	Title       string                  `json:"title"`
	Description string                  `json:"description,omitempty"`
	// Icon is one of Icons: the workspace draws it in the launcher and the navigation.
	Icon  string   `json:"icon,omitempty"`
	Pages []string `json:"pages"` // page names, in the order people see them
	// Groups are headings in its navigation (ADR-0036 17b), each over some of
	// Pages in its own order; a page in no group sits under the application's name.
	Groups []AppGroup `json:"groups,omitempty"`
	// Resources are shared owner references included in the release closure.
	// Pages remain the sole navigation list; membership grants no permissions.
	Resources []AssetRef `json:"resources,omitempty"`
}

// CheckResources checks explicit non-navigation resource membership. Assets
// keep their existing owner and may be shared by several applications.
func (a Application) CheckResources() error {
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
	return nil
}

// Dependencies is the single application closure path for discovery and
// releases. Page order affects navigation; dependency order is canonical.
func (a Application) Dependencies(owner string) []AssetRef {
	refs := append(slices.Clone(a.Resources), a.QueryPage().QueryReferences()...)
	for _, name := range a.Pages {
		refs = append(refs, AssetRef{App: owner, Kind: AssetPage, Name: name})
	}
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
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

// Icons are the icons a tenant's application may take (ADR-0036 D3).
var Icons = []string{"boxes", "clipboard", "people", "calendar", "wrench", "map", "chart", "sparkles"}

// Definition is one installed code asset as the reader may discover it.
// Entity and Action reuse the same descriptions as the existing record/action
// APIs; this registry is an index over those owners, not another executor.
type Definition struct {
	ContentVersion   string                  `json:"contentVersion,omitempty"`
	PropertyType     *PropertyType           `json:"propertyType,omitempty"`
	PropertyVersions map[string]PropertyType `json:"propertyVersions,omitempty"`
	LinkType         *LinkType               `json:"linkType,omitempty"`
	LinkVersions     map[string]LinkType     `json:"linkVersions,omitempty"`
	Ref              AssetRef                `json:"ref"`
	Source           string                  `json:"source"`  // code, until published definitions exist
	Version          string                  `json:"version"` // installed app manifest version, not a published revision
	ContractVersion  int                     `json:"contractVersion"`
	Requires         []AssetRef              `json:"requires"`
	Entity           *EntityInfo             `json:"entity,omitempty"`
	Action           *Action                 `json:"action,omitempty"`
	Page             *Page                   `json:"page,omitempty"`
	Application      *Application            `json:"application,omitempty"`
	Query            *NamedQuery             `json:"query,omitempty"`
	QueryVersions    map[string]NamedQuery   `json:"queryVersions,omitempty"`
	Function         *AIFunction             `json:"function,omitempty"`
	Operation        *Operation              `json:"operation,omitempty"`
}
