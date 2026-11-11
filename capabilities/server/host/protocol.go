package host

import (
	releaseowner "platformserver/services/devops/releases"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (t *Tenant) router() actions.Router {
	return actions.Router{Host: t, Bindings: t.bindings, Tenant: t.ID}
}

func (t *Tenant) StageSubmission(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return (hostView{t: t}).Submit(c, s, now)
}

func (t *Tenant) ProtocolProvider(protocol string) (string, bool) {
	return t.router().ProtocolProvider(protocol)
}

func (t *Tenant) Invoke(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	target, record, err, _ := t.invokeDiagnosed(m, protocol, action, id, payload, key, now)
	return target, record, err
}

func (t *Tenant) invokeDiagnosed(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	provider, schema, ok := t.router().Provider(platform.ProtocolAction(protocol, action))
	if !ok {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	declared, _ := provider.Manifest().Actions.Action(schema)
	target := &pb.EntityRef{Type: declared.Target, Id: id}
	record, err, issues := t.submitDiagnosed(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: t.AuthorityOf(declared.Target),
		Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: payload}, now)
	return target, record, err, issues
}

func (t *Tenant) Query(m platform.Member, protocol, read string) (any, *kernel.Error) {
	b, bound := t.router().Resolve(protocol)
	if !bound {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	name, declared := b.Provision.Reads[read]
	if !declared || !slices.Contains(b.Provision.Protocol.Reads, read) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.read(m, name)
}

func (t *Tenant) AuthorityOf(dataClass string) string {
	for _, d := range t.Declarations() {
		if d.GetDataClass() == dataClass {
			return d.GetAuthorityId()
		}
	}
	return ""
}

func (t *Tenant) describer() releaseowner.Describer { return releaseowner.NewDescriber(t) }
