package authoring

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"strings"
	"sync"
)

type buildManifestCache struct {
	mu           sync.RWMutex
	gen          uint64
	baseEntities []platform.RecordType
	ready        bool
	entities     []platform.RecordType
	roles        []string
	sequences    []platform.Sequence
	provisions   []platform.Provision
	reads        []string
	readSources  map[string]string
}

func (b *Build) invalidateManifest() {
	if b.manifestCache == nil {
		return
	}
	b.manifestCache.mu.Lock()
	b.manifestCache.gen++
	b.manifestCache.ready = false
	b.manifestCache.mu.Unlock()
}

func (b *Build) cachedManifestState() ([]platform.RecordType, []string, []platform.Sequence, []platform.Provision, []string, map[string]string) {
	cache := b.manifestCache
	if cache == nil {
		entities := b.baseRecordTypes()
		for _, typ := range sortedTypes(b.installed) {
			entities = append(entities, b.installed[typ])
		}
		provisions, reads := b.objectProtocols()
		return entities, b.manifestRoles(), ontology.TransactionSequences(b.installed), provisions, append([]string{ReadReleaseProfile}, reads...), protocolReadSources(reads)
	}
	cache.mu.RLock()
	if cache.ready {
		entities, roles, sequences, provisions, reads, readSources := cache.entities, cache.roles, cache.sequences, cache.provisions, cache.reads, cache.readSources
		cache.mu.RUnlock()
		return entities, roles, sequences, provisions, reads, readSources
	}
	baseEntities := cache.baseEntities
	gen := cache.gen
	cache.mu.RUnlock()

	if baseEntities == nil {
		baseEntities = b.baseRecordTypes()
	}
	var entities []platform.RecordType
	if len(b.installed) == 0 {
		entities = baseEntities
	} else {
		entities = make([]platform.RecordType, len(baseEntities), len(baseEntities)+len(b.installed))
		copy(entities, baseEntities)
		for _, typ := range sortedTypes(b.installed) {
			entities = append(entities, b.installed[typ])
		}
	}
	provisions, reads := b.objectProtocols()
	roles := b.manifestRoles()
	sequences := ontology.TransactionSequences(b.installed)
	fullReads := append([]string{ReadReleaseProfile}, reads...)
	readSources := protocolReadSources(reads)

	cache.mu.Lock()
	if cache.baseEntities == nil {
		cache.baseEntities = baseEntities
	}
	if cache.gen == gen {
		cache.entities = entities
		cache.roles = roles
		cache.sequences = sequences
		cache.provisions = provisions
		cache.reads = fullReads
		cache.readSources = readSources
		cache.ready = true
	}
	cache.mu.Unlock()
	return entities, roles, sequences, provisions, fullReads, readSources
}

func (b *Build) baseRecordTypes() []platform.RecordType {
	return b.recordTypes("", true)
}

func (b *Build) recordTypes(schema string, manifest bool) []platform.RecordType {
	var entities []platform.RecordType
	if manifest {
		entities = make([]platform.RecordType, 0, 40)
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
	selectType(ContractType, func() platform.RecordType { return contractKind.entity(b) })
	selectType(ImplementationType, func() platform.RecordType { return implementationKind.entity(b) })
	selectType(ConnectorSetupType, b.connectorSetupEntity)
	selectType(CapabilityCallType, b.capabilityCallEntity)
	selectType(protocol.FunctionType, b.functions.Entity)
	selectType(protocol.FunctionCallType, b.functions.CallEntity)
	evaluation := b.evaluations.Entity()
	if matches(evaluation.Type) {
		add(evaluation)
	}
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
	if !manifest {
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
