package build

import (
	"fmt"
	"slices"

	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	dataowner "platformserver/services/data"
)

type resourceFamily uint8

const (
	recordResources resourceFamily = iota
	definitionResources
	integrationResources
	versionedResources
)

type resourceRouteMode string

const (
	defaultResourceRoute     resourceRouteMode = "default"
	pageResourceRoute        resourceRouteMode = "page"
	releasePlanResourceRoute resourceRouteMode = "release-plan"
)

type resourcePresentation struct {
	kind        string
	view        string
	projectView string
	route       resourceRouteMode
	label       string
	plural      string
	order       int
	listed      bool
	release     bool
	releaseKind string
}

func presented(kind, view, label, plural string, order int) resourcePresentation {
	return resourcePresentation{
		kind: kind, view: view, route: defaultResourceRoute,
		label: label, plural: plural, order: order,
		listed: true, release: true, releaseKind: kind,
	}
}

func unreleased(kind, view, label, plural string, order int) resourcePresentation {
	presentation := presented(kind, view, label, plural, order)
	presentation.release = false
	return presentation
}

func releaseOnly(kind, view, label, plural string, order int) resourcePresentation {
	presentation := presented(kind, view, label, plural, order)
	presentation.listed = false
	return presentation
}

func withReleaseKind(presentation resourcePresentation, kind string) resourcePresentation {
	presentation.releaseKind = kind
	return presentation
}

func withPageRoute(presentation resourcePresentation) resourcePresentation {
	presentation.route = pageResourceRoute
	return presentation
}

func withReleasePlanRoute(presentation resourcePresentation, projectView string) resourcePresentation {
	presentation.route, presentation.projectView = releasePlanResourceRoute, projectView
	return presentation
}

type resourceDescriptor struct {
	app                 string
	typ                 string
	kind                platform.AssetKind
	family              resourceFamily
	released            bool
	qualifiedName       bool
	defaultDraft        bool
	directInstall       bool
	acceptedPublication bool
	presentations       []resourcePresentation
}

func (d resourceDescriptor) publication() string { return d.typ + ".publish" }

func (d resourceDescriptor) record(id string) string { return d.typ + "/" + id }

