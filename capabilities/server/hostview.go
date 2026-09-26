package platformserver

import (
	"context"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// hostView is the tenant as its own apps see it (internal/host.Host, ADR-0025 D4).
type hostView struct{ t *Tenant }

func (h hostView) OwnerOf(dataClass string) (string, bool) {
	for _, a := range h.t.apps {
		if slices.ContainsFunc(a.Declarations(), func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == dataClass }) {
			return a.Manifest().ID, true
		}
	}
	return "", false
}

func (h hostView) ProtocolEvent(name string) (platform.ProtocolEvent, bool) {
	return h.t.protocolEvent(name)
}

func (h hostView) As(c platform.Caller, app string) platform.Caller {
	return platform.NewCaller(runtime{h.t}, c.Member, app, c.Replaying, c.Automation)
}

func (h hostView) Caller(m platform.Member, app string, replaying bool) platform.Caller {
	return platform.NewCaller(runtime{h.t}, m, app, replaying, false)
}

func (h hostView) Automation(app string, replaying bool) platform.Caller {
	return h.t.automation(app, replaying)
}

func (h hostView) Member(id string) (platform.Member, bool) { return h.t.member(id) }

func (h hostView) Holding(app, role string) []string {
	if d, ok := h.t.app(PlatformApp).(*Console); ok {
		return d.holding(app, role)
	}
	return nil
}

func (h hostView) Action(schema string) (string, platform.Action, bool) {
	a := h.t.owner["action:"+schema]
	if a == nil {
		return "", platform.Action{}, false
	}
	declared, ok := a.Manifest().Actions.Action(schema)
	return a.Manifest().ID, declared, ok
}

func (h hostView) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a := h.t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return a.Submit(platform.NewCaller(runtime{h.t}, c.Member, a.Manifest().ID, c.Replaying, c.Automation), s, now)
}

func (h hostView) Recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	return h.t.recipients(c, now, to)
}

func (h hostView) Directory() host.Directory { return h.t.directory }

func (h hostView) Seen(app string, keys ...string) {
	h.t.opsMu.Lock()
	defer h.t.opsMu.Unlock()
	for i, n := range h.t.notices {
		if n.App == app && slices.Contains(keys, n.Key) {
			h.t.notices[i].Read = true
		}
	}
}

func (h hostView) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *kernel.Error) {
	ref, _, err := h.t.invoke(c, protocol, action, id, payload, key, correlation, now)
	return ref, err
}

func (h hostView) Tasks() host.Tasks { return h.t.tasks }

func (h hostView) Processes() host.Processes { return h.t.procs }

func (h hostView) Runs() host.Runs {
	if h.t.agents == nil {
		return nil
	}
	return h.t.agents
}

func (h hostView) Declares(name string) bool { return h.t.declares(name) }

func (h hostView) Readable(m platform.Member, ref string, now time.Time) bool {
	return h.t.Readable(m, ref, now)
}

func (h hostView) Stored(tenant, hash string) bool {
	return h.t.files().Exists(context.Background(), tenant+"/"+hash)
}

func (h hostView) Record(ref string) (any, bool) { return h.t.Held(ref) }
