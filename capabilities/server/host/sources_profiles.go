package host

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) CheckConnections(now time.Time) {
	if t.businessStopped() || !t.ownerActive(build.ID) {
		return
	}
	conns, _ := recordRead(t, context.Background(), func(t *Tenant) ([]dataowner.Connection, *kernel.Error) {
		rows, _, refusal := platform.Find[dataowner.Connection](t.automation(build.ID, false), platform.Query{Domain: json.RawMessage(`[["requested","=",true]]`), Sort: []string{"id"}, Limit: 50})
		return rows, refusal
	})
	for _, c := range conns {
		check := dataowner.ConnectionCheck{At: now}
		detail, err := t.sourceReader().Check(c)
		check.OK, check.Detail = err == nil, detail
		if err != nil {
			check.Error = err.Error()
		}
		payload, _ := json.Marshal(map[string]any{"revision": c.Revision, "check": check})
		caller := t.automation(build.ID, false)
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID, IdempotencyKey: fmt.Sprintf("checked:%s:%d:%s", c.ID, c.Revision, now.UTC().Format(time.RFC3339Nano)),
			Target: &pb.EntityRef{Type: dataowner.ConnectionType, Id: c.ID}, Schema: &pb.SchemaRef{Name: dataowner.SchemaConnectionChecked, Version: 1}, Payload: payload}
		if _, err := t.submitBackground(caller.Member, sub, now); err != nil {
			log.Printf("tenant %s: connection check result refused: %v", t.ID, err)
		}
	}
}
