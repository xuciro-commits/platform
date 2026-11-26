package ontology

import (
	"fmt"
	runtimeprotocol "platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type ProtocolEnvironment struct {
	Actions      func(platform.RecordType) []platform.Action
	Installed    func() []platform.RecordType
	Contract     func(string) (platform.Protocol, bool)
	CheckProcess func(Object) error
}

func ProtocolReadName(typ, name string) string { return "protocol:" + typ + ":" + name }

func ObjectProvisions(object Object, entity platform.RecordType, actions []platform.Action) []platform.Provision {
	if len(object.Protocols) == 0 {
		return nil
	}
	catalog := platform.NewCatalog(actions...)
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
				continue
			}
			action.Schema, action.Roles, action.Automation = verb, nil, false
			p.Protocol.Actions = append(p.Protocol.Actions, action)
			p.Actions[verb] = schema
		}
		for _, read := range declaration.Reads {
			p.Reads[read] = ProtocolReadName(entity.Type, read)
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

func CheckObjectProtocols(object Object, env ProtocolEnvironment) error {
	if len(object.Protocols) == 0 {
		return nil
	}
	if len(object.Protocols) > 16 {
		return fmt.Errorf("An object supports at most 16 protocol bindings")
	}
	if err := env.CheckProcess(object); err != nil {
		return err
	}
	draft := object
	draft.Protocols = slices.Clone(object.Protocols)
	for i := range draft.Protocols {
		draft.Protocols[i].Contract = nil
	}
	declaration := draft
	declaration.Protocols = nil
	entity := CompileObjectEntity(declaration, ObjectRuntimeEnvironment{})
	if _, err := records.Describe(AppID, entity, func(reflect.Type) string { return "?" }); err != nil {
		return err
	}
	catalog := platform.NewCatalog(env.Actions(entity)...)
	generated := ObjectProvisions(draft, entity, env.Actions(entity))
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
				if !publication.ValidName(verb) || !exists {
					return fmt.Errorf("Protocol %s maps %s to an undeclared object action", id, verb)
				}
			}
		}
		reads := map[string]bool{}
		for _, read := range declaration.Reads {
			if !publication.ValidName(read) || reads[read] {
				return fmt.Errorf("Protocol reads must be unique named record projections")
			}
			reads[read] = true
		}
		if declaration.Contract != nil {
			if err := runtimeprotocol.CheckProtocol(*declaration.Contract, actual); err != nil {
				return err
			}
		}
		for _, installed := range env.Installed() {
			for _, provision := range installed.Protocols {
				if provision.Protocol.ID() != id {
					continue
				}
				if err := runtimeprotocol.CheckProtocol(provision.Protocol, actual); err != nil {
					if installed.Type != entity.Type {
						return fmt.Errorf("Protocol %s is also provided by %s with a different shape: %w", id, installed.Type, err)
					}
					return err
				}
			}
		}
		if contract, exists := env.Contract(id); exists {
			if err := runtimeprotocol.CheckProtocol(contract, actual); err != nil {
				return err
			}
		}
	}
	return nil
}
