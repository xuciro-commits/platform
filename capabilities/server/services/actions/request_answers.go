package actions

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type PendingRequest struct {
	platform.Request
	Caller platform.Caller
	Record *pb.ChangeRecord
	N      int
}

type RequestAnswers struct {
	Tenant      string
	Pending     func() bool
	Pop         func() PendingRequest
	Begin       func(name, app, target string) func(outcome string)
	Attempt     func(func() (*pb.ChangeRecord, *platform.Error)) (*pb.ChangeRecord, *platform.Error)
	Router      func() Router
	Automated   func(platform.Caller, string) platform.Caller
	Reply       func(c platform.Caller, reply *pb.Submission, now time.Time) *platform.Error
	Failure     func() *platform.Error
	Unsupported func()
}

func (r RequestAnswers) Run(now time.Time) *platform.Error {
	for count := 0; r.Pending(); count++ {
		if count >= acceptedformat.MaxHops {
			r.Unsupported()
		}
		if err := r.answer(r.Pop(), now); err != nil {
			return err
		}
	}
	return r.Failure()
}

func (r RequestAnswers) answer(q PendingRequest, now time.Time) (refusal *platform.Error) {
	end := r.Begin("request "+q.Protocol+"#"+q.Action, q.Caller.App, q.Target)
	outcome := "ok"
	defer func() {
		if refusal != nil {
			outcome = refusal.Code.String()
		}
		end(outcome)
	}()
	s := q.Record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.Caller.App, s.GetIdempotencyKey(), q.N)
	providerKey := key
	if q.Key != "" {
		providerKey = q.Key
	}
	answer := protocol.Answer{Tag: q.Tag, Call: q.Target, Action: q.Action, Outcome: "accepted"}
	var ref *pb.EntityRef
	_, err := r.Attempt(func() (*pb.ChangeRecord, *platform.Error) {
		var record *pb.ChangeRecord
		var refusal *platform.Error
		ref, record, refusal = r.Router().Invoke(q.Caller, q.Protocol, q.Action, q.Target,
			platform.Raw(q.Payload), providerKey, s.GetIdempotencyKey(), now, q.Provider)
		return record, refusal
	})
	if q.Reply == "" {
		if err != nil {
			outcome = err.Code.String()
			return err
		}
		return r.Failure()
	}
	if err != nil {
		answer.Outcome, answer.Code, answer.Reason = "refused", err.Code.String(), err.Message
		outcome = answer.Code
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
	}
	payload, errJSON := json.Marshal(answer)
	if errJSON != nil {
		r.Unsupported()
	}
	c := r.Automated(q.Caller, q.Caller.App)
	reply := &pb.Submission{TenantId: r.Tenant, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key,
		CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if err := r.Reply(c, reply, now); err != nil {
		return err
	}
	return r.Failure()
}
