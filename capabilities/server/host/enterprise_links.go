package host

import (
	"encoding/json"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	build "platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"

	"platformserver/platform"
)

type enterpriseLinkModel interface{ Model() enterprisemodel.Model }

func (t *Tenant) tenantLinkModel() (enterprisemodel.Model, *platform.Error) {
	owner := t.App(enterprisemodel.ID)
	if owner == nil {
		return enterprisemodel.Model{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The enterprise model is unavailable")
	}
	return owner.(enterpriseLinkModel).Model(), nil
}

func (t *Tenant) tenantConnection(c platform.Caller, id string, now time.Time) (dataowner.Connection, *platform.Error) {
	c = platform.RouteCaller(c, build.ID)
	c.Automation = false
	permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: dataowner.ConnectionType, Record: id, Fields: []string{"kind", "address", "secret", "allowPrivate", "state", "published", "marking"}}, now)
	if err := authorizationRefusal(permission); err != nil {
		return dataowner.Connection{}, err
	}
	if len(permission.Withheld) > 0 {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The connection settings are not readable")
	}
	raw, refusal := c.ReadRecord(dataowner.ConnectionType, id, now)
	if refusal != nil {
		return dataowner.Connection{}, refusal
	}
	var connection dataowner.Connection
	if json.Unmarshal(raw, &connection) != nil {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connection settings are not readable")
	}
	owner := t.DataOwner()
	if owner == nil {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The connection owner is unavailable")
	}
	fixed, err := owner.ExecutionConnection(connection)
	if err != nil {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs a ready HTTP connection")
	}
	if refusal := dataowner.TenantSourceRefusal(fixed, func() string { return t.dataClient().Authorization(fixed) }); refusal != nil {
		return dataowner.Connection{}, refusal
	}
	return fixed, nil
}

func (t *Tenant) checkTenantConnection(c platform.Caller, id string, now time.Time) *platform.Error {
	_, err := t.tenantConnection(c, id, now)
	return err
}
