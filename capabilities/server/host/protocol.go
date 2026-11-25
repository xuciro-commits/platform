package host

import (
	releaseowner "platformserver/services/devops/releases"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (t *Tenant) router() actions.Router {
	return actions.Router{Host: t, Bindings: t.bindings, Tenant: t.ID}
}

func (t *Tenant) StageSubmission(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return (hostView{t: t}).Submit(c, s, now)
}

func (t *Tenant) ProtocolProvider(protocol string) (string, bool) {
	return t.router().ProtocolProvider(protocol)
}

func (h hostView) IsBoundConnector(protocol, implementation string) bool {
	return h.t.router().IsBoundConnector(protocol, implementation)
}

func (t *Tenant) Invoke(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error) {
	target, record, err, _ := t.invokeDiagnosed(m, protocol, action, id, payload, key, now)
	return target, record, err
}

func (t *Tenant) invokeDiagnosed(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error, []platform.FieldIssue) {
	sub, ok := t.router().Submission(m.ID, protocol, action, id, payload, key)
	if !ok {
		return nil, nil, t.router().Unavailable(protocol), nil
	}
	record, err, issues := t.submitDiagnosed(m, sub, now)
	return sub.GetTarget(), record, err, issues
}

func (t *Tenant) Query(m platform.Member, protocol, read string) (any, *platform.Error) {
	name, err := t.router().ReadName(protocol, read)
	if err != nil {
		return nil, err
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

func (t *Tenant) describer() releaseowner.Describer {
	return releaseowner.NewDescriber(t, PublicationResolver())
}
