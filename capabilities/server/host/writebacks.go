package host

import (
	dataowner "platformserver/services/data"

	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) integrationEffects(m platform.Member, now time.Time) ([]dataowner.IntegrationEffect, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	if !m.Holds(build.ID, build.Builder) && !m.Holds(build.ID, build.Integrator) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return dataowner.IntegrationEffects(t.Effects(now)), nil
}

func (t *Tenant) writebackEffects(e platform.Event, preparing bool) ([]platform.Effect, *kernel.Error) {
	if t.App(build.ID) == nil || e.App != build.ID {
		return nil, nil
	}
	return t.DataOwner().WritebackEffects(t.ID, t.automation(build.ID, false), e, preparing, t.checkWritebackCredential)
}

func (t *Tenant) checkWritebackCredential(request dataowner.WritebackRequest) *kernel.Error {
	if request.Secret != "" {
		if value, ok := t.CredentialValue(request.Secret, "connection/"+request.Connection, request.URL); !ok || strings.TrimSpace(string(value)) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The connection credential is not configured or permitted for this target")
		}
	}
	return nil
}
