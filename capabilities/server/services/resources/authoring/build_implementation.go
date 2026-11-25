package authoring

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
)

var implementationKind = resourceKind[ontology.ImplementationRecord, ontology.ImplementationDefinition]{
	typ: ImplementationType, prefix: "implementation", noun: "implementation", title: "Capability implementation", plural: "Capability implementations", capability: "contracts", joint: true,
	description: "A connector that implements a capability contract by calling a third-party service over https from the host: its outbound origins, the credential it needs and one request per contract action. The tenant binds it after it passes the contract's conformance tests.",
	transition:  "Publish this capability implementation as its next retained version.",
	immutable:   "A published capability implementation keeps its name and the contract it implements",
	records:     func(b *Build) map[string]ontology.ImplementationRecord { return b.implementations },
	validate: func(b *Build, r ontology.ImplementationRecord) (ontology.ImplementationRecord, error) {
		implementation := protocol.ProtocolImplementation(r.Definition())
		if err := implementation.CheckPathTemplates(); err != nil {
			return r, err
		}
		return r, b.checkImplementation(implementation)
	},
	identity:        func(a, b ontology.ImplementationRecord) bool { return a.Name == b.Name && a.Contract == b.Contract },
	fromDeclaration: ontology.ImplementationRecordFromDeclaration,
	install:         func(protocol.BuildHost, platform.Caller, ontology.ImplementationDefinition, int) error { return nil },
	validateInstall: func(_ protocol.BuildHost, d ontology.ImplementationDefinition) error { return d.Check() },
	checkInstall: func(b *Build, d ontology.ImplementationDefinition) error {
		return b.checkImplementation(protocol.ProtocolImplementation(d))
	},
}

func (b *Build) checkImplementation(i protocol.ProtocolImplementation) error {
	if err := i.Check(); err != nil {
		return err
	}
	contract, declared := b.installedContract(i.Contract)
	if !declared {
		return fmt.Errorf("Implementation %s implements %s, which is not an installed capability contract", i.Name, i.Contract)
	}
	return protocol.CheckConnector(contract, i)
}

func (b *Build) checkContractImplementations(c protocol.ProtocolContract) error {
	for _, record := range b.implementations {
		if implementation := protocol.ProtocolImplementation(record.Definition()); implementation.Contract == c.ID() {
			if err := protocol.CheckConnector(c, implementation); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Build) ProtocolImplementations() []protocol.ProtocolImplementation {
	out := make([]protocol.ProtocolImplementation, 0, len(b.implementations))
	for _, record := range b.implementations {
		out = append(out, protocol.ProtocolImplementation(record.Definition()))
	}
	slices.SortFunc(out, func(x, y protocol.ProtocolImplementation) int { return strings.Compare(x.Name, y.Name) })
	return out
}
