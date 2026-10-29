package platformserver

import (
	"encoding/json"
	"net/url"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	enterprisemodel "platformserver/engine/organization"
	"platformserver/studio/build"

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

// The caller keeps its original runtime, so a staged import reads the same
// authorized connection record as its other original owner decisions.
func (t *Tenant) tenantConnection(c platform.Caller, id string, now time.Time) (build.Connection, *kernel.Error) {
	c = platform.RouteCaller(c, build.ID)
	c.Automation = false
	permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: build.ConnectionType, Record: id, Fields: []string{"kind", "address", "secret", "allowPrivate", "state", "published", "marking"}}, now)
	if err := authorizationRefusal(permission); err != nil {
		return build.Connection{}, err
	}
	if len(permission.Withheld) > 0 {
		return build.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The connection settings are not readable")
	}
	raw, refusal := c.ReadRecord(build.ConnectionType, id, now)
	if refusal != nil {
		return build.Connection{}, refusal
	}
	var connection build.Connection
	if json.Unmarshal(raw, &connection) != nil {
		return build.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connection settings are not readable")
	}
	owner, ok := t.app(build.ID).(build.IntegrationDefinitions)
	if !ok {
		return build.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The connection owner is unavailable")
	}
	fixed, err := owner.ExecutionConnection(connection)
	if err != nil || fixed.Archived || fixed.State != "ready" || fixed.Kind != "http" {
		return build.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs a ready HTTP connection")
	}
	address, err := url.Parse(fixed.Address)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Scheme != "https" && !(address.Scheme == "http" && fixed.AllowPrivate) {
		return build.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an HTTP service root")
	}
	if fixed.Secret == "" || t.authorization(fixed) == "" {
		return build.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an available connection credential")
	}
	return fixed, nil
}

func (t *Tenant) checkTenantConnection(c platform.Caller, id string, now time.Time) *kernel.Error {
	_, err := t.tenantConnection(c, id, now)
	return err
}
