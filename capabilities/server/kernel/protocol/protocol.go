package protocol

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

const ConfirmationType = "build.confirmation"

const ConfirmationAnswerSchema = ConfirmationType + ".answer"

func PendingConfirmations(c platform.Caller) ([]platform.ConfirmationRun, *platform.Error) {
	var all []platform.ConfirmationRun
	for {
		rows, count, err := platform.Find[platform.ConfirmationRun](c, platform.Query{Domain: json.RawMessage(`[["state","!=","compensated"],["completion","=",""],["completionError","=",""]]`), Sort: []string{"id"}, Limit: 500, Offset: len(all)})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation inventory changed while being read")
		}
	}
}

const DispatchType = "build.dispatch"

const DispatchAnswerSchema = DispatchType + ".answer"

const DispatchReviewSchema = DispatchType + ".review"

const DispatchTickSchema = DispatchType + ".tick"

func PendingDispatches(c platform.Caller) ([]platform.DispatchRun, *platform.Error) {
	var all []platform.DispatchRun
	for {
		rows, count, err := platform.Find[platform.DispatchRun](c, platform.Query{Domain: json.RawMessage(`[["state","!=","completed"],["state","!=","cancelled"],["state","!=","compensated"]]`), Sort: []string{"id"}, Limit: 500, Offset: len(all)})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch inventory changed while being read")
		}
	}
}

