package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	buildapi "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

const ObservationType = buildapi.ObservationType
const SchemaObservation = buildapi.SchemaObservation

type Observation = buildapi.Observation

func (b *Build) observationEntity() platform.Entity {
	return platform.Entity{Type: ObservationType, Title: "Observation source", Plural: "Observation sources", Model: Observation{}, Display: "title",
		Description: "Read external time-series summaries and apply declared business decisions without retaining raw points.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			record := value.(*Observation)
			if record.State == "retired" && record.Published != "" {
				if _, err := b.ExecutionObservation(*record); err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
				return nil
			}
			if err := b.checkObservationDefinition(c, *record, record.State == "published"); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}, {Name: "retired", Title: "Retired", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Retain the logical source, authorized reads and declared decision as one immutable version.", From: []string{"draft", "published", "retired"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: func(c platform.Caller, value any, _ json.RawMessage, _ time.Time) *kernel.Error {
					record := value.(*Observation)
					if c.Replaying {
						return nil
					}
					if err := b.checkObservation(c, *record); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
					}
					record.Publisher = c.ID
					record.PublisherScopes = string(platform.Raw(c.Member.Scopes))
					fixed, err := b.freezeDirectIntegration(platform.AssetObservation, *record)
					if err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
					}
					*record = fixed.(Observation)
					return nil
				}},
				{Name: "retire", Title: "Retire", Description: "Stop new reads and evaluations while retaining every published definition.", From: []string{"published"}, To: []string{"retired"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}},
			}},
	}
}

func (b *Build) observationDefinition(record Observation) (platform.ObservationDefinition, error) {
	connection, err := b.integrationName(platform.AssetConnection, record.Connection)
	if err != nil {
		return platform.ObservationDefinition{}, err
	}
	object, ok := b.lookupEntity(record.Object)
	if !ok {
		return platform.ObservationDefinition{}, fmt.Errorf("The observation object is unavailable")
	}
	return platform.ObservationDefinition{Name: record.Name, Title: record.Title, Description: record.Description,
		Connection: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: connection}, Object: platform.AssetRef{App: object.App, Kind: platform.AssetObject, Name: record.Object},
		DeviceField: record.DeviceField, Signal: record.Signal, Unit: record.Unit, Path: record.Path, WindowSeconds: record.WindowSeconds,
		ExpectedRetentionSeconds: record.ExpectedRetentionSeconds, ExpectedGranularitySeconds: record.ExpectedGranularitySeconds,
		Roles: slices.Clone(record.Roles), Scheduled: record.Scheduled, RequireComplete: record.RequireComplete, Decision: record.Decision}, nil
}

func (b *Build) checkObservation(c platform.Caller, record Observation) error {
	return b.checkObservationDefinition(c, record, true)
}

func (b *Build) checkObservationDefinition(c platform.Caller, record Observation, ready bool) error {
	if err := b.checkName(record.Name, record.ID); err != nil {
		return err
	}
	if record.Published != "" {
		var prior platform.ReleaseAsset
		if buildapi.ValueDecodeIntegration([]byte(record.Published), &prior) != nil || prior.Ref.Name != record.Name {
			return fmt.Errorf("A published observation source keeps its name")
		}
	}
	definition, err := b.observationDefinition(record)
	if err != nil {
		return err
	}
	if err := platform.CheckObservationDefinition(definition, b.lookupEntity); err != nil {
		return err
	}
	rows, err := b.integrationInventory(platform.AssetObservation)
	if err != nil {
		return err
	}
	for _, row := range rows {
		other := row.(Observation)
		if other.Name == record.Name && other.ID != record.ID {
			return fmt.Errorf("The observation source name is already used")
		}
	}
	for _, role := range record.Roles {
		if !slices.Contains(b.Manifest().AllRoles(), role) && role != platform.AnyMember {
			return fmt.Errorf("The observation role is not declared by its owner")
		}
	}
	if definition.Decision != nil {
		owner, action, ok := b.observationAction(definition)
		if !ok || owner != definition.Decision.Action.App || action.Target != definition.Object.Name {
			return fmt.Errorf("The observation decision must use an action on its declared object")
		}
		if ready && c.ID != "" && (!c.Member.InScope(action.Schema) || !c.May(owner, action.Schema, action.Roles).Allow) {
			return fmt.Errorf("The publisher cannot invoke the declared observation decision")
		}
		if err := platform.CheckObservationAction(definition, action); err != nil {
			return err
		}
	}
	connection, ok := platform.Get[Connection](b.host.Automation(platform.Caller{}, ID), record.Connection)
	if !ok || connection.Archived || ready && connection.State != "ready" || connection.Kind != "http" {
		return fmt.Errorf("The observation connection must be a ready HTTP connection")
	}
	return nil
}

// A joint candidate may declare the object's action in the same publication.
// Read that original object draft rather than requiring an earlier live install.
func (b *Build) observationAction(definition platform.ObservationDefinition) (string, platform.Action, bool) {
	if _, joint := b.jointEntities[definition.Object.Name]; joint {
		objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
		if err != nil {
			return "", platform.Action{}, false
		}
		for _, object := range objects {
			if object.Archived || buildapi.TypeOf(object.Name) != definition.Object.Name {
				continue
			}
			for _, action := range objectActions(object, Entity(object)) {
				if action.Schema == definition.Decision.Action.Name {
					return ID, action, true
				}
			}
		}
	}
	return b.host.Action(definition.Decision.Action.Name)
}

// ExecutionObservation returns accepted declaration bytes, never a mutable
// editor row and never observations fetched from the external store.
func (b *Build) ExecutionObservation(record Observation) (platform.ObservationDefinition, error) {
	var asset platform.ReleaseAsset
	if record.Published == "" || buildapi.ValueDecodeIntegration([]byte(record.Published), &asset) != nil || asset.Ref != (platform.AssetRef{App: ID, Kind: platform.AssetObservation, Name: record.Name}) {
		return platform.ObservationDefinition{}, fmt.Errorf("The observation source is not published")
	}
	if err := validateIntegrationFamily(record, asset); err != nil {
		return platform.ObservationDefinition{}, err
	}
	var definition platform.ObservationDefinition
	if err := buildapi.ValueDecodeIntegration(asset.Body, &definition); err != nil {
		return definition, err
	}
	return definition, nil
}

var _ host.ObservationDefinitions = (*Build)(nil)
