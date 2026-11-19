package authoring

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"strings"
)

func (b *Build) recordTypes(schema string, manifest bool) []platform.RecordType {
	var entities []platform.RecordType
	if manifest {
		entities = make([]platform.RecordType, 0, 40+len(b.installed))
	}
	matches := func(typ string) bool { return manifest || strings.HasPrefix(schema, typ+".") }
	add := func(entity platform.RecordType) {
		if draft := BranchDraftType(entity.Type); draft {
			if entity.Scope.Levels == nil {
				entity.Scope.Levels = map[string]string{}
			}
			entity.Scope.Levels[protocol.ProjectViewerRole] = platform.ScopeTenant
			if schema == entity.Type+".archive" {
				entity.Validate = nil
			}
		}
		if entity.Type == protocol.AppType && (schema == protocol.AppType+".withdraw" || schema == protocol.AppType+".restore") {
			entity.Validate = nil
		}
		entities = append(entities, entity)
	}
	selectType := func(typ string, build func() platform.RecordType) {
		if matches(typ) {
			add(build())
		}
	}
	if manifest {
		add(protocol.RelationEntity(ID))
	}
	selectType(ObjectType, b.objectEntity)
	selectType(ActionTypeType, b.actionTypeEntity)
	page := b.apps.PageEntity
	if schema == protocol.PageType+".create" || schema == protocol.PageType+".edit" {
		page = b.apps.PageDraftEntity
	}
	selectType(protocol.PageType, page)
	selectType(protocol.AppType, b.apps.ApplicationEntity)
	selectType(TestPlanType, b.testPlanEntity)
	if manifest {
		add(b.automation.ProcessEntity())
	}
	selectType(PropertyTypeType, func() platform.RecordType { return propertyKind.entity(b) })
	selectType(ValueTypeType, func() platform.RecordType { return valueKind.entity(b) })
	selectType(ObjectTypeGroupType, b.objectTypeGroupEntity)
	selectType(LinkTypeType, func() platform.RecordType { return linkKind.entity(b) })
	selectType(QueryType, b.queries.Entity)
	selectType(InterfaceType, func() platform.RecordType { return interfaceKind.entity(b) })
	selectType(protocol.FunctionType, b.functions.Entity)
	selectType(protocol.FunctionCallType, b.functions.CallEntity)
	evaluation := b.evaluations.Entity()
	if matches(evaluation.Type) {
		add(evaluation)
	}
	agent := b.agent.Entity
	if schema == AgentType+".create" || schema == AgentType+".edit" {
		agent = b.agent.DraftEntity
	}
	selectType(AgentType, agent)
	if manifest {
		add(b.functions.CodeEntity())
		add(b.functions.TableEntity())
	}
	selectType(SampleSetType, b.sampleSets.Entity)
	selectType(BranchType, b.branchEntity)
	selectType(protocol.ConfirmationType, b.data.ConfirmationEntity)
	selectType(protocol.DispatchType, b.data.DispatchEntity)
	selectType(ProductType, b.productEntity)
	selectType(ProductVersionType, b.productVersionEntity)
	b.data.VisitEntities(matches, add)
	if matches(ResourceType) || matches(FolderType) {
		for _, entity := range b.resourceEntities() {
			if matches(entity.Type) {
				add(entity)
			}
		}
	}
	selectType(WorkingStateType, workingStateEntity)
	if manifest {
		for _, typ := range sortedTypes(b.installed) {
			entities = append(entities, b.installed[typ])
		}
	} else {
		for i := range len(schema) {
			if schema[i] == '.' {
				if entity, ok := b.installed[schema[:i]]; ok {
					if schema == entity.Type+".archive" {
						if _, err := draftAssetRef(entity.Type, ""); err == nil {
							entity.Validate = nil
						}
					}
					entities = append(entities, entity)
				}
			}
		}
	}
	return entities
}
