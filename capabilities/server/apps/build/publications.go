package build

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func (*Build) AcceptedPublicationSchemas() []string {
	return []string{construction.SchemaPublish, construction.SchemaRelease, construction.SchemaHandOver, SchemaPropertyType, construction.SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish, SchemaAgentPublish, SchemaTablePublish, SchemaAlertPublish}
}

// publicationImage reads and verifies the already committed record image
// without rerunning builder action rules or publishing a second decision.
func (b *Build) publicationImage(schema string, image []byte) (platform.Entity, []platform.Action, []platform.Page, *platform.Application, error) {
	switch schema {
	case SchemaAgentPublish:
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		if _, ok := construction.ReadPublication[Agent](record.Published); !ok {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted agent has no published definition")
		}
		return b.agentEntity(), nil, nil, nil, nil
	case construction.SchemaPublish:
		var record construction.Object
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := construction.ReadPublication[construction.Object](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name || record.Installed != construction.TypeOf(published.Name) {
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
		return entity, actions, []platform.Page{construction.CompileObjectPage(published)}, nil, nil
	case construction.SchemaRelease:
		var record Page
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := construction.ReadPublication[Page](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted page has no published definition")
		}
		return platform.Entity{}, nil, []platform.Page{descriptor(published)}, nil, nil
	case construction.SchemaHandOver:
		var record Application
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := construction.ReadPublication[Application](record.Published)
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
		agent, ok := construction.ReadPublication[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		return b.checkAgent(agent)
	}

	if schema == SchemaPropertyType {
		l, err := propertyTypeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallPropertyType(l.Definition())
	}
	if schema == construction.SchemaLinkType {
		l, err := linkTypeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallLinkType(l.Definition())
	}
	if schema == SchemaQuery {
		q, err := queryImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallQuery(q.Definition())
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(platform.Caller{Replaying: true}, code.Definition())
	}
	if schema == SchemaFunction {
		f, err := functionImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallFunction(f.Definition())
	}
	_, _, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case construction.SchemaPublish:
		var record construction.Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := construction.ReadPublication[construction.Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		return b.validateObjectInstallation(platform.Caller{Replaying: true}, published)
	case construction.SchemaRelease:
		return b.host.ValidateInstallPage(pages[0])
	case construction.SchemaHandOver:
		return b.host.ValidateInstallApplication(*app)
	}
	return fmt.Errorf("unknown builder publication: %s", schema)
}

// Installation reconstructs the runtime registry from the saved published
// image, not by running the publish transition or making another journal entry.
func (b *Build) ApplyAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaAlertPublish {
		_, _, err := alertImage(image)
		return err
	}
	if schema == SchemaTablePublish {
		tb, err := tableImage(image)
		if err != nil {
			return err
		}
		var record construction.Table
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
		agent, ok := construction.ReadPublication[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		b.agents[agent.Name] = agent
		return nil
	}

	if schema == SchemaPropertyType {
		return applyDefinitionHistory(image, propertyTypeImage, b.installPropertyType)
	}
	if schema == construction.SchemaLinkType {
		return applyDefinitionHistory(image, linkTypeImage, b.installLinkType)
	}
	if schema == SchemaQuery {
		return applyDefinitionHistory(image, queryImage, b.installQuery)
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.installCode(platform.Caller{Replaying: true}, code)
	}
	if schema == SchemaFunction {
		f, err := functionImage(image)
		if err != nil {
			return err
		}
		return b.installFunction(platform.Caller{Replaying: true}, f)
	}
	entity, actions, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case construction.SchemaPublish:
		var record construction.Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := construction.ReadPublication[construction.Object](record.Published)
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
	case construction.SchemaRelease:
		if err := b.host.InstallPage(platform.Caller{Replaying: true}, pages[0]); err != nil {
			return fmt.Errorf("install accepted page: %w", err)
		}
	case construction.SchemaHandOver:
		if err := b.host.InstallApplication(platform.Caller{Replaying: true}, *app); err != nil {
			return fmt.Errorf("install accepted application: %w", err)
		}
	}
	return nil
}

// published is a definition as it was published: what is installed, kept on the
// record so a restore installs that and not a draft written since.
func published[T any](definition T) string {
	// A published definition is one version, not a recursive copy of every
	// preceding publication. Keeping the old Published string inside the new
	// image would double its size at each edit and exhaust the bounded result.
	switch value := any(definition).(type) {
	case construction.Object:
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
	case LinkType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Query:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case construction.Function:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Code:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	}
	raw, _ := json.Marshal(definition)
	return string(raw)
}

// Reinstall installs every published object and page again, objects first: the
// host calls it when a tenant is restored from a snapshot, before the records
// of the types they define. It installs what was published, so a
// draft written since stays a draft.
func (b *Build) Reinstall() error {
	if err := restoreDefinitionPublications(b, b.propertyTypeInventory, SchemaPropertyType); err != nil {
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
	if err := restoreDefinitionPublications(b, b.linkTypeInventory, construction.SchemaLinkType); err != nil {
		return err
	}
	if err := restoreDefinitionPublications(b, b.queryInventory, SchemaQuery); err != nil {
		return err
	}
	if err := restoreDefinitionPublications(b, b.functionInventory, SchemaFunction); err != nil {
		return err
	}

	agents, err := b.agentInventory()
	if err != nil {
		return err
	}
	for _, record := range agents {
		if a, ok := construction.ReadPublication[Agent](record.Published); ok && !record.Archived {
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
	for _, p := range pages { // after the objects they show
		was, ok := construction.ReadPublication[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.release(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("page %s: %v", p.Name, err)
		}
	}
	// Navigation may be reciprocal or refer to a later page ID. All original
	// descriptors must exist before the same strict live target/interface check.
	for _, p := range pages {
		was, ok := construction.ReadPublication[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.host.ValidateInstallPage(descriptor(was)); err != nil {
			return fmt.Errorf("page %s navigation: %w", p.Name, err)
		}
	}
	if err := b.installTables(); err != nil {
		return err
	}
	for _, a := range applications { // after the pages they hold
		was, ok := construction.ReadPublication[Application](a.Published)
		if !ok || a.Archived {
			continue
		}
		if err := b.hand(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("application %s: %v", a.Name, err)
		}
	}
	return nil
}
