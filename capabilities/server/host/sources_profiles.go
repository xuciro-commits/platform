package host

import (
	"context"
	"encoding/json"
	"log"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) CheckConnections(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	conns, _ := recordRead(t, context.Background(), func(t *Tenant) ([]dataowner.Connection, *kernel.Error) {
		rows, _, refusal := platform.Find[dataowner.Connection](t.automation(build.ID, false), platform.Query{Domain: json.RawMessage(`[["requested","=",true]]`), Sort: []string{"id"}, Limit: 50})
		return rows, refusal
	})
	for _, c := range conns {
		detail, err := t.dataClient().Check(c)
		caller := t.automation(build.ID, false)
		sub := dataowner.ConnectionCheckSubmission(t.ID, build.ID, caller.Member, c, detail, err, now)
		if _, err := t.submitBackground(caller.Member, sub, now); err != nil {
			log.Printf("tenant %s: connection check result refused: %v", t.ID, err)
		}
	}
}
