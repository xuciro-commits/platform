package authoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

const (
	ConnectorSetupType         = ID + ".connectorsetup"
	SchemaConnectorSetupTested = ConnectorSetupType + ".tested"
)

type ConnectorSetup struct {
	platform.Record
	Implementation string                   `json:"implementation" field:"required,search" title:"Connector" help:"The connector implementation, as build:<name>"`
	Address        string                   `json:"address,omitempty" help:"The https origin this tenant calls; one of the connector's outbound origins"`
	Secret         string                   `json:"secret,omitempty" title:"Credential name" help:"The credential's name; its allowed uses must include implementation/<connector> at this address"`
	Auth           string                   `json:"auth,omitempty" choices:"basic,hmac-sha256" title:"Authentication"`
	Templates      map[string]any           `json:"templates,omitempty" type:"json" title:"Request templates" help:"JSON object keyed by action; use {parameter} as an entire value to insert a typed parameter"`
	State          string                   `json:"state" field:"readonly" choices:"draft,passed,failed"`
	Requested      bool                     `json:"requested,omitempty" field:"readonly" title:"Test requested"`
	Last           *protocol.ConformanceRun `json:"last,omitempty" field:"readonly" type:"json" title:"Last test run"`
}

func (b *Build) ConnectorParts(identity string) (protocol.ProtocolContract, protocol.ProtocolImplementation, bool) {
	app, name, named := protocol.SplitImplementationIdentity(identity)
	if !named || app != ID {
		return protocol.ProtocolContract{}, protocol.ProtocolImplementation{}, false
	}
	for _, implementation := range b.ProtocolImplementations() {
		if implementation.Name == name {
			contract, declared := b.installedContract(implementation.Contract)
			return contract, implementation, declared
		}
	}
	return protocol.ProtocolContract{}, protocol.ProtocolImplementation{}, false
}

func (b *Build) connectorSetupEntity() platform.RecordType {
	return platform.RecordType{Type: ConnectorSetupType, Title: "Connector setup", Plural: "Connector setups", Model: ConnectorSetup{}, Display: "implementation",
		Description: "This tenant's address and credential for one connector, and the result of its last conformance test run. A connector is bound only after its tests pass with the setup it has now.",
		Validate:    b.validateConnectorSetup,
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "contracts"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Not tested", Tone: "warning"}, {Name: "passed", Title: "Passed", Tone: "success"}, {Name: "failed", Title: "Failed", Tone: "danger"}},
			Actions: []platform.ActionType{{
				Name: "test", Title: "Run conformance tests", Description: "Call the connector once for each conformance test of its contract from the host and keep the result.",
				Roles: []string{Builder}, Capability: "contracts", Parameters: []platform.Field{},
				Criteria: platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "passed", "failed"}}},
				Rules:    []platform.ActionRule{{Function: b.requestConnectorTest}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"draft"}}}},
			}}}}
}

func connectorSetupActions() []platform.Action {
	return []platform.Action{{Schema: SchemaConnectorSetupTested, Target: ConnectorSetupType, Capability: "contracts", Title: "Keep conformance test result",
		Description: "Retain what one conformance test run of a connector found.", Automation: true,
		Payload: []platform.Field{{Name: "revision", Type: "integer", Required: true, Description: "The setup revision tested"},
			{Name: "run", Type: "json", Required: true, Description: "When, whether every test passed, and what each test found"}}}}
}

func (b *Build) validateConnectorSetup(c platform.Caller, record any) *platform.Error {
	next := record.(*ConnectorSetup)
	contract, implementation, found := b.ConnectorParts(next.Implementation)
	if !found {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{implementation} is not an installed connector", next.Implementation)
	}
	if next.Address != "" && (protocol.ConnectorOrigin(next.Address) != next.Address || !slices.Contains(implementation.Origins, next.Address)) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The address must be one of the connector's outbound origins: {origins}", strings.Join(implementation.Origins, ", "))
	}
	if err := checkConnectorOptions(contract, implementation, *next); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	setups, _, _ := platform.Find[ConnectorSetup](c, platform.Query{Limit: 500, Sort: []string{"id"}})
	if slices.ContainsFunc(setups, func(s ConnectorSetup) bool {
		return s.ID != next.ID && s.Implementation == next.Implementation && !s.Archived
	}) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{implementation} already has a setup", next.Implementation)
	}
	if old, exists := platform.Get[ConnectorSetup](c, next.ID); exists {
		if old.Implementation != next.Implementation {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A connector setup keeps its connector")
		}
		if old.Address != next.Address || old.Secret != next.Secret || old.Auth != next.Auth || !reflect.DeepEqual(old.Templates, next.Templates) {
			next.State, next.Requested, next.Last = "draft", false, nil
		}
	}
	return nil
}

