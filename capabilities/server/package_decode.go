package platformserver

import (
	"fmt"

	"platformserver/internal/acceptance"
)

// decodePackageContent reads the reviewed JSON of a platform package: one
// installable descriptor, without embedded native bytes.
func decodePackageContent(raw []byte) (PackageDescriptor, error) {
	var descriptor PackageDescriptor
	if err := acceptance.DecodeJSON(raw, &descriptor, "platform package"); err != nil {
		return PackageDescriptor{}, err
	}
	if descriptor.ID == "" && descriptor.Version == "" && descriptor.Namespace == "" {
		return PackageDescriptor{}, fmt.Errorf("The package file does not describe a platform package")
	}
	return descriptor, nil
}
