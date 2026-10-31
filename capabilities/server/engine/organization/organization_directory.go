package organization

import (
	"platformserver/platform"
	"slices"
)

func (m Model) InternalUsage(id string) string {
	for _, r := range m.Relationships {
		if r.Source == id || r.Target == id {
			return "relationship " + r.ID
		}
	}
	for _, el := range m.Elements {
		if el.ID == id && el.Published {
			return "published element"
		}
	}
	for _, v := range m.Views {
		if slices.Contains(v.Elements, id) || slices.Contains(v.Context, id) {
			return "view " + v.ID
		}
		if _, ok := v.Layout[id]; ok {
			return "view " + v.ID
		}
		for _, pin := range v.Pins {
			if pin.Anchor == id || pin.Ref == "record:enterprise.element/"+id {
				return "view " + v.ID
			}
		}
	}
	if refs := m.ElementPropertyUses(id, ""); len(refs) > 0 {
		return "element " + refs[0]
	}
	for _, link := range m.TenantLinks {
		if link.Organization == id || ImportedElementID(link.Tenant, link.Root) == id {
			return "tenant link " + link.ID
		}
	}
	return ""
}

func (m Model) ElementPropertyUses(id string, day platform.Date) []string {
	out := []string{}
	for _, element := range m.Elements {
		if element.ID == id || element.Deleted && day != "" {
			continue
		}
		if day != "" && !slices.ContainsFunc(element.PeriodsFromLegacy(), func(period platform.Period) bool { return period.Until == "" || period.Until > day }) {
			continue
		}
		for _, property := range historicalReferences[element.Stereotype] {
			match := func(value any) bool {
				text, ok := value.(string)
				return ok && (text == id || text == "enterprise.element/"+id)
			}
			value := element.Properties[property.Name]
			found := match(value)
			if property.Many {
				if values, ok := value.([]any); ok {
					found = slices.ContainsFunc(values, match)
				}
				if values, ok := value.([]string); ok {
					found = slices.ContainsFunc(values, func(value string) bool { return match(value) })
				}
			}
			if found {
				out = append(out, element.ID)
				break
			}
		}
	}
	return out
}

type DirectoryModel struct {
	Version      string                           `json:"version"`
	URI          string                           `json:"uri"`
	Domains      []string                         `json:"domains"`
	Stereotypes  map[string]*DirectoryType        `json:"stereotypes"`
	Enumerations map[string]*DirectoryEnumeration `json:"enumerations"`
}

type DirectoryType struct {
	Name        string              `json:"name"`
	ID          string              `json:"id"`
	Domain      string              `json:"domain"`
	Abstract    bool                `json:"abstract,omitempty"`
	Generals    []string            `json:"generals,omitempty"`
	Bases       []string            `json:"bases,omitempty"`
	Properties  []DirectoryProperty `json:"properties,omitempty"`
	Client      []string            `json:"client,omitempty"`
	Supplier    []string            `json:"supplier,omitempty"`
	Constraints []string            `json:"constraints,omitempty"`
}

type DirectoryProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Many bool   `json:"many,omitempty"`
}

type DirectoryEnumeration struct {
	Name     string   `json:"name"`
	Literals []string `json:"literals"`
}

func directoryElement(name string) bool {
	return name == Organization || name == Post || name == Person
}

func directoryRelationship(name string) bool {
	return name == Placement || name == Membership || name == FillsPost || name == Owns
}

func directory() *DirectoryModel {
	types := map[string]*DirectoryType{}
	for _, name := range []string{Organization, Post, Person, Placement, Membership, FillsPost, Owns} {
		types[name] = &DirectoryType{Name: name, ID: name, Domain: "Organisation"}
	}
	return &DirectoryModel{Version: "directory-1", URI: "platform:directory:1", Domains: []string{"Organisation"}, Stereotypes: types, Enumerations: map[string]*DirectoryEnumeration{}}
}

func (m *DirectoryModel) Is(name, parent string) bool { return name == parent }

func (m *DirectoryModel) Relationship(name string) bool { return directoryRelationship(name) }

