package ontology

import (
	"slices"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

type ImplementationRecord struct {
	platform.Record
	Name        string                                  `json:"name" field:"required,search"`
	Title       string                                  `json:"title" field:"required,search"`
	Description string                                  `json:"description,omitempty" type:"longtext"`
	Contract    string                                  `json:"contract" field:"required" title:"Contract" help:"The capability contract it implements, as name/major"`
	Kind        string                                  `json:"kind" field:"required" choices:"connector"`
	Origins     []string                                `json:"origins,omitempty" title:"Outbound origins" help:"The exact https origins the connector may call"`
	Credential  string                                  `json:"credential,omitempty" choices:"api-key,bearer" help:"The kind of credential the tenant gives it"`
	Requests    []runtimeprotocol.ImplementationRequest `json:"requests,omitempty" type:"json"`
	State       string                                  `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                                     `json:"version,omitempty" field:"readonly"`
	Published   string                                  `json:"published,omitempty" field:"readonly,internal" type:"longtext" title:"Installed" help:"What is installed"`
	Versions    []string                                `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

type ImplementationDefinition runtimeprotocol.ProtocolImplementation

func (d ImplementationDefinition) Check() error {
	return runtimeprotocol.ProtocolImplementation(d).Check()
}

func (r ImplementationRecord) Definition() ImplementationDefinition {
	return ImplementationDefinition{Name: r.Name, Title: r.Title, Description: r.Description, Contract: r.Contract, Kind: r.Kind,
		Origins: slices.Clone(r.Origins), Credential: r.Credential, Requests: slices.Clone(r.Requests)}
}

func ImplementationRecordFromDeclaration(d ImplementationDefinition) ImplementationRecord {
	return ImplementationRecord{Name: d.Name, Title: d.Title, Description: d.Description, Contract: d.Contract, Kind: d.Kind,
		Origins: slices.Clone(d.Origins), Credential: d.Credential, Requests: slices.Clone(d.Requests)}
}
