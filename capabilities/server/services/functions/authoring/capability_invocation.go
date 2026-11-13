package authoring

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type CapabilityPreparation struct {
	Descriptor platform.CapabilityDescriptor
	Inputs     json.RawMessage
	Sources    []string
	Query      json.RawMessage
}

type CapabilityHooks struct {
	Retained  func() (platform.CapabilityDescriptor, bool)
	Palette   func() []platform.CapabilityDescriptor
	ReadQuery func() (json.RawMessage, *kernel.Error)
	Bind      func() (json.RawMessage, []string, *kernel.Error)
}

func CheckInvocation(q platform.CapabilityInvocation) *kernel.Error {
	if len(q.Key) == 0 || len(q.Key) > 200 || strings.ContainsAny(q.Key, "/\x00\r\n") || q.Version < 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a capability and bounded stable invocation key")
	}
	if _, err := platform.DecodeValue(q.Inputs, 64<<10); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

func PrepareCapability(h CapabilityHooks, q platform.CapabilityInvocation) (CapabilityPreparation, *kernel.Error) {
	out := CapabilityPreparation{}
	selected := false
	if q.Version > 0 {
		out.Descriptor, selected = h.Retained()
	} else {
		for _, descriptor := range h.Palette() {
			if descriptor.Ref == q.Ref {
				out.Descriptor, selected = descriptor, true
				break
			}
		}
	}
	if !selected {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Capability is unavailable to this member")
	}
	if out.Descriptor.Kind == "query" {
		if out.Descriptor.Source == "tenant" && q.Version == 0 || q.Target != "" || q.ExpectedRevision != nil || len(q.Sources) != 0 || q.Record != "" || len(q.Bindings) != 0 {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Query takes only its declared inputs")
		}
		var refusal *kernel.Error
		out.Query, refusal = h.ReadQuery()
		return out, refusal
	}
	if out.Descriptor.Kind == "action" && (q.Version != 0 || q.Target == "" || len(q.Sources) != 0) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Action requires its target and canonical payload")
	}
	if out.Descriptor.Kind == "compute" && (q.Target != "" || q.ExpectedRevision != nil) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Compute takes typed inputs and protected source references")
	}
	if out.Descriptor.Kind == "compute" || out.Descriptor.Kind == "action" && (len(q.Bindings) > 0 || q.Record != "") {
		var refusal *kernel.Error
		out.Inputs, out.Sources, refusal = h.Bind()
		return out, refusal
	}
	return out, nil
}

func BoundActionPayload(q platform.CapabilityInvocation, bound json.RawMessage) (json.RawMessage, *kernel.Error) {
	if len(q.Bindings) == 0 && q.Record == "" {
		return q.Inputs, nil
	}
	var inputs, values map[string]json.RawMessage
	if json.Unmarshal(q.Inputs, &inputs) != nil || inputs == nil || json.Unmarshal(bound, &values) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Bound action inputs need an object payload")
	}
	for name, value := range values {
		inputs[name] = value
	}
	payload, _ := json.Marshal(inputs)
	return payload, nil
}

func FunctionCallPayload(q platform.CapabilityInvocation) ([]byte, *kernel.Error) {
	if q.Target != "" || q.ExpectedRevision != nil || len(q.Sources) != 0 || q.Record != "" || len(q.Bindings) != 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "AI function takes a readable source record")
	}
	input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}
	if err := input.Validate(q.Inputs, 64<<10); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	var values struct {
		Source string `json:"source"`
	}
	_ = json.Unmarshal(q.Inputs, &values)
	payload, _ := json.Marshal(map[string]any{"app": q.Ref.App, "name": q.Ref.Name, "source": values.Source, "version": q.Version})
	return payload, nil
}

func CapabilityPermission(d platform.Definition) (string, []string) {
	switch {
	case d.Action != nil:
		return d.Ref.Name, d.Action.Roles
	case d.Function != nil:
		return d.Function.Name, d.Function.Roles
	case d.Operation != nil:
		return d.Operation.Name, d.Operation.Roles
	}
	return "", nil
}

func FunctionReadable(function platform.AIFunction, entities []platform.EntityInfo) bool {
	readable := false
	for _, entity := range entities {
		if entity.Type == function.Object && !slices.ContainsFunc(function.Fields, func(name string) bool { _, exists := entity.Field(name); return !exists }) {
			readable = true
		}
	}
	return readable
}

func QueryEntity(entities []platform.EntityInfo, object string) platform.EntityInfo {
	for _, entity := range entities {
		if entity.Type == object {
			return entity
		}
	}
	return platform.EntityInfo{}
}

func MemberQueryVersion(definitions []platform.Definition, appVersion string, ref platform.AssetRef, version int) (platform.NamedQuery, string, bool) {
	if ref.Kind != platform.AssetQuery || version < 0 || version > 64 {
		return platform.NamedQuery{}, "", false
	}
	for _, d := range definitions {
		if d.Ref != ref {
			continue
		}
		if version == 0 {
			if d.Source == "code" && d.Query != nil {
				return *d.Query, d.Version, true
			}
			break
		}
		if d.Source != "tenant" {
			break
		}
		source := appVersion + ".query-" + strconv.Itoa(version)
		if selected := d.QueryVersion(source); selected != nil {
			return *selected.Query, source, true
		}
		break
	}
	return platform.NamedQuery{}, "", false
}
