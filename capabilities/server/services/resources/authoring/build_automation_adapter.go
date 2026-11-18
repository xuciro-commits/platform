package authoring

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

type automationEnvironment struct {
	b *Build
}

func (e automationEnvironment) Application() platform.Service { return e.b }
func (e automationEnvironment) Host() protocol.BuildHost      { return e.b.host }
func (e automationEnvironment) Ledger() platform.Authority    { return e.b.authority }
func (e automationEnvironment) AppID() string                 { return ID }
func (e automationEnvironment) Version() string               { return e.b.Manifest().Version }
func (e automationEnvironment) Roles() []string               { return e.b.Manifest().AllRoles() }
func (e automationEnvironment) JointMode() bool               { return e.b.jointMode }
func (e automationEnvironment) FunctionCallAction() string {
	return protocol.SchemaFunctionCall
}
func (e automationEnvironment) FunctionResult(c platform.Caller, id string) (protocol.FunctionOutcome, bool) {
	call, ok := platform.Get[protocol.FunctionRun](c, id)
	if !ok {
		return protocol.FunctionOutcome{}, false
	}
	return protocol.FunctionOutcome{State: call.State, Output: call.Output, Reason: call.Reason, Sources: call.Sources}, true
}
func (e automationEnvironment) InstalledObject(typ string) (platform.RecordType, bool) {
	entity, ok := e.b.installed[typ]
	return entity, ok
}

func (e automationEnvironment) Objects(c platform.Caller) ([]platform.RecordType, error) {
	objects, err := readDefinitionInventory[ontology.Object](c)
	if err != nil {
		return nil, err
	}
	models := make([]platform.RecordType, 0, len(objects))
	for _, object := range objects {
		if !object.Archived {
			models = append(models, Entity(object))
		}
	}
	return models, nil
}

func (e automationEnvironment) Entity(typ string) (platform.RecordTypeInfo, bool) {
	return e.b.lookupEntity(typ)
}

func (e automationEnvironment) Query(name string, version int) (platform.NamedQuery, int, bool) {
	return e.b.queries.Definition(name, version)
}

func (e automationEnvironment) ValidateQuery(query platform.NamedQuery) error {
	if e.b.jointMode {
		if _, selected := e.b.jointEntities[query.Object]; selected {
			return query.Check()
		}
	}
	return e.b.host.ValidateInstallQuery(query)
}

func (e automationEnvironment) CheckIntake(c platform.Caller, process protocol.Process) *platform.Error {
	if process.Continuous == nil || process.Continuous.Intake == nil {
		return nil
	}
	source, ok := platform.Get[dataowner.Source](c, process.Continuous.Intake.SourceRecord)
	if !ok || !source.Stream || source.State != "published" || source.Name != process.Continuous.Source {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The intake names a readable published continuous source")
	}
	return e.b.data.CheckSource(c, source)
}

var _ publication.AutomationEnvironment = automationEnvironment{}
