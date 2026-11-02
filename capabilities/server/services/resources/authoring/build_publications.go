package build

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
)

func (*Build) AcceptedPublicationSchemas() []string {
	return []string{SchemaActionType, schemaAlertMigrate, SchemaPublish, SchemaRelease, SchemaHandOver, SchemaProcess, SchemaPropertyType, SchemaValueType, SchemaObjectTypeGroup, SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish, SchemaAgentPublish, SchemaTablePublish, SchemaAlertPublish, SchemaEvaluationSetPublish}
}

func (b *Build) publicationImage(schema string, image []byte) (platform.Entity, []platform.Action, []platform.Page, *platform.Application, error) {
	switch schema {
	case SchemaEvaluationSetPublish:
		_, _, err := b.testPlanImage(image)
		return b.testPlanEntity(), nil, nil, nil, err
	case SchemaAgentPublish:
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		if _, ok := ReadPublication[Agent](record.Published); !ok {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted agent has no published definition")
		}
		return b.agentEntity(), nil, nil, nil, nil
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := ReadPublication[Object](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name || record.Installed != TypeOf(published.Name) {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted object has no matching published definition")
		}
		if published.ExtensionSource != nil {
			if err := published.ExtensionSource.Check(published.Extends); err != nil {
				return platform.Entity{}, nil, nil, nil, err
			}
		}
		entity := b.entity(published)
		portable, err := b.dispatchSchemaObject(published)
		if err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		if err := checkObjectSchemas(portable); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		actions := objectActions(published, entity)
		return entity, actions, []platform.Page{CompileObjectPage(published)}, nil, nil
	case SchemaRelease:
		var record Page
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := ReadPublication[Page](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted page has no published definition")
		}
		return platform.Entity{}, nil, []platform.Page{descriptor(published)}, nil, nil
	case SchemaHandOver:
		var record Application
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := ReadPublication[Application](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted application has no published definition")
		}
		app := applicationDescriptor(published)
		return platform.Entity{}, nil, nil, &app, nil
	}
	return platform.Entity{}, nil, nil, nil, fmt.Errorf("not an accepted builder publication: %s", schema)
}

func (b *Build) ValidateAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaActionType {
		return actionTypeImage(image)
	}
	if schema == schemaAlertMigrate {
		schema = SchemaProcess
	}
	if schema == SchemaAlertPublish {
		_, asset, err := alertImage(image)
		if err != nil {
			return err
		}
		alert, _, err := alertFromAsset(asset)
		if err != nil {
			return err
		}
		return b.checkAlert(alert)
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
		agent, ok := ReadPublication[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		return b.checkAgent(agent)
	}

	if k := definitionKindOfSchema(schema); k != nil {
		return k.validateImage(b, image)
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(platform.Caller{Replaying: true}, code.Definition())
	}
	if schema == SchemaProcess {
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if b.host.BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return b.host.BuildProcesses().Validate(b, b.flowOf(p))
	}
	_, _, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := ReadPublication[Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		return b.validateObjectInstallation(platform.Caller{Replaying: true}, published)
	case SchemaRelease:
		return b.host.ValidateInstallPage(pages[0])
	case SchemaHandOver:
		return b.host.ValidateInstallApplication(*app)
	}
	return fmt.Errorf("unknown builder publication: %s", schema)
}

func (b *Build) ApplyAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaActionType {
		return actionTypeImage(image)
	}
	if schema == schemaAlertMigrate {
		schema = SchemaProcess
	}
	if schema == SchemaAlertPublish {
		_, _, err := alertImage(image)
		return err
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
		var record Table
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
		agent, ok := ReadPublication[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		b.agents[agent.Name] = agent
		return nil
	}

	if k := definitionKindOfSchema(schema); k != nil {
		return k.applyImage(b, image)
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.installCode(platform.Caller{Replaying: true}, code)
	}
	if schema == SchemaProcess {
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if b.host.BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return b.host.BuildProcesses().Install(b, b.flowOf(p))
	}
	entity, actions, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := ReadPublication[Object](record.Published)
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
		b.ledger.Catalog.ReplaceTarget(entity.Type, actions)
		if err := b.host.Install(platform.Caller{Replaying: true}, entity, actions, pages...); err != nil {
			return fmt.Errorf("install accepted object: %w", err)
		}
		b.installed[entity.Type] = entity
	case SchemaRelease:
		if err := b.host.InstallPage(platform.Caller{Replaying: true}, pages[0]); err != nil {
			return fmt.Errorf("install accepted page: %w", err)
		}
	case SchemaHandOver:
		if err := b.host.InstallApplication(platform.Caller{Replaying: true}, *app); err != nil {
			return fmt.Errorf("install accepted application: %w", err)
		}
	}
	return nil
}

func published[T any](definition T) string {
	switch value := any(definition).(type) {
	case Object:
		value.Published = ""
		definition = any(value).(T)
	case Page:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Application:
		value.Published = ""
		definition = any(value).(T)
	case PropertyType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case ValueType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case ObjectTypeGroup:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case LinkType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Query:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Function:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Code:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Process:
		value.Paused = false
		value.PauseReason = ""
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

	objects, pages, applications, err := b.releaseInventory()
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
	if err := functionKind.restore(b); err != nil {
		return err
	}

	agents, err := b.agentInventory()
	if err != nil {
		return err
	}
	for _, record := range agents {
		if a, ok := ReadPublication[Agent](record.Published); ok && !record.Archived {
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
	for _, p := range pages {
		was, ok := ReadPublication[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.release(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("page %s: %v", p.Name, err)
		}
	}
	for _, p := range pages {
		was, ok := ReadPublication[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.host.ValidateInstallPage(descriptor(was)); err != nil {
			return fmt.Errorf("page %s navigation: %w", p.Name, err)
		}
	}
	if err := b.installProcesses(); err != nil {
		return err
	}
	if err := b.installTables(); err != nil {
		return err
	}
	for _, a := range applications {
		was, ok := ReadPublication[Application](a.Published)
		if !ok || a.Archived {
			continue
		}
		if err := b.hand(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("application %s: %v", a.Name, err)
		}
	}
	return nil
}
