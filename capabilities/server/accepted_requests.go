package platformserver

import (
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Internal protocol requests and replies are child decisions of the input,
// never separately confirmed journal writes. They run after application
// locks are released and before the single result is planned/committed.
func (d *stagedDecision) answerRequests(now time.Time) *kernel.Error {
	for count := 0; len(d.requests) > 0; count++ {
		if count >= maxHops {
			unsupportedStagedEffect()
		}
		q := d.requests[0]
		d.requests = d.requests[1:]
		if err := d.answerRequest(q, now); err != nil {
			return err
		}
	}
	return d.failure
}

func (d *stagedDecision) answerRequest(q request, now time.Time) (refusal *kernel.Error) {
	end := d.tenant.begin("request "+q.Protocol+"#"+q.Action, trace.SpanContext{}, attribute.String("platform.app", q.caller.App), attribute.String("platform.target", q.Target))
	outcome := "ok"
	defer func() {
		if refusal != nil {
			outcome = refusal.Code.String()
		}
		end(outcome)
	}()
	s := q.record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.caller.App, s.GetIdempotencyKey(), q.n)
	providerKey := key
	if q.Key != "" {
		providerKey = q.Key
	}
	answer := platform.Answer{Tag: q.Tag, Call: q.Target, Action: q.Action, Outcome: "accepted"}
	var ref *pb.EntityRef
	_, err := d.Attempt(func() (*pb.ChangeRecord, *kernel.Error) {
		var record *pb.ChangeRecord
		var refusal *kernel.Error
		ref, record, refusal = d.routingTenant().invoke(q.caller, q.Protocol, q.Action, q.Target,
			platform.Raw(q.Payload), providerKey, s.GetIdempotencyKey(), now, q.Provider)
		return record, refusal
	})
	if err != nil {
		answer.Outcome, answer.Code, answer.Reason = "refused", err.Code.String(), err.Message
		outcome = answer.Code
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
	}
	payload, errJSON := json.Marshal(answer)
	if errJSON != nil {
		unsupportedStagedEffect()
	}
	c := d.tenant.automated(q.caller, q.caller.App)
	reply := &pb.Submission{TenantId: d.tenant.ID, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key,
		CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if _, err := platform.Decide(c, d.tenant.app(c.App), reply, now); err != nil {
		return err
	}
	return d.failure
}
