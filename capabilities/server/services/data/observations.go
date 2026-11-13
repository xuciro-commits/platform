package data

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (o *Owner) observationEntity() platform.Entity {
	return platform.Entity{Type: ObservationType, Title: "Observation source", Plural: "Observation sources", Model: Observation{}, Display: "title",
		Description: "Read external time-series summaries and apply declared business decisions without retaining raw points.",
		Scope:       integrationScope(),
		Standard:    integrationStandard(),
		Validate: func(c platform.Caller, value any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			record := value.(*Observation)
			if record.State == "retired" && record.Published != "" {
				if _, err := o.ExecutionObservation(*record); err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
				return nil
			}
			if err := o.checkObservationDefinition(c, *record, record.State == "published"); err != nil {
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
					if err := o.checkObservation(c, *record); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
					}
					record.Publisher = c.ID
					record.PublisherScopes = string(platform.Raw(c.Member.Scopes))
					fixed, err := o.freezeDirectIntegration(platform.AssetObservation, *record)
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

func (o *Owner) observationDefinition(record Observation) (platform.ObservationDefinition, error) {
	connection, err := o.integrationName(platform.AssetConnection, record.Connection)
	if err != nil {
		return platform.ObservationDefinition{}, err
	}
	object, ok := o.lookupEntity(record.Object)
	if !ok {
		return platform.ObservationDefinition{}, fmt.Errorf("The observation object is unavailable")
	}
	return platform.ObservationDefinition{Name: record.Name, Title: record.Title, Description: record.Description,
		Connection: platform.AssetRef{App: App, Kind: platform.AssetConnection, Name: connection}, Object: platform.AssetRef{App: object.App, Kind: platform.AssetObject, Name: record.Object},
		DeviceField: record.DeviceField, Signal: record.Signal, Unit: record.Unit, Path: record.Path, WindowSeconds: record.WindowSeconds,
		ExpectedRetentionSeconds: record.ExpectedRetentionSeconds, ExpectedGranularitySeconds: record.ExpectedGranularitySeconds,
		Roles: slices.Clone(record.Roles), Scheduled: record.Scheduled, RequireComplete: record.RequireComplete, Decision: record.Decision, Intervals: record.Intervals}, nil
}

func (o *Owner) ObservationDefinition(record Observation) (platform.ObservationDefinition, error) {
	return o.observationDefinition(record)
}

func (o *Owner) checkObservation(c platform.Caller, record Observation) error {
	return o.checkObservationDefinition(c, record, true)
}

func (o *Owner) checkObservationDefinition(c platform.Caller, record Observation, published bool) error {
	if _, known := o.lookupEntity(record.Object); !known && !published {
		if _, draft := o.host.DraftEntity(record.Object); !draft {
			return fmt.Errorf("The observation object is unavailable")
		}
	}
	if err := o.checkName(record.Name, record.ID); err != nil {
		return err
	}
	if record.Published != "" {
		var prior platform.ReleaseAsset
		if ValueDecodeIntegration([]byte(record.Published), &prior) != nil || prior.Ref.Name != record.Name {
			return fmt.Errorf("A published observation source keeps its name")
		}
	}
	definition, err := o.observationDefinition(record)
	if err != nil {
		return err
	}
	if err := platform.CheckObservationDefinition(definition, o.lookupEntity, c.Rules()); err != nil {
		return err
	}
	rows, err := o.integrationInventory(platform.AssetObservation)
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
		declared := o.host.RoleDeclared(role) || role == platform.AnyMember
		if !declared {
			return fmt.Errorf("The observation role is not declared by its owner")
		}
	}
	if definition.Decision != nil {
		owner, action, ok := o.observationAction(c, definition)
		if !ok || owner != definition.Decision.Action.App || action.Target != definition.Object.Name {
			return fmt.Errorf("The observation decision must use an action on its declared object")
		}
		if err := platform.CheckObservationAction(definition, action); err != nil {
			return err
		}
	}
	connection, ok := platform.Get[Connection](o.host.Automation(platform.Caller{}, App), record.Connection)
	if !ok || connection.Archived {
		return fmt.Errorf("The observation connection is unavailable")
	}
	connection, err = o.ConnectionConfiguration(connection)
	if err != nil {
		return err
	}
	if connection.Kind != "http" {
		return fmt.Errorf("The observation connection must be an HTTP connection")
	}
	return nil
}

func (o *Owner) observationAction(c platform.Caller, definition platform.ObservationDefinition) (string, platform.Action, bool) {
	if actions, ok := o.host.ObjectActions(c, definition.Object.Name); ok {
		for _, action := range actions {
			if action.Schema == definition.Decision.Action.Name {
				return App, action, true
			}
		}
	}
	return o.host.Action(c, definition.Decision.Action.Name)
}

func (o *Owner) ExecutionObservation(record Observation) (platform.ObservationDefinition, error) {
	var asset platform.ReleaseAsset
	if record.Published == "" || ValueDecodeIntegration([]byte(record.Published), &asset) != nil || asset.Ref != (platform.AssetRef{App: App, Kind: platform.AssetObservation, Name: record.Name}) {
		return platform.ObservationDefinition{}, fmt.Errorf("The observation source is not published")
	}
	if err := validateIntegrationFamily(record, asset); err != nil {
		return platform.ObservationDefinition{}, err
	}
	var definition platform.ObservationDefinition
	if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
		return definition, err
	}
	return definition, nil
}

const ObservationType = "build.observation"

type Observation struct {
	platform.Record
	Name                       string                         `json:"name" field:"required,search" source:"record"`
	Title                      string                         `json:"title" field:"required,search" source:"record"`
	Description                string                         `json:"description,omitempty" type:"longtext"`
	Connection                 string                         `json:"connection" field:"required" ref:"build.connection" source:"record"`
	Object                     string                         `json:"object" field:"required"`
	DeviceField                string                         `json:"deviceField" field:"required" title:"Device identity field"`
	Signal                     string                         `json:"signal" field:"required"`
	Unit                       string                         `json:"unit" field:"required"`
	Path                       string                         `json:"path" field:"required" help:"Relative adapter path on the logical connection"`
	WindowSeconds              int                            `json:"windowSeconds" field:"required" title:"Decision window in seconds"`
	ExpectedRetentionSeconds   int                            `json:"expectedRetentionSeconds" field:"required" title:"Expected external retention in seconds"`
	ExpectedGranularitySeconds int                            `json:"expectedGranularitySeconds" field:"required" title:"Expected external granularity in seconds"`
	Roles                      []string                       `json:"roles" field:"required"`
	Scheduled                  bool                           `json:"scheduled,omitempty" title:"Evaluate completed windows"`
	RequireComplete            bool                           `json:"requireComplete,omitempty" title:"Require complete, good-quality windows"`
	Decision                   *platform.ObservationDecision  `json:"decision,omitempty" type:"json"`
	Intervals                  *platform.ObservationIntervals `json:"intervals,omitempty" type:"json" title:"State interval analysis"`
	State                      string                         `json:"state" field:"readonly" choices:"draft,published,retired"`
	Publisher                  string                         `json:"publisher,omitempty" field:"readonly" source:"record"`
	PublisherScopes            string                         `json:"publisherScopes" field:"readonly" type:"json" title:"Publisher scopes"`
	DefinitionVersion          int                            `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published                  string                         `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed" source:"record"`
	Versions                   []string                       `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type EnterpriseTarget struct {
	Source     string   `json:"source"`
	Stereotype string   `json:"stereotype"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ShortName  string   `json:"shortName,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Parent     string   `json:"parent,omitempty"`
	Root       string   `json:"root,omitempty"`
	In         string   `json:"in,omitempty"`
	Relation   string   `json:"relation,omitempty"`
	At         string   `json:"at,omitempty"`
	AtKind     string   `json:"atKind,omitempty"`
	From       string   `json:"from,omitempty"`
	Until      string   `json:"until,omitempty"`
	Properties []string `json:"properties,omitempty"`
}

func (e EnterpriseTarget) Check() error {
	return fmt.Errorf("Enterprise slice targets are retired; use explicit directory actions")
}
