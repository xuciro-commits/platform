package data

import (
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
	"platformserver/services/resources/publication"
)

const (
	App                    = "build"
	Builder                = "builder"
	Integrator             = "integrator"
	ValueDefinitionVersion = "1"
)

type Host interface {
	NameTaken(name, id string) (string, bool, error)
	CompartmentExists(id string) bool
	Entity(typ string) (platform.RecordTypeInfo, bool)
	DraftEntity(typ string) (platform.RecordTypeInfo, bool)
	Action(platform.Caller, string) (string, platform.Action, bool)
	ObjectActions(platform.Caller, string) ([]platform.Action, bool)
	RoleDeclared(role string) bool
	Automation(platform.Caller, string) platform.Caller
}

type Owner struct {
	resolver        publication.Resolver
	host            Host
	files           runtimeprotocol.MediaFileHost
	integration     IntegrationRuntime
	releaseDraftIDs map[platform.AssetRef]string
	inventory       IntegrationInventory
}

type IntegrationInventory func(platform.AssetKind) ([]any, error)

func New(host Host, resolver publication.Resolver) *Owner {
	return &Owner{host: host, resolver: resolver}
}

func (o *Owner) AttachFiles(files runtimeprotocol.MediaFileHost) {
	o.files = files
}

func (o *Owner) Fork(host Host) *Owner {
	return &Owner{host: host, resolver: o.resolver, files: o.files, integration: o.integration, releaseDraftIDs: cloneDraftIDs(o.releaseDraftIDs)}
}

func (o *Owner) WithInventory(inventory IntegrationInventory) *Owner {
	copy := *o
	copy.inventory = inventory
	return &copy
}

func (o *Owner) DraftID(ref platform.AssetRef) string {
	return o.releaseDraftIDs[ref]
}

func (o *Owner) DraftIDs() map[platform.AssetRef]string {
	return o.releaseDraftIDs
}

func cloneDraftIDs(ids map[platform.AssetRef]string) map[platform.AssetRef]string {
	if ids == nil {
		return nil
	}
	out := make(map[platform.AssetRef]string, len(ids))
	for ref, id := range ids {
		out[ref] = id
	}
	return out
}

func (o *Owner) checkName(name, id string) *platform.Error {
	if !publication.ValidName(name) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "the name %q is not lower-case letters and digits", name)
	}
	other, taken, err := o.host.NameTaken(name, id)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if taken {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The name {name} is already {other}", name, other)
	}
	return nil
}

func (o *Owner) lookupEntity(typ string) (platform.RecordTypeInfo, bool) {
	return o.host.Entity(typ)
}

func (o *Owner) Entities() []platform.RecordType {
	return append([]platform.RecordType{
		o.connectionEntity(),
		o.datasetEntity(),
		o.datasetVersionEntity(),
		o.sourceEntity(),
		o.listenerEntity(),
		o.pipelineEntity(),
		o.writebackEntity(),
		o.matchEntity(),
		o.observationEntity(),
		o.scheduleEntity(),
	}, MediaSetEntities()...)
}

func (o *Owner) Actions() []platform.Action {
	var actions []platform.Action
	for _, entity := range o.Entities() {
		actions = append(actions, actioncatalog.EntityActions(entity)...)
	}
	actions = append(actions, connectionActions()...)
	actions = append(actions, datasetActions()...)
	actions = append(actions, sourceActions()...)
	actions = append(actions, writebackActions()...)
	actions = append(actions, pipelineActions()...)
	actions = append(actions, MediaSetActions()...)
	actions = append(actions, ScheduleActions()...)
	return actions
}

func (o *Owner) Types() []string {
	var types []string
	for _, entity := range o.Entities() {
		types = append(types, entity.Type)
	}
	return types
}

func (o *Owner) Handles(submission *pb.Submission) bool {
	typ := submission.GetTarget().GetType()
	schema := submission.GetSchema().GetName()
	if slices.Contains(o.Types(), typ) {
		return true
	}
	return strings.HasPrefix(schema, ConnectionType+".") ||
		strings.HasPrefix(schema, DatasetType+".") ||
		strings.HasPrefix(schema, SourceType+".") ||
		strings.HasPrefix(schema, ListenerType+".") ||
		strings.HasPrefix(schema, PipelineType+".") ||
		strings.HasPrefix(schema, WritebackType+".") ||
		strings.HasPrefix(schema, MatchType+".") ||
		strings.HasPrefix(schema, ObservationType+".")
}

func (o *Owner) Decide(authority platform.Authority, c platform.Caller, submission *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool) {
	if !o.Handles(submission) {
		return nil, nil, false
	}
	name := submission.GetSchema().GetName()
	switch name {
	case SchemaSourcePulled:
		record, err := o.SubmitSourcePulled(authority, c, submission, now)
		return record, err, true
	case SchemaConnectionChecked:
		record, err := o.SubmitConnectionChecked(authority, c, submission, now)
		return record, err, true
	case ConnectionType + ".configure":
		record, err := o.ConfigureConnection(authority, c, submission, now)
		return record, err, true
	case SchemaDatasetLoad:
		record, err := o.SubmitDatasetLoad(authority, c, submission, now)
		return record, err, true
	case SchemaPipelineRan:
		record, err := o.SubmitPipelineRan(authority, c, submission, now)
		return record, err, true
	case SchemaDatasetProjected:
		record, err := o.SubmitDatasetProjected(authority, c, submission, now)
		return record, err, true
	case SchemaWritebackAnswered:
		record, err := o.SubmitWritebackAnswered(authority, c, submission, now)
		return record, err, true
	case platform.SchemaMediaSetAdd, platform.SchemaMediaSetRemove:
		record, err := SubmitMediaSet(authority, o.files, c, submission, now)
		return record, err, true
	case platform.SchemaScheduleRan:
		record, err := SubmitScheduleRan(authority, c, submission, now)
		return record, err, true
	}
	if typ, archive := strings.CutSuffix(name, ".archive"); archive && typ == ObservationType && !c.Replaying {
		if !c.Holds(c.App, Builder) && !c.Holds(c.App, Integrator) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}, true
		}
		if observation, ok := platform.Get[Observation](c, submission.GetTarget().GetId()); ok && observation.Published != "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be archived; its installed version must be retained"), true
		}
	}
	record, err, ok := authority.Generated(c, submission, now, nil, o.Entities()...)
	return record, err, ok || o.Handles(submission)
}
