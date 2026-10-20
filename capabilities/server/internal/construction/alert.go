package construction

import (
	"platformserver/platform"
)

const AlertType = "build.alertrule"

// Alert is a rule over a published object (ADR-0008): when a record of it
// comes to match a condition, the people holding a role are told, once per
// record or every time. It is the operations face's "situation": no page has
// to be open for it to be seen.
type Alert struct {
	platform.Record
	Name              string   `json:"name" field:"required,search"`
	Title             string   `json:"title" field:"required,search"`
	Object            string   `json:"object" field:"required" title:"Object" help:"A published object, build.<name>"`
	Field             string   `json:"field" field:"required"`
	Operator          string   `json:"operator" field:"required" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value             string   `json:"value,omitempty"`
	Message           string   `json:"message" field:"required" type:"longtext" help:"What the people told should know or do"`
	Role              string   `json:"role,omitempty" title:"Tell" help:"A role of this builder; empty: the builder"`
	Every             bool     `json:"every,omitempty" title:"Every time" help:"Tell again on every change that matches; default once per record"`
	Active            bool     `json:"active" title:"Active"`
	State             string   `json:"state,omitempty" field:"readonly"`
	Version           int      `json:"version,omitempty" field:"readonly"`
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly"`
	Versions          []string `json:"versions,omitempty" field:"readonly" type:"json"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"json"`
}

func (a Alert) Definition() platform.AlertRule {
	return platform.AlertRule{Name: a.Name, Title: a.Title, Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: a.Object}, Field: a.Field, Operator: a.Operator, Value: a.Value, Message: a.Message, Role: a.Role, Every: a.Every, Active: a.Active}
}

// Matches says whether a record of the alert's object matches it now.
func (a Alert) Matches(record map[string]any, kind string) bool {
	return ValueHolds(record[a.Field], kind, a.Operator, a.Value)
}
