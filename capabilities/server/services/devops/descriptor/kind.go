package descriptor

import (
	"encoding/json"
	"fmt"

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

func PackageKinds(packages []platform.PackageDescriptor) map[string]string {
	kinds := map[string]string{}
	inSet := map[string]bool{}
	for _, p := range packages {
		inSet[p.ID] = true
	}
	ready := func(p platform.PackageDescriptor) bool {
		for _, need := range p.Requires {
			if id, _ := PackageDependency(need); inSet[id] && kinds[id] == "" {
				return false
			}
		}
		return true
	}
	for range packages {
		for _, p := range packages {
			if kinds[p.ID] == "" && ready(p) {
				kinds[p.ID] = Kind(p, kinds)
			}
		}
	}
	for _, p := range packages {
		if kinds[p.ID] == "" {
			kinds[p.ID] = Kind(p, kinds)
		}
	}
	return kinds
}

func Kind(p platform.PackageDescriptor, kinds map[string]string) string {
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

func CheckKinds(packages []platform.PackageDescriptor, kinds map[string]string) error {
	for _, p := range packages {
		kind := kinds[p.ID]
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
	for _, root := range p.Blueprint.Roots {
		if root.Kind != platform.AssetApp {
			continue
		}
		for _, asset := range p.Blueprint.Assets {
			var body struct {
				Surface string `json:"surface"`
			}
			if asset.Ref == root && json.Unmarshal(asset.Body, &body) == nil && body.Surface != KindMasterData {
				return true
			}
		}
	}
	return false
}

func providesContract(p platform.PackageDescriptor) bool {
	if p.Blueprint == nil {
		return false
	}
	for _, asset := range p.Blueprint.Assets {
		if asset.Ref.Kind == protocol.AssetContract || asset.Ref.Kind == protocol.AssetImplementation {
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
