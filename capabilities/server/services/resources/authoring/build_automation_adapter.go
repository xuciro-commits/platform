package build

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	dataowner "platformserver/services/data"
)

type automationEnvironment struct {
	b *Build
}

func (e automationEnvironment) Application() platform.App { return e.b }
func (e automationEnvironment) Host() platform.BuildHost  { return e.b.host }
func (e automationEnvironment) Ledger() *platform.Ledger  { return e.b.ledger }
func (e automationEnvironment) AppID() string             { return ID }
func (e automationEnvironment) Version() string           { return e.b.Manifest().Version }
func (e automationEnvironment) Roles() []string           { return e.b.Manifest().AllRoles() }
func (e automationEnvironment) JointMode() bool           { return e.b.jointMode }
func (e automationEnvironment) FunctionCallAction() string {
	return SchemaFunctionCall
}
func (e automationEnvironment) FunctionResult(c platform.Caller, id string) (automationauthoring.FunctionOutcome, bool) {
	call, ok := platform.Get[FunctionRun](c, id)
	if !ok {
		return automationauthoring.FunctionOutcome{}, false
	}
	return automationauthoring.FunctionOutcome{State: call.State, Output: call.Output, Reason: call.Reason, Sources: call.Sources}, true
}
func (e automationEnvironment) InstalledObject(typ string) (platform.Entity, bool) {
	entity, ok := e.b.installed[typ]
	return entity, ok
}

func (e automationEnvironment) Objects(c platform.Caller) ([]platform.Entity, error) {
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, err
	}
	models := make([]platform.Entity, 0, len(objects))
	for _, object := range objects {
		if !object.Archived {
			models = append(models, Entity(object))
		}
	}
	return models, nil
}

func (e automationEnvironment) Entity(typ string) (platform.EntityInfo, bool) {
	return e.b.lookupEntity(typ)
}

func (e automationEnvironment) Query(name string, version int) (platform.NamedQuery, int, bool) {
	return e.b.QueryDefinition(name, version)
}

func (e automationEnvironment) ValidateQuery(query platform.NamedQuery) error {
	if e.b.jointMode {
		if _, selected := e.b.jointEntities[query.Object]; selected {
			return query.Check()
		}
	}
	return e.b.host.ValidateInstallQuery(query)
}

func (e automationEnvironment) CheckIntake(c platform.Caller, process automationauthoring.Process) *kernel.Error {
	if process.Continuous == nil || process.Continuous.Intake == nil {
		return nil
	}
	source, ok := platform.Get[dataowner.Source](c, process.Continuous.Intake.SourceRecord)
	if !ok || !source.Stream || source.State != "published" || source.Name != process.Continuous.Source {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The intake names a readable published continuous source")
	}
	return e.b.data.CheckSource(c, source)
}

var _ automationauthoring.Environment = automationEnvironment{}
