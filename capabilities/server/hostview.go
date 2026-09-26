package platformserver

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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