var resourceDescriptors = []resourceDescriptor{
	{
		app: ID, typ: ObjectType, kind: platform.AssetObject, family: recordResources, released: true, qualifiedName: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("object", "object-type", "Object type", "Object types", 3)},
	},
	{
		app: ID, typ: ActionTypeType, kind: platform.AssetAction, family: recordResources, released: true, defaultDraft: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("action", "action-type", "Action type", "Action types", 1)},
	},
	{
		app: ID, typ: PageType, kind: platform.AssetPage, family: recordResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{withPageRoute(presented("page", "module", "Page", "Pages", 10))},
	},
	{
		app: ID, typ: AppType, kind: platform.AssetApp, family: recordResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{releaseOnly("app", "project", "Application", "Applications", 30)},
	},
	{
		app: ID, typ: automationauthoring.ProcessType, kind: platform.AssetFlow, family: recordResources, released: true, qualifiedName: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{
			presented("flow", "flow", "Logic flow", "Logic flows", 11),
			unreleased("automation", "automation", "Automation", "Automations", 12),
		},
	},
	{
		app: ID, typ: CodeType, kind: platform.AssetCompute, family: versionedResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("compute", "code", "Code function", "Code functions", 17)},
	},
	{
		app: ID, typ: TableType, kind: platform.AssetCompute, family: versionedResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{withReleaseKind(presented("table", "decision-table", "Decision table", "Decision tables", 18), "table")},
	},
	{
		app: ID, typ: AgentType, kind: platform.AssetAgent, family: versionedResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("agent", "agent", "Agent", "Agents", 16)},
	},
	{
		app: ID, typ: TestPlanType, kind: platform.AssetEvaluation, family: versionedResources, released: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("evaluation-set", "evaluation-set", "Evaluation set", "Evaluation sets", 14)},
	},
	{
		app: ID, typ: TestPlanType, kind: platform.AssetTestPlan, family: versionedResources, released: true, defaultDraft: true, acceptedPublication: true,
		presentations: []resourcePresentation{withReleasePlanRoute(presented("test-plan", "test-plan", "Business test plan", "Business test plans", 15), "project")},
	},
	{
		app: ID, typ: SampleSetType, kind: platform.AssetSampleSet, family: versionedResources, released: true, defaultDraft: true,
		presentations: []resourcePresentation{presented("sample-set", "sample-set", "Sample set", "Sample sets", 2)},
	},
	{
		app: ID, typ: dataowner.ConnectionType, kind: platform.AssetConnection, family: integrationResources, released: true, defaultDraft: true,
		presentations: []resourcePresentation{presented("connection", "connection", "Connection", "Connections", 19)},
	},
	{
		app: ID, typ: dataowner.ListenerType, kind: platform.AssetListener, family: integrationResources, released: true, defaultDraft: true, directInstall: true,
		presentations: []resourcePresentation{presented("listener", "listener", "Listener", "Listeners", 22)},
	},
	{
		app: ID, typ: dataowner.SourceType, kind: platform.AssetSource, family: integrationResources, released: true, defaultDraft: true, directInstall: true,
		presentations: []resourcePresentation{presented("source", "data-source", "Data source", "Data sources", 21)},
	},
	{
		app: ID, typ: platform.MediaSetType, kind: platform.AssetMediaSet, family: integrationResources, released: true, defaultDraft: true,
		presentations: []resourcePresentation{withReleaseKind(presented("media-set", "media-set", "Media set", "Media sets", 23), "mediaset")},
	},
	{
		app: ID, typ: dataowner.DatasetType, kind: platform.AssetDataset, family: integrationResources, released: true, defaultDraft: true,
		presentations: []resourcePresentation{presented("dataset", "dataset", "Dataset", "Datasets", 25)},
	},
	{
		app: ID, typ: dataowner.PipelineType, kind: platform.AssetPipeline, family: integrationResources, released: true, defaultDraft: true, directInstall: true,
		presentations: []resourcePresentation{presented("pipeline", "pipeline", "Pipeline", "Pipelines", 26)},
	},
	{
		app: ID, typ: platform.ScheduleType, kind: platform.AssetSchedule, family: integrationResources, released: true, defaultDraft: true,
		presentations: []resourcePresentation{presented("schedule", "schedule", "Schedule", "Schedules", 24)},
	},
	{
		app: ID, typ: dataowner.WritebackType, kind: platform.AssetWriteback, family: integrationResources, released: true, defaultDraft: true, directInstall: true,
		presentations: []resourcePresentation{presented("writeback", "writeback", "Writeback", "Writebacks", 27)},
	},
	{
		app: ID, typ: dataowner.ObservationType, kind: platform.AssetObservation, family: integrationResources, released: true, defaultDraft: true,
		presentations: []resourcePresentation{presented("observation", "observation-source", "Observation source", "Observation sources", 20)},
	},
	{
		app: ID, typ: dataowner.MatchType, kind: platform.AssetMatching, family: integrationResources, released: true, defaultDraft: true, directInstall: true,
		presentations: []resourcePresentation{presented("matching", "match", "Matching rule", "Matching rules", 28)},
	},
	{
		app: ID, typ: QueryType, kind: platform.AssetQuery, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("query", "object-set", "Object set", "Object sets", 9)},
	},
	{
		app: ID, typ: PropertyTypeType, kind: platform.AssetPropertyType, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("property-type", "property-type", "Shared property", "Shared properties", 6)},
	},
	{
		app: ID, typ: ValueTypeType, kind: platform.AssetValueType, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("value-type", "value-type", "Value type", "Value types", 7)},
	},
	{
		app: ID, typ: LinkTypeType, kind: platform.AssetLinkType, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("link-type", "link-type", "Link type", "Link types", 5)},
	},
	{
		app: ID, typ: InterfaceType, kind: platform.AssetInterface, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("interface", "interfaces", "Interface", "Interfaces", 8)},
	},
	{
		app: ID, typ: FunctionType, kind: platform.AssetFunction, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("function", "function", "AI function", "AI functions", 13)},
	},
	{
		app: ID, typ: ObjectTypeGroupType, kind: platform.AssetObjectTypeGroup, family: definitionResources, released: true, defaultDraft: true, directInstall: true, acceptedPublication: true,
		presentations: []resourcePresentation{presented("object-type-group", "object-type-group", "Object type group", "Object type groups", 4)},
	},
	{
		app: ID, typ: ProductType, kind: platform.AssetProduct, family: recordResources, defaultDraft: true,
		presentations: []resourcePresentation{unreleased("product", "products", "Product", "Products", 29)},
	},
}

type ResourceMetadataEntry struct {
	App                 string            `json:"app"`
	Kind                string            `json:"kind"`
	Type                string            `json:"type"`
	AssetKind           string            `json:"assetKind"`
	Ref                 string            `json:"ref"`
	View                string            `json:"view"`
	ProjectView         string            `json:"projectView,omitempty"`
	Route               resourceRouteMode `json:"route"`
	Label               string            `json:"label"`
	Plural              string            `json:"plural"`
	Order               int               `json:"order"`
	Listed              bool              `json:"listed"`
	Release             bool              `json:"release"`
	ReleaseKind         string            `json:"releaseKind"`
	Publication         string            `json:"publication"`
	DirectInstall       bool              `json:"directInstall"`
	AcceptedPublication bool              `json:"acceptedPublication"`
	QualifiedName       bool              `json:"qualifiedName"`
	Released            bool              `json:"released"`
}

