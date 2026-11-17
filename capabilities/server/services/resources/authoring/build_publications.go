package build

import (
	"encoding/json"
	"fmt"
	functionauthoring "platformserver/services/functions/authoring"

	"platformserver/platform"
	appsauthoring "platformserver/services/apps/authoring"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func (b *Build) AcceptedPublicationSchemas() []string {
	return acceptedPublicationSchemas()
}

func (b *Build) publicationImage(schema string, image []byte) (platform.Entity, []platform.Action, []platform.Page, error) {
	switch schema {
	case SchemaEvaluationSetPublish:
		_, _, err := b.testPlanImage(image)
		return b.testPlanEntity(), nil, nil, err
	case SchemaAgentPublish:
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, err
		}
		if _, ok := publication.Read[Agent](record.Published); !ok {
			return platform.Entity{}, nil, nil, fmt.Errorf("accepted agent has no published definition")
		}
		return b.agentEntity(), nil, nil, nil
	case SchemaPublish:
		var record ontology.Object
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, err
		}
		published, ok := publication.Read[ontology.Object](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name || record.Installed != TypeOf(published.Name) {
			return platform.Entity{}, nil, nil, fmt.Errorf("accepted object has no matching published definition")
		}
		if published.ExtensionSource != nil {
			if err := published.ExtensionSource.Check(published.Extends); err != nil {
				return platform.Entity{}, nil, nil, err
			}
		}
		entity := b.entity(published)
		portable, err := b.dispatchSchemaObject(published)
		if err != nil {
			return platform.Entity{}, nil, nil, err
		}
		if err := checkObjectSchemas(portable); err != nil {
			return platform.Entity{}, nil, nil, err
		}
		actions := objectActions(published, entity)
		return entity, actions, []platform.Page{appsauthoring.CompileObjectPage(published, ID)}, nil
	}
	return platform.Entity{}, nil, nil, fmt.Errorf("not an accepted builder publication: %s", schema)
}

func (b *Build) ValidateAcceptedPublication(schema string, image []byte) error {
	if hooks, found := publication.Schema(b.publications, schema); found {
		return hooks.Validate(image)
	}
	if b.apps.HandlesPublication(schema) {
		return b.apps.ValidateImage(schema, image)
	}
	if schema == SchemaFunction {
		return b.functions.ValidateImage(image)
	}
	if b.automation.HandlesPublication(schema) {
		return b.automation.ValidateImage(schema, image)
	}
	if schema == SchemaEvaluationSetPublish {
		_, _, err := b.testPlanImage(image)
		return err
	}
	if schema == SchemaTablePublish {
		tb, err := tableImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(platform.Caller{Replaying: true}, tb.Definition())
	}
	if schema == SchemaAgentPublish {
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		agent, ok := publication.Read[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		return b.checkAgent(agent)
	}

	if k := definitionKindOfSchema(schema); k != nil {
		return k.validateImage(b, image)
	}
	if schema == functionauthoring.SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(platform.Caller{Replaying: true}, code.Definition())
	}
	_, _, _, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record ontology.Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := publication.Read[ontology.Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		return b.validateObjectInstallation(platform.Caller{Replaying: true}, published)
	}
	return fmt.Errorf("unknown builder publication: %s", schema)
}

func (b *Build) ApplyAcceptedPublication(schema string, image []byte) error {
	if hooks, found := publication.Schema(b.publications, schema); found {
		return hooks.Install(image)
	}
	if b.apps.HandlesPublication(schema) {
		return b.apps.ApplyImage(schema, image)
	}
	if schema == SchemaFunction {
		return b.functions.ApplyImage(image)
	}
	if b.automation.HandlesPublication(schema) {
		return b.automation.ApplyImage(schema, image)
	}
	if schema == SchemaEvaluationSetPublish {
		_, _, err := b.testPlanImage(image)
		return err
	}
	if schema == SchemaTablePublish {
		tb, err := tableImage(image)
		if err != nil {
			return err
		}
		var record functionauthoring.Table
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		tb.Versions = record.Versions
		return b.installTable(platform.Caller{Replaying: true}, tb)
	}
	if schema == SchemaAgentPublish {
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		agent, ok := publication.Read[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		b.agents[agent.Name] = agent
		return nil
	}

	if k := definitionKindOfSchema(schema); k != nil {
		return k.applyImage(b, image)
	}
	if schema == functionauthoring.SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.installCode(platform.Caller{Replaying: true}, code)
	}
	entity, actions, pages, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record ontology.Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := publication.Read[ontology.Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		pages, _, err = b.objectInstallationPages(published)
		if err != nil {
			return err
		}
		if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
			return fmt.Errorf("extend accepted object: %w", err)
		}
		b.ledger.Catalog().ReplaceTarget(entity.Type, actions)
		if err := b.host.Install(platform.Caller{Replaying: true}, entity, actions, pages...); err != nil {
			return fmt.Errorf("install accepted object: %w", err)
		}
		b.installed[entity.Type] = entity
	}
	return nil
}

func published[T any](definition T) string {
	switch value := any(definition).(type) {
	case ontology.Object:
		value.Published = ""
		definition = any(value).(T)
	case ontology.PropertyType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case ontology.ValueType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case ontology.ObjectTypeGroup:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case ontology.LinkType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Query:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case functionauthoring.Function:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case functionauthoring.Code:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	}
	raw, _ := json.Marshal(definition)
	return string(raw)
}

func (b *Build) Reinstall() error {
	if err := propertyKind.restore(b); err != nil {
		return err
	}
	if err := valueKind.restore(b); err != nil {
		return err
	}
	if err := interfaceKind.restore(b); err != nil {
		return err
	}

	objects, _, _, err := b.releaseInventory()
	if err != nil {
		return err
	}
	ordered, err := businessVersionInstallOrder(objects)
	if err != nil {
		return err
	}
	beforeMode := b.jointMode
	b.jointMode = true
	defer func() { b.jointMode = beforeMode }()
	for _, was := range ordered {
		if err := b.install(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("object %s: %v: %s", was.Name, err, err.Message)
		}
	}
	b.jointMode = beforeMode
	if err := objectTypeGroupKind.restore(b); err != nil {
		return err
	}
	if err := linkKind.restore(b); err != nil {
		return err
	}
	if err := queryKind.restore(b); err != nil {
		return err
	}
	if err := b.functions.Restore(); err != nil {
		return err
	}

	agents, err := b.agentInventory()
	if err != nil {
		return err
	}
	for _, record := range agents {
		if a, ok := publication.Read[Agent](record.Published); ok && !record.Archived {
			b.agents[a.Name] = a
		}
	}
	codes, err := b.codeInventory()
	if err != nil {
		return err
	}
	for _, record := range codes {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		code, err := codeImage(raw)
		if err != nil {
			return err
		}
		if err := b.installCode(platform.Caller{Replaying: true}, code); err != nil {
			return err
		}
	}
	if err := b.apps.RestorePages(); err != nil {
		return err
	}
	if err := b.automation.Restore(); err != nil {
		return err
	}
	if err := b.installTables(); err != nil {
		return err
	}
	if err := b.apps.RestoreApplications(); err != nil {
		return err
	}
	return nil
}
