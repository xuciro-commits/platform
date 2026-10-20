package build

import "platformserver/internal/construction"

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func protocolReadName(typ, name string) string { return "protocol:" + typ + ":" + name }

func objectProvisions(object construction.Object, entity platform.Entity) []platform.Provision {
	if len(object.Protocols) == 0 {
		return nil
	}
	catalog := platform.NewCatalog(platform.EntityActions(entity)...)
	var out []platform.Provision
	for _, declaration := range object.Protocols {
		p := platform.Provision{Protocol: platform.Protocol{Name: declaration.Name, Version: declaration.Version, Reads: slices.Clone(declaration.Reads)}, Actions: map[string]string{}, Reads: map[string]string{}, Events: map[string]string{}}
		verbs := make([]string, 0, len(declaration.Actions))
		for verb := range declaration.Actions {
			verbs = append(verbs, verb)
		}
		slices.Sort(verbs)
		for _, verb := range verbs {
			schema := entity.Type + "." + declaration.Actions[verb]
			action, ok := catalog.Action(schema)
			if !ok {
				continue // The owner validator reports missing action bindings.
			}
			action.Schema, action.Roles, action.Automation = verb, nil, false
			p.Protocol.Actions = append(p.Protocol.Actions, action)
			p.Actions[verb] = schema
		}
		for _, read := range declaration.Reads {
			p.Reads[read] = protocolReadName(entity.Type, read)
		}
		verbs = verbs[:0]
		for event := range declaration.Events {
			verbs = append(verbs, event)
		}
		slices.Sort(verbs)
		for _, event := range verbs {
			p.Events[event] = entity.Type + "." + declaration.Events[event]
			p.Protocol.Events = append(p.Protocol.Events, platform.ProtocolEvent{Name: event, Title: event})
		}
		if declaration.Contract != nil {
			p.Protocol = *declaration.Contract
		}
		out = append(out, p)
	}
	return out
}

func (b *Build) checkObjectProtocols(object construction.Object) error {
	if len(object.Protocols) == 0 {
		return nil
	}
	if len(object.Protocols) > 16 {
		return fmt.Errorf("An object supports at most 16 protocol bindings")
	}
	if err := checkProcess(object, b.conditionLookup); err != nil {
		return err
	}
	draft := object
	draft.Protocols = slices.Clone(object.Protocols)
	for i := range draft.Protocols {
		draft.Protocols[i].Contract = nil
	}
	declaration := draft
	declaration.Protocols = nil
	entity := Entity(declaration)
	if _, err := platform.Describe(construction.ID, entity, func(reflect.Type) string { return "?" }); err != nil {
		return err
	}
	catalog := platform.NewCatalog(platform.EntityActions(entity)...)
	generated := objectProvisions(draft, entity)
	seen := map[string]bool{}
	for i, declaration := range object.Protocols {
		actual := generated[i].Protocol
		id := actual.ID()
		if declaration.Name == "" || strings.ContainsAny(declaration.Name, "/# :\t\n") || declaration.Version < 1 || seen[id] || len(declaration.Actions) == 0 {
			return fmt.Errorf("A protocol binding needs a unique name, positive version and declared actions")
		}
		seen[id] = true
		for _, bindings := range []map[string]string{declaration.Actions, declaration.Events} {
			for verb, action := range bindings {
				_, exists := catalog.Action(entity.Type + "." + action)
				if !named(verb) || !exists {
					return fmt.Errorf("Protocol %s maps %s to an undeclared object action", id, verb)
				}
			}
		}
		reads := map[string]bool{}
		for _, read := range declaration.Reads {
			if !named(read) || reads[read] {
				return fmt.Errorf("Protocol reads must be unique named record projections")
			}
			reads[read] = true
		}
		if declaration.Contract != nil {
			if err := platform.CheckProtocol(*declaration.Contract, actual); err != nil {
				return err
			}
		}
		for typ, installed := range b.installed {
			for _, provision := range installed.Protocols {
				if provision.Protocol.ID() != id {
					continue
				}
				if typ != entity.Type {
					return fmt.Errorf("Protocol %s is already provided by %s", id, typ)
				}
				if err := platform.CheckProtocol(provision.Protocol, actual); err != nil {
					return err
				}
			}
		}
		if host, ok := b.host.(interface {
			ProtocolContract(string) (platform.Protocol, bool)
		}); ok {
			if contract, exists := host.ProtocolContract(id); exists {
				if err := platform.CheckProtocol(contract, actual); err != nil {
					return err
				}
			}
		}
	}
	return nil
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

func (b *Build) readProtocol(c platform.Caller, name string) (any, *kernel.Error) {
	for _, typ := range sortedTypes(b.installed) {
		entity := b.installed[typ]
		for _, provision := range entity.Protocols {
			for _, read := range provision.Reads {
				if read != name {
					continue
				}
				if owner, ok := b.host.(interface {
					ReadProtocolRecords(platform.Caller, string) ([]any, *kernel.Error)
				}); ok {
					return owner.ReadProtocolRecords(c, typ)
				}
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Protocol records require the original member read owner")
			}
		}
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