var historicalReferences = map[string][]DirectoryProperty{
	"Achiever":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Achieves":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Activity":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualCondition":                {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualEffect":                   {{Name: "enablingCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualEnduringTask":             {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualEnterprisePhase":          {{Name: "statementTask", Type: "ActualEnduringTask", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualEnvironment":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualLocation":                 {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMeasurement":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMeasurementSet":           {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMission":                  {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualMissionPhase":             {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualMissionScenario":          {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMissionVignette":          {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualOrganization":             {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualOrganizationRole":         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualOrganizationalResource":   {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualOutcome":                  {{Name: "enablingCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualPerson":                   {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualPost":                     {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualProject":                  {{Name: "ownedMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualProjectMilestone":         {{Name: "actualResource", Type: "ActualResource", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "versionReleased", Type: "VersionedElement", Many: true}, {Name: "versionWithdrawn", Type: "VersionedElement", Many: true}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualProjectMilestoneRole":     {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualProjectRole":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualPropertySet":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResource":                 {{Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResourceRelationship":     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResourceRole":             {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResponsibility":           {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualResponsibleResource":      {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualRisk":                     {{Name: "actualRiskOwner", Type: "ActualResponsibleResource", Many: false}, {Name: "affectedActualResource", Type: "ActualResource", Many: false}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualService":                  {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualState":                    {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualStrategicPhase":           {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"AffectableElement":              {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Affects":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"AffectsInContext":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Alias":                          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArbitraryConnector":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArchitecturalDescription":       {{Name: "view", Type: "View", Many: true}, {Name: "viewpoint", Type: "Viewpoint", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArchitecturalReference":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Architecture":                   {{Name: "describedBy", Type: "ArchitecturalDescription", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArchitectureMetadata":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Asset":                          {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"AssetRole":                      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Capability":                     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"CapabilityConfiguration":        {{Name: "doctrine", Type: "StandardOperationalActivity", Many: true}, {Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"CapabilityRole":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"CapableElement":                 {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Challenge":                      {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Command":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ComparesTo":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Competence":                     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"CompetenceForRole":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"CompetenceToConduct":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ConceptItem":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ConceptRole":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Concern":                        {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Condition":                      {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ConflictsWith":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Control":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Creates":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Defines":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Definition":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"DesignationKind":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Desirer":                        {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Desires":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Doctrine":                       {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Driver":                         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Effect":                         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Enables":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnhancedSecurityControl":        {{Name: "mitigatingActualResource", Type: "ActualResource", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Enhances":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnterpriseGoal":                 {{Name: "benefit", Type: "ValueItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnterpriseObjective":            {{Name: "benefit", Type: "ValueItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnterpriseVision":               {{Name: "statement", Type: "VisionStatement", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Environment":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"EnvironmentProperty":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EvokedBy":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Exchange":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Exhibits":                       {{Name: "environmentalConditions", Type: "Environment", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FieldedCapability":              {{Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FillsPost":                      {{Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Forecast":                       {{Name: "forecastPeriod", Type: "ActualStrategicPhase", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Function":                       {{Name: "affectedResource", Type: "ResourceExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionAction":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionControlFlow":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionEdge":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionObjectFlow":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"GeoPoliticalExtentType":         {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}},
	"GovernedBy":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"HighLevelOperationalConcept":    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ISO8601DateTime":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ImpactedBy":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Implements":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Information":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"InformationModel":               {{Name: "conformsTo", Type: "Standard", Many: true}},
	"IsCapableToPerform":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"KnownResource":                  {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}},
	"Location":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"LocationHolder":                 {{Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MapsToCapability":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MapsToGoal":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MeasurableElement":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Measurement":                    {{Name: "environmentalContext", Type: "ActualCondition", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MeasurementSet":                 {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Metadata":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MilestoneDependency":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Mission":                        {{Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionEngineeringThread":       {{Name: "affectedResource", Type: "ResourceExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionScenario":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionTask":                    {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionTaskAction":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionThread":                  {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionVignette":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Mitigates":                      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MotivatedBy":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MotivationalElement":            {{Name: "conformsTo", Type: "Standard", Many: true}},
	"NaturalResource":                {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"OperationalActivity":            {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalActivityAction":      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalActivityEdge":        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalAgent":               {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalArchitecture":        {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"OperationalAsset":               {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalConnector":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalConstraint":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalControlFlow":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalExchange":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalExchangeItem":        {{Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalInformation":         {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}},
	"OperationalInformationRole":     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalInterface":           {{Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalMessage":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalMethod":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalMitigation":          {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"OperationalObjectFlow":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalParameter":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalPerformer":           {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"OperationalPort":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalRole":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"OperationalSignal":              {{Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalSignalProperty":      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalStateDescription":    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Opportunity":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"OpposableElement":               {{Name: "designation", Type: "DesignationKind", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Opposes":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Organization":                   {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"OrganizationInPhase":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OrganizationalResource":         {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"OwnsProcess":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OwnsRisk":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OwnsRiskInContext":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OwnsValue":                      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"PerformsInContext":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Person":                         {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"PhaseableElement":               {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Phases":                         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"PhysicalResource":               {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"Post":                           {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"PresentedBy":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProblemDomain":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"Project":                        {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"ProjectActivity":                {{Name: "affectedResource", Type: "ResourceExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectActivityAction":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectMilestone":               {{Name: "resource", Type: "ResourcePerformer", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectMilestoneRole":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectRole":                    {{Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ProjectSequence":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectStatus":                  {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectTheme":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"PropertySet":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Protects":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtectsInContext":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Protocol":                       {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtocolImplementation":         {{Name: "implements", Type: "Protocol", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtocolLayer":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtocolStack":                  {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProvidedServiceLevel":           {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProvidesCompetence":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"RequiredServiceLevel":           {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"RequiresCompetence":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Resource":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceArchitecture":           {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"ResourceArtifact":               {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourceAsset":                  {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceConnector":              {{Name: "boundaryCondition", Type: "Environment", Many: true}, {Name: "implements", Type: "Protocol", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}},
	"ResourceConstraint":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceExchange":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceExchangeItem":           {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceInformation":            {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"ResourceInformationRole":        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceInterface":              {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceMessage":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceMethod":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceMitigation":             {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"ResourceParameter":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourcePerformer":              {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourcePort":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "implements", Type: "Protocol", Many: true}},
	"ResourceRole":                   {{Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourceService":                {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourceServiceInterface":       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceSignal":                 {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceSignalProperty":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceStateDescription":       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Responsibility":                 {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"ResponsibleFor":                 {{Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Risk":                           {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Rule":                           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SameAs":                         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityConstraint":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityControl":                {{Name: "mitigatingActualResource", Type: "ActualResource", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityControlFamily":          {{Name: "mitigatingActualResource", Type: "ActualResource", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityEnclave":                {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"SecurityProcess":                {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityProcessAction":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityRisk":                   {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Sequence":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Service":                        {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"ServiceArchitecture":            {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"ServiceConnector":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceContract":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceControlFlow":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceExchange":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceExchangeItem":            {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceFunction":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceFunctionAction":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceFunctionEdge":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceInterface":               {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceMessage":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceMethod":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceObjectFlow":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceParameter":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServicePolicy":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServicePort":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceRole":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceSignal":                  {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceSignalProperty":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceStateDescription":        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Software":                       {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"Stakeholder":                    {{Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Standard":                       {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StandardOperationalActivity":    {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StatusIndicators":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicAsset":                 {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicConstraint":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicExchange":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicExchangeItem":          {{Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicInformation":           {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"StrategicPhase":                 {{Name: "conformsTo", Type: "Standard", Many: true}},
	"StructuralPart":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfForecast":              {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfOperationalConstraint": {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfResourceConstraint":    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfSecurityConstraint":    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfStrategicConstraint":   {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Supports":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"System":                         {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"Technology":                     {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"TemporalPart":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"UAFElement":                     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ValueItem":                      {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"ValueStream":                    {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"VersionOfConfiguration":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"VersionSuccession":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"VersionedElement":               {{Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"View":                           {{Name: "viewpoint", Type: "Viewpoint", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Viewpoint":                      {{Name: "stakeholder", Type: "Stakeholder", Many: true}, {Name: "concern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"VisionStatement":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"WholeLifeConfiguration":         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"WholeLifeEnterprise":            {{Name: "statementTask", Type: "ActualEnduringTask", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
}
