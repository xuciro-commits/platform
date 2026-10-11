package platform

// SimulationFactory is trusted composition capability. It creates a fresh
// owner with its original declarations; no records, grants or side effects
// are copied from the active owner.
type SimulationFactory interface {
	NewSimulationApp(tenant string) App
}

// SimulationIdentityFactory constructs only the explicit test principals;
// active authentication subjects, sessions and tokens never enter a sandbox.
type SimulationIdentityFactory interface {
	NewSimulationIdentity(tenant string, members []Member) App
}
