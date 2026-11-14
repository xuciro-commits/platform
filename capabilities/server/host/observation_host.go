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

func (t *Tenant) observationSources() dataowner.Observations {
	caller := func(m platform.Member, app string) platform.Caller {
		return platform.NewCaller(runtime{t: t, scoped: true}, m, app, false, false)
	}
	return dataowner.NewObservations(dataowner.ObservationHost{
		ObservationMember: func(m platform.Member) (platform.Member, *kernel.Error) {
			m = t.authorizationMember(m)
			return m, t.Admits(m)
		},
		ObservationDefinitions:  t.observationDefinitions,
		ObservationSourceActive: func(record string) bool { return t.Projection.ObservationSourceActive(record) },
		Entity:                  t.Entity,
		AuthorizeObservation: func(m platform.Member, app, permission, kind string, roles []string, at time.Time) *kernel.Error {
			return authorizationRefusal(t.authorizeNative(caller(m, app), permission, kind, roles, true, authorizationDetails{At: at}))
		},
		AuthorizeRead: func(m platform.Member, app string, q platform.AuthzRequest, at time.Time) (bool, *kernel.Error) {
			decision := t.authorizeFrom(t.Projection, caller(m, app), q, at, false)
			return len(decision.Withheld) > 0, authorizationRefusal(decision)
		},
		ReadRecord: func(m platform.Member, app, typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
			return t.objectOwner(t.Projection).ReadRecordFrom(t.Projection, caller(m, app), typ, id, at)
		},
		ObservationConnections: func() ([]dataowner.Connection, int, *kernel.Error) {
			return platform.Find[dataowner.Connection](t.automation(build.ID, false), platform.Query{Limit: 500})
		},
		DataOwner:        t.DataOwner,
		CredentialHeader: t.CredentialHeader,
		ActionSchemaVersion: func(ref platform.AssetRef) (platform.SchemaVersion, error) {
			owner := t.App(ref.App)
			if owner == nil {
				return platform.SchemaVersion{}, fmt.Errorf("observation decision owner is unavailable")
			}
			action, exists := owner.Manifest().Actions.Action(ref.Name)
			if !exists {
				return platform.SchemaVersion{}, fmt.Errorf("observation decision action is unavailable")
			}
			return t.actionSchemaVersion(owner, action)
		},
		Send:  func(r *http.Request, allowPrivate bool) (*http.Response, error) { return t.sender()(r, allowPrivate) },
		Rules: func() platform.RuleEngine { return t.services.Rules },
	})
}