type InFlightBinding struct {
	By      string `json:"by,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Inverse string `json:"inverse,omitempty"`
}

type ProtocolBinding struct {
	Contract *platform.Protocol `json:"contract,omitempty"`
	InFlight *InFlightBinding   `json:"inFlight,omitempty"`
	Name     string             `json:"name"`
	Version  int                `json:"version"`
	Actions  map[string]string  `json:"actions"`
	Reads    []string           `json:"reads,omitempty"`
	Events   map[string]string  `json:"events,omitempty"`
}

func ContractFieldInfos(fields []platform.Field) []platform.FieldInfo {
	out := make([]platform.FieldInfo, 0, len(fields))
	for _, f := range fields {
		typ := f.Type
		switch typ {
		case "string":
			typ = "text"
			if f.Ref != "" {
				typ = "reference"
			} else if len(f.Choices) > 0 {
				typ = "choice"
			}
		case "number":
			typ = "decimal"
		}
		out = append(out, platform.FieldInfo{Name: f.Name, Title: f.Description, Type: typ, Required: f.Required, ReadOnly: true, Ref: f.Ref, RefType: f.RefType, Choices: slices.Clone(f.Choices)})
	}
	return out
}

func CheckProtocolActions(contract platform.Protocol, actions []platform.Action) error {
	for _, required := range contract.Actions {
		index := slices.IndexFunc(actions, func(a platform.Action) bool { return a.Schema == required.Schema })
		if index < 0 || len(actions[index].Payload) != len(required.Payload) {
			return fmt.Errorf("protocol %s action %s has an incompatible payload", contract.ID(), required.Schema)
		}
		kind := func(typ string) string {
			switch typ {
			case "string", "text", "longtext":
				return "text"
			case "number", "decimal":
				return "decimal"
			}
			return typ
		}
		for _, field := range required.Payload {
			matched := slices.ContainsFunc(actions[index].Payload, func(actual platform.Field) bool {
				return actual.Name == field.Name && kind(actual.Type) == kind(field.Type) && actual.Required == field.Required && actual.Ref == field.Ref && slices.Equal(actual.Choices, field.Choices)
			})
			if !matched {
				return fmt.Errorf("protocol %s action %s field %s differs", contract.ID(), required.Schema, field.Name)
			}
		}
	}
	if len(actions) != len(contract.Actions) {
		return fmt.Errorf("protocol %s action set differs", contract.ID())
	}
	return nil
}

func CheckProtocol(contract, actual platform.Protocol) error {
	if contract.ID() != actual.ID() {
		return fmt.Errorf("protocol contract identity differs")
	}
	if err := CheckProtocolActions(contract, actual.Actions); err != nil {
		return err
	}
	names := func(p platform.Protocol) []string {
		out := append([]string{}, p.Reads...)
		for _, event := range p.Events {
			out = append(out, "event:"+event.Name)
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(names(contract), names(actual)) {
		return fmt.Errorf("protocol %s read or event declarations differ", contract.ID())
	}
	kind := func(typ string) string {
		switch typ {
		case "string", "text", "longtext", "choice", "reference":
			return "text"
		case "number", "decimal":
			return "decimal"
		}
		return typ
	}
	for read, reqFields := range contract.ReadSchemas {
		actFields := actual.ReadSchemas[read]
		if len(actFields) == 0 {
			continue
		}
		for _, rf := range reqFields {
			if !slices.ContainsFunc(actFields, func(af platform.Field) bool { return af.Name == rf.Name && kind(af.Type) == kind(rf.Type) }) {
				return fmt.Errorf("protocol %s read %s field %s differs", contract.ID(), read, rf.Name)
			}
		}
	}
	return nil
}

func ProtocolAction(protocol, action string) string { return protocol + "#" + action }

type Prompt struct {
	System    string `json:"system,omitempty"`
	User      string `json:"user"`
	MaxTokens int    `json:"maxTokens,omitempty"`
}

type Answer struct {
	Tag            string  `json:"tag,omitempty"`
	Call           string  `json:"call"`
	Action         string  `json:"action,omitempty"`
	Outcome        string  `json:"outcome"`
	Code           string  `json:"code,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	Ref            string  `json:"ref,omitempty"`
	Text           string  `json:"text,omitempty"`
	Metered        bool    `json:"metered,omitempty"`
	TokensReported bool    `json:"tokensReported,omitempty"`
	InputTokens    int     `json:"inputTokens,omitempty"`
	OutputTokens   int     `json:"outputTokens,omitempty"`
	CostReported   bool    `json:"costReported,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	LatencyMillis  int64   `json:"latencyMillis,omitempty"`
	ServedModel    string  `json:"servedModel,omitempty"`
}

func AnswerFields() []platform.Field {
	return []platform.Field{{Name: "call", Type: "string", Required: true, Description: "What was asked of the provider"},
		{Name: "action", Type: "string", Description: "The protocol action"},
		{Name: "outcome", Type: "string", Required: true, Description: "accepted or refused", Choices: []string{"accepted", "refused", "released"}},
		{Name: "code", Type: "string", Description: "The refusal's code"},
		{Name: "reason", Type: "string", Description: "Why it was refused, as the provider said"},
		{Name: "ref", Type: "string", Description: "The provider's record"},
		{Name: "text", Type: "string", Description: "A model's answer"}}
}

func FunctionAnswerFields() []platform.Field {
	return append(AnswerFields(),
		platform.Field{Name: "metered", Type: "boolean", Description: "The provider was called"},
		platform.Field{Name: "tokensReported", Type: "boolean", Description: "The provider reported token counts"},
		platform.Field{Name: "inputTokens", Type: "integer", Description: "Reported input tokens"},
		platform.Field{Name: "outputTokens", Type: "integer", Description: "Reported output tokens"},
		platform.Field{Name: "costReported", Type: "boolean", Description: "The provider reported a USD cost"},
		platform.Field{Name: "costUsd", Type: "decimal", Description: "Reported USD cost"},
		platform.Field{Name: "latencyMillis", Type: "integer", Description: "Model call latency in milliseconds"},
		platform.Field{Name: "servedModel", Type: "string", Description: "Provider-reported model when different"})
}

const (
	RelationsApp = "relations"
	CommentType  = "platform.comment"
	FollowType   = "platform.follow"
)

type RecordComment struct {
	platform.Record
	Target   string   `json:"target" field:"readonly" title:"On" source:"record"`
	Text     string   `json:"text" field:"required,search" type:"longtext" source:"user-input"`
	By       string   `json:"by" field:"readonly" source:"record"`
	Mentions []string `json:"mentions,omitempty" field:"readonly"`
}

type RecordFollow struct {
	platform.Record
	Member string `json:"member" field:"readonly"`
	Target string `json:"target" field:"readonly" title:"Follows"`
}

func FollowID(member, target string) string {
	return member + "@" + strings.ReplaceAll(target, "/", "~")
}

type Breaker struct {
	Destination string    `json:"destination"`
	State       string    `json:"state"`
	Failures    int       `json:"failures"`
	Until       time.Time `json:"until,omitzero"`
}
