package authoring

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

const (
	CapabilityCallType           = protocol.CapabilityCallType
	SchemaCapabilityCallAnswered = CapabilityCallType + ".answered"
)

type CapabilityCall struct {
	platform.Record
	Protocol       string         `json:"protocol" field:"required,search" title:"Capability"`
	Action         string         `json:"action" field:"required"`
	Implementation string         `json:"implementation" field:"required,search" title:"Connector"`
	Payload        map[string]any `json:"payload,omitempty" type:"json"`
	Caller         string         `json:"caller,omitempty" field:"readonly" title:"Called by"`
	State          string         `json:"state" field:"readonly" choices:"sent,answered,failed,unknown"`
	Effect         string         `json:"effect,omitempty" field:"readonly" title:"Outbound delivery"`
	Detail         string         `json:"detail,omitempty" field:"readonly"`
	Answer         string         `json:"answer,omitempty" field:"readonly" type:"longtext"`
}

type capabilityCallInput struct {
	Protocol       string         `json:"protocol"`
	Action         string         `json:"action"`
	Implementation string         `json:"implementation"`
	Payload        map[string]any `json:"payload"`
}

func (b *Build) capabilityCallEntity() platform.RecordType {
	return platform.RecordType{Type: CapabilityCallType, Title: "Capability call", Plural: "Capability calls", Model: CapabilityCall{}, Display: "action",
		Description: "One call of a capability action that a connector served: what was sent, to which connector, and what it answered.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Roles: []string{Builder}, Capability: "contracts"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "sent", States: []platform.State{{Name: "sent", Title: "Sent", Tone: "info"},
			{Name: "answered", Title: "Answered", Tone: "success"}, {Name: "failed", Title: "Failed", Tone: "danger"}, {Name: "unknown", Title: "Outcome unknown", Tone: "warning"}}}}
}

func capabilityCallActions() []platform.Action {
	return []platform.Action{
		{Schema: protocol.SchemaCapabilityCallSend, Target: CapabilityCallType, Capability: "contracts", Title: "Call a connector", Roles: []string{platform.AnyMember},
			Description: "Send one capability action to the connector the tenant bound; applications call it through the capability, not directly.",
			Payload: []platform.Field{{Name: "protocol", Type: "string", Required: true}, {Name: "action", Type: "string", Required: true},
				{Name: "implementation", Type: "string", Required: true}, {Name: "payload", Type: "json"}}},
		{Schema: SchemaCapabilityCallAnswered, Target: CapabilityCallType, Capability: "contracts", Title: "Keep connector answer", Automation: true,
			Description: "Retain what the connector answered to one capability call.",
			Payload: []platform.Field{{Name: "effect", Type: "string", Required: true}, {Name: "state", Type: "string", Required: true},
				{Name: "detail", Type: "string"}, {Name: "answer", Type: "string"}}},
	}
}

func (b *Build) decideCapability(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool) {
	switch s.GetSchema().GetName() {
	case SchemaConnectorSetupTested:
		record, err := b.submitConnectorTested(c, s, now)
		return record, err, true
	case protocol.SchemaCapabilityCallSend:
		record, err := b.submitCapabilityCall(c, s, now)
		return record, err, true
	case SchemaCapabilityCallAnswered:
		record, err := b.submitCapabilityCallAnswered(c, s, now)
		return record, err, true
	}
	return nil, nil, false
}

func (b *Build) connectorRequest(in capabilityCallInput) (protocol.ImplementationRequest, ConnectorSetup, error) {
	contract, implementation, found := b.ConnectorParts(in.Implementation)
	if !found || implementation.Contract != in.Protocol || !slices.ContainsFunc(contract.Actions, func(a protocol.ContractAction) bool { return a.Name == in.Action }) {
		return protocol.ImplementationRequest{}, ConnectorSetup{}, fmt.Errorf("%s does not serve %s#%s", in.Implementation, in.Protocol, in.Action)
	}
	at := slices.IndexFunc(implementation.Requests, func(r protocol.ImplementationRequest) bool { return r.Action == in.Action })
	setups, err := readDefinitionInventory[ConnectorSetup](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return protocol.ImplementationRequest{}, ConnectorSetup{}, err
	}
	setup := slices.IndexFunc(setups, func(s ConnectorSetup) bool { return s.Implementation == in.Implementation && !s.Archived })
	if at < 0 || setup < 0 {
		return protocol.ImplementationRequest{}, ConnectorSetup{}, fmt.Errorf("%s is not set up", in.Implementation)
	}
	if _, err := protocol.ConnectorPath(implementation.Requests[at].Path, in.Payload); err != nil {
		return protocol.ImplementationRequest{}, ConnectorSetup{}, err
	}
	return implementation.Requests[at], setups[setup], nil
}

