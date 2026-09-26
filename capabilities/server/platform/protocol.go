package platform

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Protocol is a named, versioned interface apps provide and consume (ADR-0011):
// actions (payload fields and meaning), reads (result shape, by convention the
// protocol package's types) and events. Consumers depend on a protocol, never on
// the app that provides it.
type Protocol struct {
	Name    string
	Version int
	Actions []Action // Schema is the protocol action's short name; Roles stay the provider's
	Reads   []string
	Events  []ProtocolEvent
}

type ProtocolEvent struct {
	Name  string `json:"name"`
	Title string `json:"title"` // how the timeline tells it, e.g. "Booking canceled"
}

// ID is "<name>/<version>", e.g. "lodging.booking/1".
func (p Protocol) ID() string { return fmt.Sprintf("%s/%d", p.Name, p.Version) }

// Provision maps a protocol onto the provider's own actions, reads and events
// (each keyed by the protocol's name, valued by the provider's).
type Provision struct {
	Protocol Protocol
	Actions  map[string]string // protocol action → provider action schema (same payload)
	Reads    map[string]string // protocol read → provider read (returning the protocol's types)
	Events   map[string]string // protocol event → provider action schema whose decisions are that event
}

// Consumption declares a protocol an app uses; an optional one may have no provider.
type Consumption struct {
	Protocol string // ID
	Optional bool
}

// ProtocolAction names a protocol action or event: "<protocol id>#<name>".
func ProtocolAction(protocol, action string) string { return protocol + "#" + action }

// ProviderResult is one provider's result of a protocol read, with the type of
// the entities it holds, so consumers match their links exactly.
type ProviderResult struct {
	Provider string
	Type     string // e.g. "pms.reservation"
	Result   any
}

// Probe asks the tenant's provider, in a decision's rules, whether it would
// accept a protocol action from the member now: its policy and rules run, and
// nothing is recorded or applied. NOT_FOUND says no provider is bound. In a
// replay it holds, as it did, when a provider is bound. Rules never change another app (ADR-0026 D1):
// what they need of one is a Request, made once the decision is accepted.
func (c Caller) Probe(protocol, action, id string, payload any, now time.Time) *kernel.Error {
	if c.rt == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.Probe(c, protocol, action, id, Raw(payload), now)
}

// Request is a protocol action an accepted decision asks of the tenant's provider
// (ADR-0026 D2). The host submits it after the decision, as the member, and
// submits the provider's Answer to the app's Reply action on the decision's target.
type Request struct {
	Protocol, Action string
	Target           string // the provider's record the action is about
	Payload          any
	Reply            string // the app's own action that receives the Answer; a member may take it too (D5)
}

// Answer is how the provider answered a Request: the payload of the Reply action.
// A member who records an answer themselves gives the same fields.
type Answer struct {
	Call    string `json:"call"`             // the request's target at the provider
	Action  string `json:"action,omitempty"` // the protocol action
	Outcome string `json:"outcome"`          // accepted or refused; a protocol's events may add their own, e.g. released
	Code    string `json:"code,omitempty"`   // the refusal's code
	Ref     string `json:"ref,omitempty"`    // "<type>/<id>" of the provider's record
}

// AnswerFields are the payload fields of a Reply action.
func AnswerFields() []Field {
	return []Field{{Name: "call", Type: "string", Required: true, Description: "What was asked of the provider"},
		{Name: "action", Type: "string", Description: "The protocol action"},
		{Name: "outcome", Type: "string", Required: true, Description: "accepted or refused"},
		{Name: "code", Type: "string", Description: "Why it was refused"},
		{Name: "ref", Type: "string", Description: "The provider's record"}}
}

// Request asks for a protocol action once the decision r is accepted: made in
// the decision's apply, run by the host after it in order, and run again by a replay.
func (c Caller) Request(r *pb.ChangeRecord, q Request) {
	if c.rt != nil && !c.rt.Probing() {
		c.rt.Request(c, r, q)
	}
}

// Raw is a payload as JSON: bytes as they are, anything else marshalled.
func Raw(v any) []byte {
	if b, ok := v.([]byte); ok {
		return b
	}
	if b, ok := v.(json.RawMessage); ok {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}

// Query reads a protocol read from every provider of the tenant, the bound one
// first: switching the binding sends new calls elsewhere, but what the other
// providers hold stays visible (#99). Only a consumer of the protocol may query it.
func (c Caller) Query(protocol, read string) ([]ProviderResult, *kernel.Error) {
	if c.rt == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.Query(c, protocol, read)
}
