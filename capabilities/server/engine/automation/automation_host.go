package automation

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

// FlowBindingContext selects original retained definitions; accepted rows and
// current source/SQL reads stay with their existing authority.
type FlowBindingContext struct {
	App            func(string) platform.App
	Asset          func(string, string, int) (platform.ReleaseAsset, error)
	Assets         func() ([]platform.ReleaseAsset, error)
	Active         func() string
	Candidate      func(string) (platform.ReleaseCandidate, error)
	Applications   func(platform.AssetRef, string, string) ([]platform.AssetBinding, error)
	CloseCandidate func([]platform.AssetRef, []platform.ReleaseAsset, []platform.AssetRef) (platform.ReleaseCandidate, error)
	SameRetained   func(platform.ReleaseCandidate, platform.ReleaseCandidate) bool
	CodeMatches    func(platform.ReleaseAsset, platform.ReleaseAsset) (bool, error)
}

// BindFlow runs under the tenant input/recovery lock. The flow owner supplies
// its retained native version; dependency owners supply their complete assets.
func (h FlowBindingContext) BindFlow(app, name string, version int, prior platform.FlowBinding) (platform.FlowBinding, error) {
	if prior.Existing && prior.Dependencies == "" && prior.Release == "" {
		if _, controlled := h.App(app).(platform.FlowReleaseOwner); !controlled {
			return platform.FlowBinding{}, nil
		}
	}
	if prior.Release != "" {
		closed, err := h.Retained(app, name, version, prior)
		if err != nil {
			return platform.FlowBinding{}, err
		}
		bound := prior
		bound.Assets = nil
		for _, asset := range closed.Assets {
			bound.Assets = append(bound.Assets, asset.Ref)
		}
		return bound, nil
	}
	closure, root, err := h.Closure(app, name, version)
	if err != nil {
		return platform.FlowBinding{}, err
	}
	if closure.ID == "" {
		if prior.Dependencies != "" {
			return platform.FlowBinding{}, fmt.Errorf("flow %s.%s has no release descriptor owner", app, name)
		}
		return platform.FlowBinding{}, nil
	}
	binding := platform.FlowBinding{Dependencies: closure.ID}
	bindings, bindErr := h.Applications(root, h.Active(), closure.ID)
	if bindErr != nil {
		return binding, bindErr
	}
	binding.Applications = bindings
	for _, asset := range closure.Assets {
		binding.Assets = append(binding.Assets, asset.Ref)
	}
	if prior.Dependencies != "" {
		if prior.Dependencies != closure.ID {
			return binding, fmt.Errorf("flow %s version %d dependency release changed", root, version)
		}
		return binding, nil
	}
	binding.Release = h.Active()
	if binding.Release == "" {
		return binding, nil
	}
	saved, err := h.Candidate(binding.Release)
	if err != nil {
		return binding, fmt.Errorf("flow release %s: %w", binding.Release, err)
	}
	if !slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == root }) {
		binding.Release = ""
		return binding, nil
	}
	closed, err := saved.Closure([]platform.AssetRef{root})
	if err != nil || !h.SameRetained(closure, closed) {
		return binding, fmt.Errorf("flow %s version %d differs from its activated release", root, version)
	}
	binding.Dependencies = closed.ID
	return binding, nil
}

func (h FlowBindingContext) Retained(app, name string, version int, prior platform.FlowBinding) (platform.ReleaseCandidate, error) {
	if prior.Dependencies == "" {
		return platform.ReleaseCandidate{}, fmt.Errorf("flow has no retained starting dependency descriptor")
	}
	root, err := h.Asset(app, name, version)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	saved, err := h.Candidate(prior.Release)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	closed, err := saved.Closure([]platform.AssetRef{root.Ref})
	if err != nil || closed.ID != prior.Dependencies {
		return platform.ReleaseCandidate{}, fmt.Errorf("flow starting dependency descriptor differs")
	}
	for _, asset := range closed.Assets {
		if asset.Ref == root.Ref {
			var descriptor platform.FlowReleaseDescriptor
			if json.Unmarshal(root.Body, &descriptor) != nil {
				return platform.ReleaseCandidate{}, fmt.Errorf("retained flow descriptor is invalid")
			}
			if !descriptor.Native {
				continue
			}
			same, err := h.CodeMatches(root, asset)
			if err != nil || !same {
				return platform.ReleaseCandidate{}, fmt.Errorf("retained native flow differs from its starting release")
			}
		}
	}
	return closed, nil
}

func (h FlowBindingContext) Closure(app, name string, version int) (platform.ReleaseCandidate, platform.AssetRef, error) {
	asset, err := h.Asset(app, name, version)
	if err != nil {
		return platform.ReleaseCandidate{}, platform.AssetRef{}, err
	}
	available, err := h.Assets()
	if err != nil {
		return platform.ReleaseCandidate{}, asset.Ref, err
	}
	i := slices.IndexFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == asset.Ref })
	if i < 0 {
		return platform.ReleaseCandidate{}, asset.Ref, fmt.Errorf("flow %s has no installed release asset", asset.Ref)
	}
	available[i] = asset
	closure, err := h.CloseCandidate([]platform.AssetRef{asset.Ref}, available, nil)
	return closure, asset.Ref, err
}
func ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time, entity func(string) (platform.EntityInfo, bool), read func(string, string, time.Time) (json.RawMessage, *kernel.Error)) (json.RawMessage, []string, *kernel.Error) {
	if len(path) == 0 || len(path) > 16 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record paths need 1–16 fields")
	}
	var sources []string
	for i, name := range path {
		raw, refusal := read(typ, id, now)
		if refusal != nil {
			return nil, nil, refusal
		}
		info, known := entity(typ)
		field, declared := info.Field(name)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		value, visible := fields[name]
		if !known || !declared || !field.ReadsAny(c.RolesIn(info.App)) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: []string{name}}, now)
		if !permission.Allow || len(permission.Withheld) > 0 {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		ref := typ + "/" + id
		sources = append(sources, ref, ref+"#"+name)
		if !visible {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input has no value")
		}
		if i == len(path)-1 {
			return value, sources, nil
		}
		if field.Type != "reference" || field.Ref == "" {
			// Opaque JSON fields retain ordinary JSON-path behavior; typed
			// form publications cannot infer a schema inside an opaque field.
			result, err := (platform.Binding{Source: "subject", Path: path[i+1:]}).Resolve(&platform.Run{}, value)
			if err != nil {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return result, sources, nil
		}
		if json.Unmarshal(value, &id) != nil || id == "" {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input reference is empty")
		}
		typ = field.Ref
	}
	return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input is unavailable")
}
