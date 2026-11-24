package host

import (
	"context"
	"log"
	"time"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func (t *Tenant) CheckConnections(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	checks, _ := recordRead(t, context.Background(), func(t *Tenant) ([]build.PendingCheck, *platform.Error) {
		return t.App(build.ID).(*build.Build).PendingChecks(t.automation(build.ID, false))
	})
	caller, env := t.automation(build.ID, false), build.CheckEnvironment{Connection: t.dataClient().Check, Credential: t.CredentialHeader, Send: t.sender()}
	for _, check := range checks {
		if _, err := t.submitBackground(caller.Member, check.Run(t.ID, caller.Member, env, now), now); err != nil {
			log.Printf("tenant %s: check result refused: %v", t.ID, err)
		}
	}
}
