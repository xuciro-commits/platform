package ontology

import (
	"slices"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

type ContractRecord struct {
	platform.Record
	Name        string                           `json:"name" field:"required,search"`
	Major       int                              `json:"major" title:"Major version"`
	Title       string                           `json:"title" field:"required,search"`
	Description string                           `json:"description,omitempty" type:"longtext"`
	Actions     []runtimeprotocol.ContractAction `json:"actions,omitempty" type:"json"`
	Reads       []string                         `json:"reads,omitempty"`
	Events      []platform.ProtocolEvent         `json:"events,omitempty" type:"json"`
	Tests       []runtimeprotocol.ContractTest   `json:"tests,omitempty" type:"json" title:"Conformance tests"`
	Languages   platform.Languages               `json:"languages,omitempty" type:"json" field:"internal"`
	State       string                           `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                              `json:"version,omitempty" field:"readonly"`
	Published   string                           `json:"published,omitempty" field:"readonly,internal" type:"longtext" title:"Installed" help:"What is installed"`
	Versions    []string                         `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

type ContractDefinition runtimeprotocol.ProtocolContract

func (d ContractDefinition) Check() error { return runtimeprotocol.ProtocolContract(d).Check() }

func cloneLanguages(in platform.Languages) platform.Languages {
	if len(in) == 0 {
		return nil
	}
	out := make(platform.Languages, len(in))
	for lang, dict := range in {
		cp := make(map[string]string, len(dict))
		for k, v := range dict {
			cp[k] = v
		}
		out[lang] = cp
	}
	return out
}

func (r ContractRecord) Definition() ContractDefinition {
	return ContractDefinition{Name: r.Name, Major: r.Major, Title: r.Title, Description: r.Description, Actions: slices.Clone(r.Actions), Reads: slices.Clone(r.Reads), Events: slices.Clone(r.Events), Tests: slices.Clone(r.Tests), Languages: cloneLanguages(r.Languages)}
}

func ContractRecordFromDeclaration(d ContractDefinition) ContractRecord {
	return ContractRecord{Name: d.Name, Major: d.Major, Title: d.Title, Description: d.Description, Actions: slices.Clone(d.Actions), Reads: slices.Clone(d.Reads), Events: slices.Clone(d.Events), Tests: slices.Clone(d.Tests), Languages: cloneLanguages(d.Languages)}
}

func (r ContractRecord) KeepsShape(next ContractRecord) bool {
	before, after := runtimeprotocol.ProtocolContract(r.Definition()), runtimeprotocol.ProtocolContract(next.Definition())
	return before.ID() == after.ID() && runtimeprotocol.CheckProtocol(before.Protocol(), after.Protocol()) == nil
}