func (b *Build) submitCapabilityCall(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return b.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var in capabilityCallInput
		if json.Unmarshal(s.GetPayload(), &in) != nil {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		if s.GetIdempotencyKey() == "" || s.GetTarget().GetId() != protocol.CapabilityCallID(in.Protocol, in.Action, s.GetIdempotencyKey()) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A connector is called through its capability with an idempotency key")
		}
		if _, exists := platform.Get[CapabilityCall](c, s.GetTarget().GetId()); exists {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This capability call was already sent")
		}
		if !c.Replaying {
			if known, err := b.ConnectorBindable(in.Protocol, in.Implementation); !known || err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Capability {protocol} cannot be served by {implementation} now", in.Protocol, in.Implementation)
			}
			if _, _, err := b.connectorRequest(in); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
		}
		call := CapabilityCall{Record: platform.Record{ID: s.GetTarget().GetId()}, Protocol: in.Protocol, Action: in.Action, Implementation: in.Implementation,
			Payload: in.Payload, Caller: c.ID, State: "sent"}
		return func(r *pb.ChangeRecord) { c.Put(r, call) }, nil
	})
}

func (b *Build) EventEffects(tenant string, c platform.Caller, e platform.Event, preparing bool, credential func(dataowner.WritebackRequest) *platform.Error) ([]platform.Effect, *platform.Error) {
	planned, refusal := b.data.WritebackEffects(tenant, c, e, preparing, credential)
	s := e.Record.GetSubmission()
	if refusal != nil || s.GetSchema().GetName() != protocol.SchemaCapabilityCallSend {
		return planned, refusal
	}
	var in capabilityCallInput
	json.Unmarshal(s.GetPayload(), &in)
	request, setup, err := b.connectorRequest(in)
	if err != nil {
		if preparing {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
		}
		return planned, nil
	}
	_, implementation, _ := b.ConnectorParts(in.Implementation)
	path, _ := protocol.ConnectorPath(request.Path, in.Payload)
	body := in.Payload
	if body == nil {
		body = map[string]any{}
	}
	req := dataowner.WritebackRequest{Consumer: "implementation/" + in.Implementation, Method: request.Method, Transport: "http", URL: setup.Address + path, Secret: setup.Secret, Body: body}
	if implementation.Credential == "api-key" {
		req.Header = "X-API-Key"
	}
	if preparing && req.Secret != "" {
		if refusal := credential(req); refusal != nil {
			return nil, refusal
		}
	}
	raw, _ := json.Marshal(req)
	at := e.Record.GetRecordedTime().AsTime()
	return append(planned, platform.Effect{ID: fmt.Sprintf("%s:%s:%s:call", tenant, ID, e.Record.GetChangeId()), Endpoint: dataowner.WritebackEndpoint + in.Implementation,
		Event: protocol.CapabilityCallEvent, App: ID, Key: e.Record.GetChangeId(), Target: CapabilityCallType + "/" + s.GetTarget().GetId(), At: at, State: "pending", Due: at, Body: string(raw)}), nil
}

func (b *Build) answerCapabilityCall(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error {
	state := map[string]string{"delivered": "answered", "rejected": "failed", "unknown": "unknown", "discarded": "failed"}[out.Result]
	if effect.State == "failed" {
		state = "failed"
	}
	_, id, found := strings.Cut(effect.Target, "/")
	if state == "" || !found {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"effect": effect.ID, "state": state, "detail": out.Detail, "answer": dataowner.ClipAnswer(out.Answer)})
	_, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: fmt.Sprintf("called:%s:%d:%s", effect.ID, effect.Attempts, out.Result),
		Target: &pb.EntityRef{Type: CapabilityCallType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaCapabilityCallAnswered, Version: 1}, Payload: payload}, now)
	return err
}

func (b *Build) submitCapabilityCallAnswered(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return b.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var in struct {
			Effect, State, Detail, Answer string
		}
		call, found := platform.Get[CapabilityCall](c, s.GetTarget().GetId())
		if !found || json.Unmarshal(s.GetPayload(), &in) != nil || !slices.Contains([]string{"answered", "failed", "unknown"}, in.State) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The answer has no capability call")
		}
		call.Effect, call.State, call.Detail, call.Answer = in.Effect, in.State, in.Detail, in.Answer
		return func(r *pb.ChangeRecord) { c.Put(r, call) }, nil
	})
}

func (b *Build) ConnectorState(protocolID, identity string) string {
	known, err := b.ConnectorBindable(protocolID, identity)
	switch {
	case !known:
		return ""
	case err == nil:
		return "passed"
	case strings.Contains(err.Error(), "Set up"):
		return "unconfigured"
	case strings.Contains(err.Error(), "changed since"):
		return "stale"
	}
	return "untested"
}