func (b *Build) requestConnectorTest(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	var setup *ConnectorSetup
	switch typed := record.(type) {
	case *ConnectorSetup:
		setup = typed
	default:
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	contract, implementation, found := b.ConnectorParts(setup.Implementation)
	switch {
	case !found:
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{implementation} is not an installed connector", setup.Implementation)
	case setup.Address == "":
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Set the address of {implementation} before testing it", setup.Implementation)
	case len(contract.Tests) == 0:
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Capability contract {contract} has no conformance tests to run", contract.ID())
	case connectorCredentialKind(implementation, *setup) != "" && setup.Secret == "":
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{implementation} needs a {kind} credential; name it before testing", setup.Implementation, connectorCredentialKind(implementation, *setup))
	}
	setup.Requested = true
	return nil
}

func ConnectorTestedSubmission(tenant string, caller platform.Member, setup ConnectorSetup, run protocol.ConformanceRun) *pb.Submission {
	payload, _ := json.Marshal(map[string]any{"revision": setup.Revision, "run": run})
	return &pb.Submission{TenantId: tenant, PrincipalId: caller.ID, Authority: ID,
		IdempotencyKey: fmt.Sprintf("tested:%s:%d:%s", setup.ID, setup.Revision, run.At.UTC().Format(time.RFC3339Nano)),
		Target:         &pb.EntityRef{Type: ConnectorSetupType, Id: setup.ID}, Schema: &pb.SchemaRef{Name: SchemaConnectorSetupTested, Version: 1}, Payload: payload}
}

func (b *Build) submitConnectorTested(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return b.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var payload struct {
			Revision uint32                  `json:"revision"`
			Run      protocol.ConformanceRun `json:"run"`
		}
		setup, found := platform.Get[ConnectorSetup](c, s.GetTarget().GetId())
		if !found || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Run.At.IsZero() || !setup.Requested || setup.Revision != payload.Revision {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The test result has no requested connector setup")
		}
		return func(r *pb.ChangeRecord) {
			run := payload.Run
			setup.Last, setup.Requested, setup.State = &run, false, "failed"
			if run.Passed {
				setup.State = "passed"
			}
			c.Put(r, setup)
		}, nil
	})
}

func (b *Build) ConnectorBindable(protocolID, identity string) (bool, error) {
	contract, implementation, found := b.ConnectorParts(identity)
	if !found || implementation.Contract != protocolID {
		return false, nil
	}
	setups, err := readDefinitionInventory[ConnectorSetup](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return true, err
	}
	at := slices.IndexFunc(setups, func(s ConnectorSetup) bool { return s.Implementation == identity && !s.Archived })
	switch {
	case at < 0:
		return true, fmt.Errorf("Set up %s and run the conformance tests of %s before binding it", identity, protocolID)
	case setups[at].State != "passed" || setups[at].Last == nil || !setups[at].Last.Passed:
		return true, fmt.Errorf("%s has not passed the conformance tests of %s", identity, protocolID)
	case setups[at].Last.Fingerprint != connectorSetupFingerprint(contract, implementation, setups[at]):
		return true, fmt.Errorf("%s changed since its last test run; run the conformance tests of %s again", identity, protocolID)
	}
	return true, nil
}

func connectorCredentialKind(i protocol.ProtocolImplementation, setup ConnectorSetup) string {
	switch setup.Auth {
	case "basic":
		return "password"
	case "hmac-sha256":
		return "api-key"
	default:
		return i.Credential
	}
}

func connectorSetupFingerprint(c protocol.ProtocolContract, i protocol.ProtocolImplementation, setup ConnectorSetup) string {
	base := protocol.ConnectorFingerprint(c, i, setup.Address, setup.Secret)
	if setup.Auth == "" && len(setup.Templates) == 0 {
		return base
	}
	raw, _ := json.Marshal(struct {
		Base      string
		Auth      string
		Templates map[string]any
	}{base, setup.Auth, setup.Templates})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func checkConnectorOptions(c protocol.ProtocolContract, i protocol.ProtocolImplementation, setup ConnectorSetup) error {
	if setup.Auth != "" && setup.Auth != "basic" && setup.Auth != "hmac-sha256" {
		return fmt.Errorf("Unsupported connector authentication")
	}
	if len(platform.Raw(setup.Templates)) > 32<<10 {
		return fmt.Errorf("Request templates exceed 32 KiB")
	}
	for name, value := range setup.Templates {
		at := slices.IndexFunc(i.Requests, func(r protocol.ImplementationRequest) bool { return r.Action == name })
		if at < 0 || i.Requests[at].Method == "GET" || i.Requests[at].Method == "DELETE" {
			return fmt.Errorf("Request templates require a body-bearing action")
		}
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return fmt.Errorf("Each request template must be a JSON object")
		}
		action := slices.IndexFunc(c.Actions, func(a protocol.ContractAction) bool { return a.Name == name })
		if action < 0 {
			return fmt.Errorf("Request template action is not declared")
		}
		params := make(map[string]any)
		for _, field := range c.Actions[action].Payload {
			params[field.Name] = nil
		}
		if _, err := renderConnectorTemplate(object, params); err != nil {
			return err
		}
	}
	return nil
}

func renderConnectorTemplate(value any, payload map[string]any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for name, item := range typed {
			rendered, err := renderConnectorTemplate(item, payload)
			if err != nil {
				return nil, err
			}
			out[name] = rendered
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			rendered, err := renderConnectorTemplate(item, payload)
			if err != nil {
				return nil, err
			}
			out[idx] = rendered
		}
		return out, nil
	case string:
		if strings.HasPrefix(typed, "{") && strings.HasSuffix(typed, "}") {
			key := typed[1 : len(typed)-1]
			v, found := payload[key]
			if !found {
				return nil, fmt.Errorf("Request template refers to a missing parameter")
			}
			return v, nil
		}
	}
	return value, nil
}
