package descriptor

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

const (
	KindApplication = "application"
	KindMasterData  = "master-data"
	KindCapability  = "capability"
	KindExtension   = "extension"
)

var Kinds = []string{KindApplication, KindMasterData, KindCapability, KindExtension}

type KindSource struct {
	Descriptor platform.PackageDescriptor
	Listing    *protocol.PackageListing
}

func PackageKinds(sources []KindSource) map[string]string {
	kinds := map[string]string{}
	inSet := map[string]bool{}
	for _, s := range sources {
		inSet[s.Descriptor.ID] = true
	}
	ready := func(s KindSource) bool {
		for _, need := range s.Descriptor.Requires {
			if id, _ := PackageDependency(need); inSet[id] && kinds[id] == "" {
				return false
			}
		}
		return true
	}
	for range sources {
		for _, s := range sources {
			if kinds[s.Descriptor.ID] == "" && ready(s) {
				kinds[s.Descriptor.ID] = Kind(s.Descriptor, s.Listing, kinds)
			}
		}
	}
	for _, s := range sources {
		if kinds[s.Descriptor.ID] == "" {
			kinds[s.Descriptor.ID] = Kind(s.Descriptor, s.Listing, kinds)
		}
	}
	return kinds
}

func Kind(p platform.PackageDescriptor, l *protocol.PackageListing, kinds map[string]string) string {
	if l != nil && slices.Contains(Domains(*l), KindMasterData) {
		return KindMasterData
	}
	if hasApplication(p) {
		return KindApplication
	}
	if providesContract(p) {
		return KindCapability
	}
	for _, need := range p.Requires {
		if id, _ := PackageDependency(need); kinds[id] == KindApplication || kinds[id] == KindExtension {
			return KindExtension
		}
	}
	return KindMasterData
}

func CheckKinds(sources []KindSource, kinds map[string]string) error {
	for _, s := range sources {
		p, kind := s.Descriptor, kinds[s.Descriptor.ID]
		for _, need := range p.Requires {
			id, constraint := PackageDependency(need)
			required, known := kinds[id]
			if !known {
				continue
			}
			upper := required == KindApplication || required == KindExtension
			switch {
			case kind == KindMasterData && required != KindMasterData:
				return fmt.Errorf("package %s is master data and can require only master data packages, but %s is %s", p.ID, id, required)
			case kind == KindCapability && upper:
				return fmt.Errorf("package %s is a capability and cannot require %s, which is %s; applications use a capability, not the other way round", p.ID, id, required)
			case kind == KindApplication && upper:
				return fmt.Errorf("package %s is an application and cannot require %s, which is %s; share through master data or a capability contract, or make %s an extension without its own app", p.ID, id, required, p.ID)
			case kind == KindExtension && upper && (constraint == "" || constraint == "*"):
				return fmt.Errorf("package %s extends %s and must name the versions it works with, such as %s@^1.0.0", p.ID, id, id)
			}
		}
	}
	return nil
}

func hasApplication(p platform.PackageDescriptor) bool {
	if p.Blueprint == nil {
		return false
	}
	return slices.ContainsFunc(p.Blueprint.Roots, func(root platform.AssetRef) bool { return root.Kind == platform.AssetApp })
}

func providesContract(p platform.PackageDescriptor) bool {
	if p.Blueprint == nil {
		return false
	}
	for _, asset := range p.Blueprint.Assets {
		if asset.Ref.Kind == protocol.AssetContract {
			return true
		}
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(asset.Body, &body) == nil {
			if raw := string(body["protocols"]); raw != "" && raw != "null" && raw != "[]" && raw != "{}" {
				return true
			}
		}
	}
	return false
}
