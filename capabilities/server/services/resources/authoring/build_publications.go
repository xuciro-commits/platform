package build

import (
	"encoding/json"
	"fmt"
	functionauthoring "platformserver/services/functions/authoring"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func (b *Build) AcceptedPublicationSchemas() []string {
	return acceptedPublicationSchemas()
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

	if k := definitionKindOfSchema(schema); k != nil {
		return k.validateImage(b, image)
	}
	return fmt.Errorf("not an accepted builder publication: %s", schema)
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

	if k := definitionKindOfSchema(schema); k != nil {
		return k.applyImage(b, image)
	}
	return fmt.Errorf("not an accepted builder publication: %s", schema)
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
	case ontology.InterfaceRecord:
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
	case functionauthoring.Function:
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
	ordered, err := ontology.BusinessVersionInstallOrder(objects)
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
	if err := b.queries.Restore(); err != nil {
		return err
	}
	if err := b.functions.Restore(); err != nil {
		return err
	}

	if err := b.agent.Restore(); err != nil {
		return err
	}
	if err := b.functions.RestoreCodes(); err != nil {
		return err
	}
	if err := b.apps.RestorePages(); err != nil {
		return err
	}
	if err := b.automation.Restore(); err != nil {
		return err
	}
	if err := b.functions.RestoreTables(); err != nil {
		return err
	}
	if err := b.apps.RestoreApplications(); err != nil {
		return err
	}
	return nil
}
