package authoring

import (
	"platformserver/kernel/protocol"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
	"platformserver/services/ontology"
)

func (b *Build) objectProvisions(object ontology.Object, entity platform.RecordType) []platform.Provision {
	return ontology.ObjectProvisions(object, entity, actioncatalog.EntityActions(entity))
}

func (b *Build) checkObjectProtocols(object ontology.Object) error {
	if err := b.checkUses(object); err != nil {
		return err
	}
	return ontology.CheckObjectProtocols(object, ontology.ProtocolEnvironment{
		Actions: actioncatalog.EntityActions,
		Installed: func() []platform.RecordType {
			out := make([]platform.RecordType, 0, len(b.installed))
			for _, typ := range sortedTypes(b.installed) {
				out = append(out, b.installed[typ])
			}
			return out
		},
		Contract: func(id string) (platform.Protocol, bool) {
			if contract, declared := b.installedContract(id); declared {
				return contract.Protocol(), true
			}
			if b.host == nil {
				return platform.Protocol{}, false
			}
			if host, ok := b.host.(interface {
				ProtocolContract(string) (platform.Protocol, bool)
			}); ok {
				return host.ProtocolContract(id)
			}
			return platform.Protocol{}, false
		},
		CheckProcess: func(value ontology.Object) error {
			return ontology.CheckProcess(value, ID, value.Type(), Builder, User, protocol.FunctionCallType, b.conditionLookup)
		},
	})
}

func (b *Build) consumedProtocols() []platform.Consumption {
	out := []platform.Consumption{}
	if b.host != nil {
		out = b.host.BuildProcessConsumptions()
	}

	for _, entity := range b.installed {
		if entity.Lifecycle == nil {
			continue
		}
		for _, action := range entity.Lifecycle.Actions {
			for _, rule := range action.Rules {
				if rule.Transaction == nil || rule.Transaction.Spec == nil || rule.Transaction.Spec.Confirmation == nil {
					continue
				}
				for _, party := range rule.Transaction.Spec.Confirmation.Parties {
					if party.Protocol != "" && !slices.ContainsFunc(out, func(c platform.Consumption) bool { return c.Protocol == party.Protocol }) {
						out = append(out, platform.Consumption{Protocol: party.Protocol, Optional: true})
					}
				}
			}
		}
	}
	if b.authority != nil {
		for action := range b.authority.Catalog().Actions() {
			for _, used := range action.Uses {
				protocol, _, _ := strings.Cut(used, "#")
				if !slices.ContainsFunc(out, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
					out = append(out, platform.Consumption{Protocol: protocol, Optional: true})
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b platform.Consumption) int { return strings.Compare(a.Protocol, b.Protocol) })
	return out
}

func (b *Build) objectProtocols() ([]platform.Provision, []string) {
	var provisions []platform.Provision
	var reads []string
	for _, typ := range sortedTypes(b.installed) {
		for _, provision := range b.installed[typ].Protocols {
			provisions = append(provisions, provision)
			for _, read := range provision.Protocol.Reads {
				reads = append(reads, provision.Reads[read])
			}
		}
	}
	return provisions, reads
}

func protocolReadSources(reads []string) map[string]string {
	sources := map[string]string{ReadReleaseProfile: "record"}
	for _, read := range reads {
		sources[read] = "record"
	}
	return sources
}

func (b *Build) readProtocol(c platform.Caller, name string) (any, *platform.Error) {
	for _, typ := range sortedTypes(b.installed) {
		entity := b.installed[typ]
		for _, provision := range entity.Protocols {
			for _, read := range provision.Reads {
				if read != name {
					continue
				}
				if owner, ok := b.host.(interface {
					ReadProtocolRecords(platform.Caller, string) ([]any, *platform.Error)
				}); ok {
					return owner.ReadProtocolRecords(c, typ)
				}
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Protocol records require the original member read owner")
			}
		}
	}
	return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
