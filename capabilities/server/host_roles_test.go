package platformserver

import (
	"platformserver/apps/build"
	"platformserver/internal/host"
	"platformserver/platform"
	"testing"
)

// Governance must not initialise permissions on a half-declared composition.
type readyConsole struct {
	*Console
	buildHost host.BuildHost
	ready     int
	testing   *testing.T
}

func (c *readyConsole) AttachBuild(h host.BuildHost) { c.buildHost = h }
func (c *readyConsole) Ready() error {
	c.ready++
	if _, _, ok := c.buildHost.Action("build.object.create"); !ok {
		c.testing.Fatal("Ready observed incomplete action declarations")
	}
	return c.Console.Ready()
}
func TestCompositionReadyRequiresAllDeclarations(t *testing.T) {
	c := &readyConsole{Console: NewConsole("ready", seatOf("admin", "platform:admin", "build:builder")), testing: t}
	tenant, err := NewTenant("ready", c, build.New("ready"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ready != 1 {
		t.Fatalf("Ready called %d times", c.ready)
	}
	if m, ok := tenant.Member("admin"); !ok || !m.Holds(build.ID, build.Builder) {
		t.Fatal("ready identity roles missing")
	}
	failed := &readyConsole{Console: NewConsole("invalid", seatOf("admin", "platform:admin")), testing: t}
	if _, err := NewTenant("invalid", failed, build.New("invalid"), build.New("invalid")); err == nil {
		t.Fatal("duplicate owner composed")
	}
	if failed.ready != 0 {
		t.Fatal("failed composition published readiness")
	}
}

var _ platform.App = (*readyConsole)(nil)
