package host

import (
	"platformserver/services/actions"
	dataowner "platformserver/services/data"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type IntegrationEffect struct {
	ID       string    `json:"id"`
	Endpoint string    `json:"endpoint"`
	Event    string    `json:"event"`
	State    string    `json:"state"`
	Due      time.Time `json:"due"`
	Last     time.Time `json:"last,omitzero"`
	Error    string    `json:"error,omitempty"`
}

func (t *Tenant) integrationEffects(m platform.Member, now time.Time) ([]IntegrationEffect, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	if !m.Holds(build.ID, build.Builder) && !m.Holds(build.ID, build.Integrator) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	out := []IntegrationEffect{}
	for _, effect := range t.Effects(now) {
		writeback := strings.HasPrefix(effect.Event, "writeback/") && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		confirmation := effect.Event == build.ID+"/confirmation" && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		dispatch := effect.Event == build.ID+"/dispatch" && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		transaction := effect.Event == build.ID+"/transaction" && strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint)
		if effect.App != build.ID || !writeback && !confirmation && !dispatch && !transaction {
			continue
		}
		item := IntegrationEffect{ID: effect.ID, Endpoint: effect.Endpoint, Event: effect.Event, State: effect.State, Due: effect.Due, Last: effect.Last}
		if effect.Error != "" {
			item.Error = "The last delivery attempt failed"
			if effect.PublicMessage != "" {
				item.Error = effect.PublicMessage
			}
		}
		out = append(out, item)
	}
	return out, nil
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

func (t *Tenant) connectionEndpoints() []actions.Endpoint {
	var out []actions.Endpoint
	for _, x := range t.outbound.Select(nil) {
		if !strings.HasPrefix(x.Endpoint, dataowner.WritebackEndpoint) || platform.EffectSettled(x.State) {
			continue
		}
		if !slices.ContainsFunc(out, func(ep actions.Endpoint) bool { return ep.ID == x.Endpoint }) {
			out = append(out, actions.Endpoint{ID: x.Endpoint, Kind: "connection"})
		}
	}
	return out
}
