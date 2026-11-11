package host

import (
	"strings"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/data/lineage"
	"platformserver/services/security"
)

type authorizationSources struct{ t *Tenant }

func (s authorizationSources) Tenant() string        { return s.t.ID }
func (s authorizationSources) Store() *records.Store { return s.t.Projection }
func (s authorizationSources) Now() time.Time        { return Now() }
func (s authorizationSources) Engine() platform.AuthorizationEngine {
	return s.t.services.Authorization
}

func (s authorizationSources) LookupMember(id string) (platform.Member, bool) { return s.t.member(id) }

func (s authorizationSources) DeclaredPrincipal(id string) bool {
	declared := s.t.identity == nil || id == "host:maintenance"
	if app, service := strings.CutPrefix(id, "app:"); service {
		declared = s.t.App(app) != nil
	}
	if agent, generated := strings.CutPrefix(id, "agent:"); generated && s.t.agents != nil {
		_, declared = s.t.agents.AgentDeclaration(agent)
	}
	return declared
}

func (s authorizationSources) App(id string) platform.App { return s.t.App(id) }

func (s authorizationSources) ActionOwner(action string) platform.App {
	if !s.t.resourceActionActive(action) {
		return nil
	}
	return s.t.owner["action:"+action]
}

func (s authorizationSources) OwnerActive(id string) bool { return s.t.ownerActive(id) }
func (s authorizationSources) AgentSuspended(id string) bool {
	return s.t.suspended(id)
}
func (s authorizationSources) NamedReadAllowed(m platform.Member, name string) bool {
	return s.t.mayCallRead(m, name)
}
func (s authorizationSources) InputGranted(m platform.Member, app, name string) bool {
	return s.t.connectorInputGranted(m, app, name)
}
func (s authorizationSources) KnowledgeReadable(store *records.Store, m platform.Member, id, field string) bool {
	return s.t.knowledgeSourceReadable(store, m, id, field)
}
func (s authorizationSources) Directory() platform.Directory { return s.t.directory }
func (s authorizationSources) DerivationSources(store *records.Store, m platform.Member, at time.Time, held bool) security.DerivationSource {
	return s.t.objectOwner(store).NarrowerIn(store, m, at, held)
}
func (s authorizationSources) ProjectAccess(store *records.Store, m platform.Member, typ, id, kind string, held bool) (bool, bool) {
	return s.t.ResourcePermission(store, m, typ, id, kind, held)
}
func (s authorizationSources) Markings(store *records.Store, held bool) security.RecordMarkings {
	return securityMarkings{s.t.markingsIn(store, held)}
}

func (t *Tenant) authorizer() security.Authorizer {
	return security.NewAuthorizer(authorizationSources{t})
}

func (t *Tenant) authorizationMember(m platform.Member) platform.Member {
	return t.authorizer().Member(m)
}

type securityMarkings struct{ index lineage.MarkingIndex }

func (m securityMarkings) Row(typ string, row *records.Row) string {
	return m.index.Row(typ, rowValue(row))
}
func (m securityMarkings) Compartments(typ string, row *records.Row) []string {
	return m.index.Compartments(typ, rowValue(row))
}
func (m securityMarkings) Highest(typ string, row *records.Row) string {
	return m.index.Highest(typ, rowValue(row))
}
