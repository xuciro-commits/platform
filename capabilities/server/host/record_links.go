package host

import records "platformserver/kernel/records"

import (
	"fmt"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) installPublicationLinkConstraint(store *records.Store, schema string, image []byte) error {
	if schema != build.SchemaLinkType {
		return nil
	}
	owner := t.App(build.ID)
	if owner == nil {
		return fmt.Errorf("link publication owner is unavailable")
	}
	declaration, err := owner.(platform.LinkPublicationReader).LinkPublication(image)
	if err != nil {
		return err
	}
	return store.InstallLinkConstraint(declaration)
}
