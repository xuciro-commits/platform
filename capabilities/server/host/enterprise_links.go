package host

import (
	"encoding/json"
	"net/url"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"

	"platformserver/platform"
)

type enterpriseLinkModel interface{ Model() enterprisemodel.Model }

func (t *Tenant) tenantLinkModel() (enterprisemodel.Model, *kernel.Error) {
	owner, ok := t.app(enterprisemodel.ID).(enterpriseLinkModel)
	if !ok {
		return enterprisemodel.Model{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The enterprise model is unavailable")
	}
	return owner.Model(), nil
}

func (t *Tenant) tenantConnection(c platform.Caller, id string, now time.Time) (dataowner.Connection, *kernel.Error) {
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
	owner := t.dataOwner()
	if owner == nil {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The connection owner is unavailable")
	}
	fixed, err := owner.ExecutionConnection(connection)
	if err != nil || fixed.Archived || fixed.State != "ready" || fixed.Kind != "http" {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs a ready HTTP connection")
	}
	address, err := url.Parse(fixed.Address)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Scheme != "https" && !(address.Scheme == "http" && fixed.AllowPrivate) {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an HTTP service root")
	}
	if fixed.Secret == "" || t.authorization(fixed) == "" {
		return dataowner.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an available connection credential")
	}
	return fixed, nil
}

func (t *Tenant) checkTenantConnection(c platform.Caller, id string, now time.Time) *kernel.Error {
	_, err := t.tenantConnection(c, id, now)
	return err
}
