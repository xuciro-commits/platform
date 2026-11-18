package data

import (
	"net/url"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func DescribeConnection(raw []byte) (ConnectionDefinition, error) {
	var definition ConnectionDefinition
	err := ValueDecodeIntegration(raw, &definition)
	return definition, err
}

func ConnectionImpact(store *records.Store, id string) (platform.PackageImpactConnection, bool, error) {
	record := store.Type(ConnectionType).Rows().Get(id)
	if record == nil {
		return platform.PackageImpactConnection{}, false, nil
	}
	connection := record.Value.Interface().(Connection)
	var definition ConnectionDefinition
	if connection.Published != "" {
		var accepted platform.ReleaseAsset
		if err := ValueDecodeIntegration([]byte(connection.Published), &accepted); err != nil {
			return platform.PackageImpactConnection{}, false, err
		}
		var err error
		definition, err = DescribeConnection(accepted.Body)
		if err != nil {
			return platform.PackageImpactConnection{}, false, err
		}
	}
	return platform.PackageImpactConnection{Configured: connection.Address != "" && (!definition.Credential || connection.Secret != ""), Credential: definition.Credential}, true, nil
}

func TenantSourceRefusal(fixed Connection, credential func() string) *platform.Error {
	if fixed.Archived || fixed.State != "ready" || fixed.Kind != "http" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs a ready HTTP connection")
	}
	address, err := url.Parse(fixed.Address)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Scheme != "https" && !(address.Scheme == "http" && fixed.AllowPrivate) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an HTTP service root")
	}
	if fixed.Secret == "" || credential() == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an available connection credential")
	}
	return nil
}
