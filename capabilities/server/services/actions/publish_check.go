package actions

import (
	"crypto/rand"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/ontology"
)

func WithPublishCheck(result ActionCheckResult, refusal *platform.Error, caller platform.Caller, owner platform.Service, schema, id string, now time.Time, say func(string) string, read *objects.Owner, decide func(*pb.Submission) *platform.Error) (ActionCheckResult, *platform.Error) {
	if refusal != nil || !result.Allowed || schema != ontology.SchemaPublish {
		return result, refusal
	}
	impact, refusal := read.PublishImpact(caller, id, now)
	if refusal != nil {
		return result, refusal
	}
	result.Impact = &impact
	sub := &pb.Submission{
		TenantId: caller.Tenant, PrincipalId: caller.ID, Authority: caller.App, IdempotencyKey: "preview:" + rand.Text(),
		Target: &pb.EntityRef{Type: ontology.ObjectType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
		ExpectedRevision: &impact.Revision, Payload: []byte("{}"),
	}
	if refusal := decide(sub); refusal != nil {
		result.Allowed = false
		result.Message = say(Explained(refusal, owner, schema, ontology.ObjectType+"/"+id).Message)
	}
	return result, nil
}
