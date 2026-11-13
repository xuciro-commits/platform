package authoring

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/functions/runtime"
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

func VisibleCapabilities(definitions []platform.Definition, builder bool, allowed func(d platform.Definition, permission string, roles []string) bool) []platform.CapabilityDescriptor {
	visible := make([]platform.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if len(definition.Provides) == 0 {
			continue
		}
		permission, roles := CapabilityPermission(definition)
		if permission != "" && !allowed(definition, permission, roles) {
			continue
		}
		visible = append(visible, definition)
	}
	return CapabilityPalette(visible, builder)
}

func DescribeCapability(ref platform.AssetRef, version int, retained func() (platform.CapabilityDescriptor, bool), palette func() []platform.CapabilityDescriptor) (platform.CapabilityDescriptor, *kernel.Error) {
	if version > 0 {
		if descriptor, ok := retained(); ok {
			return descriptor, nil
		}
		return platform.CapabilityDescriptor{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Retained capability is unavailable to this member")
	}
	for _, descriptor := range palette() {
		if descriptor.Ref == ref {
			return descriptor, nil
		}
	}
	return platform.CapabilityDescriptor{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Capability or retained version is unavailable to this member")
}

type RetainedHooks struct {
	Query    func() (platform.NamedQuery, string, bool)
	Entities func() []platform.EntityInfo
	Function func(app platform.App, name string, version int) (platform.AIFunction, int, bool)
	Allowed  func(name string, roles []string) bool
}

func RetainedCapability(app platform.App, ref platform.AssetRef, version int, h RetainedHooks) (platform.CapabilityDescriptor, bool) {
	if app == nil || version < 1 {
		return platform.CapabilityDescriptor{}, false
	}
	selected := CapabilityVersion{Ref: ref, AppVersion: app.Manifest().Version, Revision: version}
	switch ref.Kind {
	case platform.AssetQuery:
		query, source, ok := h.Query()
		if !ok {
			return platform.CapabilityDescriptor{}, false
		}
		selected.Query, selected.QueryVersion = &query, source
		selected.Entity = QueryEntity(h.Entities(), query.Object)
	case platform.AssetCompute:
		operation, ordinal, ok := compute.OperationDefinition(app, ref.Name, version)
		if !ok || ordinal != version || !h.Allowed(operation.Name, operation.Roles) {
			return platform.CapabilityDescriptor{}, false
		}
		selected.Operation = &operation
	case platform.AssetFunction:
		function, ordinal, ok := h.Function(app, ref.Name, version)
		if !ok || ordinal != version || !h.Allowed(function.Name, function.Roles) || !FunctionReadable(function, h.Entities()) {
			return platform.CapabilityDescriptor{}, false
		}
		selected.Function = &function
	default:
		return platform.CapabilityDescriptor{}, false
	}
	return DescribeRetainedCapability(selected)
}

func ActionAnswer(answer platform.CapabilityResult, record *pb.ChangeRecord) platform.CapabilityResult {
	if record == nil {
		answer.State = "pending"
		return answer
	}
	answer.Result, _ = protojson.Marshal(record)
	answer.State = "completed"
	if record.GetSubmission().GetTarget().GetType() == "work.approval" && record.GetSubmission().GetSchema().GetName() == "work.approval.request" {
		answer.State = "pending"
	}
	return answer
}

func AIAnswer(id, state, output, reason string) platform.OperationResult {
	answer := platform.OperationResult{ID: id, State: "pending"}
	if state == "ready" {
		answer.State, answer.Output = "completed", json.RawMessage(output)
	}
	if state == "rejected" {
		answer.State, answer.Error = "failed", reason
	}
	return answer
}

type Closures struct {
	Assets   func() ([]platform.ReleaseAsset, error)
	Describe func(platform.ReleaseAsset) (platform.ReleaseAsset, error)
	Retain   func(candidate platform.ReleaseCandidate, ref platform.AssetRef, sourceVersion string, retained *string) (platform.ReleaseCandidate, string, error)
	Version  func(app string) string
}

func (c Closures) capability(ref platform.AssetRef, body json.RawMessage, sourceVersion string, retained *string) (platform.ReleaseCandidate, string, error) {
	available, err := c.Assets()
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	i := slices.IndexFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return platform.ReleaseCandidate{}, "", fmt.Errorf("capability %s has no release owner", ref)
	}
	available[i].Body = body
	if sourceVersion != "" {
		available[i].SourceVersion = sourceVersion
	}
	available[i], err = c.Describe(available[i])
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	candidate, err := platform.Candidate([]platform.AssetRef{ref}, available)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	return c.Retain(candidate, ref, sourceVersion, retained)
}

func (c Closures) closure(app string, kind platform.AssetKind, name, suffix string, definition any, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	body, err := json.Marshal(definition)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	sourceVersion := ""
	if version > 0 {
		sourceVersion = c.Version(app) + suffix + strconv.Itoa(version)
	}
	return c.capability(platform.AssetRef{App: app, Kind: kind, Name: name}, body, sourceVersion, retained)
}

func (c Closures) Operation(app string, op platform.Operation, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	return c.closure(app, platform.AssetCompute, op.Name, ".compute-", op, version, retained)
}

func (c Closures) Function(app string, f platform.AIFunction, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	return c.closure(app, platform.AssetFunction, f.Name, ".function-", f, version, retained)
}
