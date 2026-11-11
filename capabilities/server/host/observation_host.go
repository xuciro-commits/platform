package host

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

type observationSourceHost struct{ t *Tenant }

func (t *Tenant) observationSources() dataowner.Observations {
	return dataowner.NewObservations(observationSourceHost{t})
}

func (h observationSourceHost) caller(m platform.Member, app string) platform.Caller {
	return platform.NewCaller(runtime{t: h.t, scoped: true}, m, app, false, false)
}

func (h observationSourceHost) ObservationMember(m platform.Member) (platform.Member, *kernel.Error) {
	m = h.t.authorizationMember(m)
	return m, h.t.Admits(m)
}

func (h observationSourceHost) ObservationDefinitions(m platform.Member) ([]platform.Definition, error) {
	return h.t.observationDefinitions(m)
}

func (h observationSourceHost) ObservationSourceActive(record string) bool {
	return h.t.observationSourceActive(record)
}

func (h observationSourceHost) Entity(typ string) (platform.EntityInfo, bool) { return h.t.Entity(typ) }

func (h observationSourceHost) AuthorizeObservation(m platform.Member, app, permission, kind string, roles []string, at time.Time) *kernel.Error {
	return authorizationRefusal(h.t.authorizeNative(h.caller(m, app), permission, kind, roles, true, authorizationDetails{At: at}))
}

func (h observationSourceHost) AuthorizeRead(m platform.Member, app string, q platform.AuthzRequest, at time.Time) (bool, *kernel.Error) {
	decision := h.t.authorizeFrom(h.t.Projection, h.caller(m, app), q, at, false)
	return len(decision.Withheld) > 0, authorizationRefusal(decision)
}

func (h observationSourceHost) ReadRecord(m platform.Member, app, typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
	return h.t.readRecordFrom(h.t.Projection, h.caller(m, app), typ, id, at)
}

func (h observationSourceHost) ObservationConnections() ([]dataowner.Connection, int, *kernel.Error) {
	return platform.Find[dataowner.Connection](h.t.automation(build.ID, false), platform.Query{Limit: 500})
}

func (h observationSourceHost) DataOwner() *dataowner.Owner { return h.t.DataOwner() }

func (h observationSourceHost) CredentialHeader(secret, scope, address string) ([]byte, bool) {
	return h.t.CredentialHeader(secret, scope, address)
}

func (h observationSourceHost) ActionSchemaVersion(ref platform.AssetRef) (platform.SchemaVersion, error) {
	owner := h.t.App(ref.App)
	if owner == nil {
		return platform.SchemaVersion{}, fmt.Errorf("observation decision owner is unavailable")
	}
	action, exists := owner.Manifest().Actions.Action(ref.Name)
	if !exists {
		return platform.SchemaVersion{}, fmt.Errorf("observation decision action is unavailable")
	}
	return h.t.actionSchemaVersion(owner, action)
}

func (h observationSourceHost) Send(r *http.Request, allowPrivate bool) (*http.Response, error) {
	return h.t.sender()(r, allowPrivate)
}

func (h observationSourceHost) Rules() platform.RuleEngine { return h.t.services.Rules }
