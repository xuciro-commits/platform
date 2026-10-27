package enterprise

import (
	"platformserver/platform"
	"platformserver/tools/uaf"
)

const (
	ID              = "enterprise"
	ModelType       = "enterprise.model"
	SchemaSliceSync = "enterprise.slice.sync"
)

type Metamodel struct {
	Version      string                      `json:"version"`
	URI          string                      `json:"uri"`
	Domains      []string                    `json:"domains"`
	Profile      []ProfileEntry              `json:"profile"`
	Stereotypes  map[string]*uaf.Stereotype  `json:"stereotypes"`
	Enumerations map[string]*uaf.Enumeration `json:"enumerations"`
	Views        []Viewpoint                 `json:"views"`
	// Contracts are the relationship stereotypes as the host checks them
	//what each end accepts, the standard's text it comes from,
	// and what this profile adds. The modeler offers exactly these.
	Contracts []Contract `json:"contracts"`
}

type EnterpriseQueryElement struct {
	ID         string            `json:"id"`
	Stereotype string            `json:"stereotype"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind,omitempty"`
	ShortName  string            `json:"shortName,omitempty"`
	Legal      bool              `json:"legal,omitempty"`
	From       platform.Date     `json:"from,omitempty"`
	Until      platform.Date     `json:"until,omitempty"`
	Periods    []platform.Period `json:"periods,omitempty"`
	// Closed marks an element that is no longer live today (ended, merged,
	// decommissioned): resolve returns it so a stored reference still reads.
	Closed bool `json:"closed,omitempty"`
}

type EnterpriseQueryResult struct {
	Elements []EnterpriseQueryElement `json:"elements"`
}
