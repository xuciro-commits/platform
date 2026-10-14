// Consumer roles expose only the host methods each compiled owner uses.
package host

import (
	"platformserver/platform"
	"time"
)

type BuildHost interface {
	Action(schema string) (app string, a platform.Action, ok bool)
	Automation(parent platform.Caller, app string) platform.Caller
	BindInterfaceQuery(platform.NamedQuery, ...platform.EntityInfo) (platform.NamedQuery, error)
	Caller(parent platform.Caller, m platform.Member, app string) platform.Caller
	Declares(name string) bool
	Entity(typ string) (platform.EntityInfo, bool)
	EntitySource(typ string) (platform.AssetBinding, bool)
	Function(app, name string, version int) (platform.AIFunction, int, bool)
	Install(c platform.Caller, e platform.Entity, actions []platform.Action, pages ...platform.Page) error
	InstallApplication(c platform.Caller, a platform.Application) error
	InstallFunction(c platform.Caller, f platform.AIFunction, version int) error
	InstallLinkType(c platform.Caller, l platform.LinkType, version int) error
	InstallOperation(c platform.Caller, o platform.Operation, version int) error
	InstallPage(c platform.Caller, p platform.Page) error
	InstallPropertyType(c platform.Caller, p platform.PropertyType, version int) error
	InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error
	Interfaces() []platform.Interface
	Member(id string) (platform.Member, bool)
	Operation(app, name string, version int) (platform.Operation, int, bool)
	RuleQuery(app, name string, version int) (platform.NamedQuery, int, bool)
	BuildProcesses() ProcessDefinitions
	Record(ref string) (any, bool)
	ResolvePropertyType(binding platform.AssetBinding) (platform.PropertyType, bool)
	ValidateInstallApplication(a platform.Application) error
	ValidateInstallDependents(e platform.Entity, actions []platform.Action, generatedPage string, pages ...platform.Page) error
	ValidateInstallFunction(f platform.AIFunction) error
	ValidateInstallLinkType(l platform.LinkType) error
	ValidateInstallOperation(o platform.Operation) error
	ValidateInstallPage(p platform.Page) error
	ValidateInstallPropertyType(p platform.PropertyType) error
	ValidateInstallQuery(q platform.NamedQuery) error
}

type RecordSetHost interface {
	ValidateRecordSet(platform.EntityInfo, platform.RecordSet) error
}

// ModelPageHost checks a publisher's model reads through the original member
// projection. It adds no installation or business-decision authority.
type ModelPageHost interface {
	CheckPageModel(platform.Caller, platform.Page) error
	ModelQueryBinding(platform.AssetRef) (platform.AssetBinding, bool)
}

type FileHost interface {
	Readable(m platform.Member, ref string, now time.Time) bool
	Stored(tenant, hash string) bool
}

type KnowledgeHost interface {
	Declares(name string) bool
	Automation(parent platform.Caller, app string) platform.Caller
}

type RelationsHost interface {
	OwnerOf(dataClass string) (app string, ok bool)
	ProtocolEvent(name string) (platform.ProtocolEvent, bool)
	As(c platform.Caller, app string) platform.Caller
	Automation(parent platform.Caller, app string) platform.Caller
	Member(id string) (platform.Member, bool)
	Readable(m platform.Member, ref string, now time.Time) bool
}

type AttachedBuild interface{ AttachBuild(BuildHost) }
type AttachedFiles interface{ AttachFiles(FileHost) }
type AttachedKnowledge interface{ AttachKnowledge(KnowledgeHost) }
type AttachedRelations interface{ AttachRelations(RelationsHost) }

// Attach is composition plumbing. Each owner receives its narrow named role.
func Attach(app platform.App, h Host) {
	if owner, ok := app.(AttachedAgent); ok {
		owner.AttachAgent(h)
	}
	if owner, ok := app.(ConsoleAttached); ok {
		owner.AttachConsole(h)
	}
	if owner, ok := app.(AttachedBuild); ok {
		owner.AttachBuild(h)
	}
	if owner, ok := app.(AttachedFiles); ok {
		owner.AttachFiles(h)
	}
	if owner, ok := app.(AttachedKnowledge); ok {
		owner.AttachKnowledge(h)
	}
	if owner, ok := app.(AttachedRelations); ok {
		owner.AttachRelations(h)
	}
	if owner, ok := app.(AttachedFlow); ok {
		owner.AttachFlow(h)
	}
	if owner, ok := app.(AttachedWork); ok {
		owner.AttachWork(h)
	}

}

// Glossary supplies only read-only terms to search and translation consumers.
type Glossary interface {
	Terms(app string) []platform.KnowledgeTerm
	TermsFor(declaration string) []string
}

// ProcessDefinitions is the declaration/compatibility surface used by Build.
type ProcessDefinitions interface {
	Install(platform.App, platform.Flow) error
	Validate(platform.App, platform.Flow) error
	HasRunningDependency(string) (bool, error)
	HasUnboundRunningDependency(string) (bool, error)
}