func ResourceMetadata() ([]ResourceMetadataEntry, error) {
	if err := validateResourceDescriptors(); err != nil {
		return nil, err
	}
	var entries []ResourceMetadataEntry
	for _, descriptor := range resourceDescriptors {
		for _, presentation := range descriptor.presentations {
			entries = append(entries, ResourceMetadataEntry{
				App: descriptor.app, Kind: presentation.kind, Type: descriptor.typ,
				AssetKind: string(descriptor.kind), Ref: string(descriptor.kind),
				View: presentation.view, ProjectView: presentation.projectView,
				Route: presentation.route, Label: presentation.label, Plural: presentation.plural,
				Order: presentation.order, Listed: presentation.listed, Release: presentation.release,
				ReleaseKind: presentation.releaseKind, Publication: descriptor.publication(),
				DirectInstall: descriptor.directInstall, AcceptedPublication: descriptor.acceptedPublication,
				QualifiedName: descriptor.qualifiedName, Released: descriptor.released,
			})
		}
	}
	slices.SortStableFunc(entries, func(a, b ResourceMetadataEntry) int { return a.Order - b.Order })
	return entries, nil
}

func validateResourceDescriptors() error {
	kinds, orders, releases := map[string]string{}, map[int]string{}, map[string]string{}
	for _, descriptor := range resourceDescriptors {
		if descriptor.app == "" || descriptor.typ == "" {
			return fmt.Errorf("resource descriptor needs an app and type")
		}
		if err := (platform.AssetRef{App: descriptor.app, Kind: descriptor.kind, Name: "metadata"}).Check(); err != nil {
			return fmt.Errorf("resource descriptor %s: %w", descriptor.typ, err)
		}
		if len(descriptor.presentations) == 0 {
			return fmt.Errorf("resource descriptor %s has no frontend presentation", descriptor.typ)
		}
		for _, presentation := range descriptor.presentations {
			if presentation.kind == "" || presentation.view == "" || presentation.label == "" || presentation.plural == "" {
				return fmt.Errorf("resource descriptor %s has an incomplete frontend presentation", descriptor.typ)
			}
			if other, exists := kinds[presentation.kind]; exists {
				return fmt.Errorf("resource kind %q is presented by both %s and %s", presentation.kind, other, descriptor.typ)
			}
			kinds[presentation.kind] = descriptor.typ
			if presentation.order < 1 {
				return fmt.Errorf("resource kind %q has no positive order", presentation.kind)
			}
			if other, exists := orders[presentation.order]; exists {
				return fmt.Errorf("resource kinds %q and %q share order %d", other, presentation.kind, presentation.order)
			}
			orders[presentation.order] = presentation.kind
			if presentation.release && !descriptor.released {
				return fmt.Errorf("resource kind %q is releasable but its descriptor is not released", presentation.kind)
			}
			if presentation.release {
				if presentation.releaseKind == "" {
					return fmt.Errorf("releasable resource kind %q has no release kind", presentation.kind)
				}
				if other, exists := releases[presentation.releaseKind]; exists {
					return fmt.Errorf("release kind %q belongs to both %q and %q", presentation.releaseKind, other, presentation.kind)
				}
				releases[presentation.releaseKind] = presentation.kind
			}
			if presentation.route == releasePlanResourceRoute && presentation.projectView == "" {
				return fmt.Errorf("resource kind %q needs a project view for its release route", presentation.kind)
			}
			if presentation.route != releasePlanResourceRoute && presentation.projectView != "" {
				return fmt.Errorf("resource kind %q has a project view without a release route", presentation.kind)
			}
		}
	}
	return nil
}

func findResourceDescriptor(match func(resourceDescriptor) bool) (resourceDescriptor, bool) {
	for _, descriptor := range resourceDescriptors {
		if match(descriptor) {
			return descriptor, true
		}
	}
	return resourceDescriptor{}, false
}

func resourceDescriptorOfType(typ string) (resourceDescriptor, bool) {
	return findResourceDescriptor(func(descriptor resourceDescriptor) bool {
		return descriptor.typ == typ && descriptor.defaultDraft
	})
}

func resourceDescriptorOfKind(kind platform.AssetKind) (resourceDescriptor, bool) {
	return findResourceDescriptor(func(descriptor resourceDescriptor) bool { return descriptor.kind == kind })
}

func resourceDescriptorOfSchema(schema string) (resourceDescriptor, bool) {
	return findResourceDescriptor(func(descriptor resourceDescriptor) bool {
		return descriptor.publication() == schema && descriptor.defaultDraft
	})
}

func publicationSchemas(include func(resourceDescriptor) bool) []string {
	seen := map[string]bool{}
	var schemas []string
	for _, descriptor := range resourceDescriptors {
		schema := descriptor.publication()
		if include(descriptor) && !seen[schema] {
			seen[schema] = true
			schemas = append(schemas, schema)
		}
	}
	return schemas
}

func acceptedPublicationSchemas() []string {
	return publicationSchemas(func(descriptor resourceDescriptor) bool { return descriptor.acceptedPublication })
}

func directInstallSchemas() []string {
	return publicationSchemas(func(descriptor resourceDescriptor) bool { return descriptor.directInstall })
}
