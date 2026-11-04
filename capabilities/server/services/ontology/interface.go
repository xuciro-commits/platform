package ontology

import (
	"slices"
	"strings"

	"platformserver/platform"
)

type InterfaceRecord struct {
	platform.Record
	Name        string                    `json:"name" field:"required,search"`
	Title       string                    `json:"title" field:"required,search"`
	Description string                    `json:"description" field:"required" type:"longtext"`
	Fields      []platform.InterfaceField `json:"fields" field:"required" type:"json"`
	State       string                    `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                       `json:"version,omitempty" field:"readonly"`
	Published   string                    `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions    []string                  `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

type InterfaceDefinition platform.Interface

func (d InterfaceDefinition) Check() error {
	app, _, _ := strings.Cut(d.Name, ".")
	return platform.Interface(d).Check(app)
}

func (r InterfaceRecord) Definition() InterfaceDefinition {
	return InterfaceDefinition{Name: r.Name, Title: r.Title, Description: r.Description, Fields: slices.Clone(r.Fields)}
}

func (r InterfaceRecord) KeepsPublishedFields(next InterfaceRecord) bool {
	for _, field := range r.Fields {
		if !slices.ContainsFunc(next.Fields, func(candidate platform.InterfaceField) bool {
			return candidate.Name == field.Name && candidate.Type == field.Type
		}) {
			return false
		}
	}
	return true
}
