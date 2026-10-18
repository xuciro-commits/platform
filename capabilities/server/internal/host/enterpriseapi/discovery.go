package enterpriseapi

import (
	"platformserver/internal/host/enterpriseapi/uaf"
)

const (
	ID              = "enterprise"
	ModelType       = "enterprise.model"
	SchemaSliceSync = "enterprise.slice.sync"
)

type PatternInfo struct {
	Pattern
	LevelName string  `json:"levelName"`
	Preview   Preview `json:"preview"`
}

type Metamodel struct {
	Version      string                      `json:"version"`
	URI          string                      `json:"uri"`
	Domains      []string                    `json:"domains"`
	Profile      []ProfileEntry              `json:"profile"`
	Stereotypes  map[string]*uaf.Stereotype  `json:"stereotypes"`
	Enumerations map[string]*uaf.Enumeration `json:"enumerations"`
	Views        []Viewpoint                 `json:"views"`
	// Contracts are the relationship stereotypes as the host checks them
	// (ADR-0031 D2): what each end accepts, the standard's text it comes from,
	// and what this profile adds. The modeler offers exactly these.
	Contracts []Contract `json:"contracts"`
}

type EnterpriseQueryElement struct {
	ID         string `json:"id"`
	Stereotype string `json:"stereotype"`
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	ShortName  string `json:"shortName,omitempty"`
	Legal      bool   `json:"legal,omitempty"`
	From       Date   `json:"from,omitempty"`
	Until      Date   `json:"until,omitempty"`
	// Closed marks an element that is no longer live today (ended, merged,
	// decommissioned): resolve returns it so a stored reference still reads.
	Closed bool `json:"closed,omitempty"`
}

type EnterpriseQueryResult struct {
	Elements []EnterpriseQueryElement `json:"elements"`
}
