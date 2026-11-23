package authoring

import (
	"slices"
	"strings"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
)

var contractKind = resourceKind[ontology.ContractRecord, ontology.ContractDefinition]{
	typ: ContractType, prefix: "contract", noun: "contract", title: "Capability contract", plural: "Capability contracts", capability: "contracts", joint: true,
	description: "A versioned interface of a capability: its actions, reads and events. Objects implement it through protocol bindings; the tenant chooses which implementation serves it.",
	transition:  "Publish this capability contract as its next retained version.",
	immutable:   "A published capability contract keeps its name, major version, actions and their parameters, reads and events; a breaking change needs a new contract name",
	records:     func(b *Build) map[string]ontology.ContractRecord { return b.contracts },
	validate: func(b *Build, c ontology.ContractRecord) (ontology.ContractRecord, error) {
		return c, b.checkContract(protocol.ProtocolContract(c.Definition()), true)
	},
	identity:        func(a, b ontology.ContractRecord) bool { return a.Name == b.Name && a.Major == b.Major },
	stable:          func(a, b ontology.ContractRecord) bool { return a.KeepsShape(b) },
	guarantees:      func(a, b ontology.ContractRecord) bool { return a.KeepsShape(b) },
	fromDeclaration: ontology.ContractRecordFromDeclaration,
	install:         func(protocol.BuildHost, platform.Caller, ontology.ContractDefinition, int) error { return nil },
	validateInstall: func(_ protocol.BuildHost, d ontology.ContractDefinition) error { return d.Check() },
	checkInstall: func(b *Build, d ontology.ContractDefinition) error {
		return b.checkContract(protocol.ProtocolContract(d), false)
	},
}

func (b *Build) checkContract(c protocol.ProtocolContract, providers bool) error {
	if err := c.Check(); err != nil {
		return err
	}
	provisions, _ := b.objectProtocols()
	for _, provision := range provisions {
		if provision.Protocol.ID() == c.ID() {
			if err := protocol.CheckContractImplementation(c, provision.Protocol); err != nil {
				return err
			}
		}
	}
	if host, ok := b.host.(interface {
		ProtocolContract(string) (platform.Protocol, bool)
	}); ok && providers {
		if implemented, found := host.ProtocolContract(c.ID()); found {
			return protocol.CheckContractImplementation(c, implemented)
		}
	}
	return nil
}

func (b *Build) ProtocolContracts() []protocol.ProtocolContract {
	out := make([]protocol.ProtocolContract, 0, len(b.contracts))
	for _, record := range b.contracts {
		out = append(out, protocol.ProtocolContract(record.Definition()))
	}
	slices.SortFunc(out, func(x, y protocol.ProtocolContract) int { return strings.Compare(x.ID(), y.ID()) })
	return out
}

func (b *Build) installedContract(id string) (protocol.ProtocolContract, bool) {
	for _, contract := range b.ProtocolContracts() {
		if contract.ID() == id {
			return contract, true
		}
	}
	return protocol.ProtocolContract{}, false
}

func (b *Build) checkImplementedContracts(entity platform.RecordType) error {
	for _, provision := range entity.Protocols {
		if contract, declared := b.installedContract(provision.Protocol.ID()); declared {
			if err := protocol.CheckContractImplementation(contract, provision.Protocol); err != nil {
				return err
			}
		}
	}
	return nil
}
