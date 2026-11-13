package packages

import (
	"fmt"
	"slices"
	"strings"

	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

type Impact struct {
	Resources  []build.Resource
	Records    func(typ string) (int, bool)
	Connection func(id string) (platform.PackageImpactConnection, bool, error)
	Removals   func(removed, replaced map[platform.AssetRef]bool) []platform.PackageImpactReference
	Extensions func(platform.ReleaseCandidate) ([]platform.PackageImpactExtension, error)
	Runs       func(ref platform.AssetRef, offset int) (platform.ApplicationRunPage, *kernel.Error)
}

func StoredImpact(store *records.Store, definitions []platform.Definition, active func(string) bool, extensions func(platform.ReleaseCandidate) ([]platform.PackageImpactExtension, error), runs func(platform.AssetRef, int) (platform.ApplicationRunPage, *kernel.Error)) Impact {
	resources := Resources(store)
	return Impact{Resources: resources,
		Records: func(typ string) (int, bool) {
			if objects := store.Type(typ); objects != nil {
				return objects.Rows().Len(), true
			}
			return 0, false
		},
		Connection: func(id string) (platform.PackageImpactConnection, bool, error) {
			record := store.Type(dataowner.ConnectionType).Rows().Get(id)
			if record == nil {
				return platform.PackageImpactConnection{}, false, nil
			}
			connection := record.Value.Interface().(dataowner.Connection)
			var definition dataowner.ConnectionDefinition
			if connection.Published != "" {
				var accepted platform.ReleaseAsset
				if err := dataowner.DecodeDescriptor([]byte(connection.Published), &accepted); err != nil {
					return platform.PackageImpactConnection{}, false, err
				}
				if err := dataowner.DecodeDescriptor(accepted.Body, &definition); err != nil {
					return platform.PackageImpactConnection{}, false, err
				}
			}
			return platform.PackageImpactConnection{Configured: connection.Address != "" && (!definition.Credential || connection.Secret != ""), Credential: definition.Credential}, true, nil
		},
		Removals: func(removed, replaced map[platform.AssetRef]bool) []platform.PackageImpactReference {
			return RemovalImpacts(resources, definitions, active, removed, replaced)
		},
		Extensions: extensions,
		Runs:       runs}
}

func (i Impact) Of(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	out := platform.PackageLifecycleImpact{Dependencies: []string{}, References: []platform.PackageImpactReference{}, Extensions: []platform.PackageImpactExtension{}, Runs: []platform.ApplicationRun{}, Connections: []platform.PackageImpactConnection{}, Outputs: []platform.PackageImpactOutput{}}
	owned, removed, replaced := map[platform.AssetRef]bool{}, map[platform.AssetRef]bool{}, map[platform.AssetRef]bool{}
	for _, asset := range after {
		replaced[asset.Ref] = true
	}
	for _, resource := range i.Resources {
		if resource.ManagedBy != id || resource.Archived || resource.Draft == "package:"+id {
			continue
		}
		owned[resource.Ref] = true
		if !replaced[resource.Ref] {
			removed[resource.Ref] = true
		}
		switch resource.Ref.Kind {
		case platform.AssetObject:
			if records, ok := i.Records(resource.Ref.Name); ok {
				out.Outputs = append(out.Outputs, platform.PackageImpactOutput{Resource: resource.Ref, Records: records, Retained: true})
			}
		case platform.AssetDataset, platform.AssetMediaSet:
			out.Outputs = append(out.Outputs, platform.PackageImpactOutput{Resource: resource.Ref, Retained: true})
		case platform.AssetConnection:
			connection, found, err := i.Connection(resource.Draft)
			if err != nil {
				return out, err
			}
			if found {
				connection.Resource = resource.Ref
				out.Connections = append(out.Connections, connection)
			}
		}
	}
	for _, impact := range i.Removals(removed, replaced) {
		out.References = append(out.References, impact)
	}
	extensions, err := i.Extensions(platform.ReleaseCandidate{Assets: after})
	if err != nil {
		return out, err
	}
	for _, extension := range extensions {
		if !owned[extension.Base.Ref] && !replaced[extension.Base.Ref] {
			continue
		}
		if removed[extension.Base.Ref] {
			extension.Issue = "The product removes this extension's base"
		}
		out.Extensions = append(out.Extensions, extension)
	}
	seen := map[string]bool{}
	for ref := range owned {
		if ref.Kind != platform.AssetApp {
			continue
		}
		for offset := 0; ; {
			page, refusal := i.Runs(ref, offset)
			if refusal != nil {
				return out, fmt.Errorf("Package work could not be inspected: %s", refusal.Message)
			}
			for _, run := range page.Runs {
				key := run.Kind + "/" + run.ID
				if seen[key] || run.Kind == "job" && (run.Association == "owner" || run.State == "scheduled") || slices.Contains([]string{"done", "completed", "delivered", "rejected", "failed", "discarded", "compensated", "canceled", "cancelled", "stopped"}, run.State) {
					continue
				}
				seen[key] = true
				out.Runs = append(out.Runs, run)
			}
			offset += len(page.Runs)
			if offset >= page.Total {
				break
			}
			if len(page.Runs) == 0 {
				return out, fmt.Errorf("Package work inventory is incomplete")
			}
		}
	}
	slices.SortFunc(out.Runs, func(left, right platform.ApplicationRun) int {
		return strings.Compare(left.Kind+"/"+left.ID, right.Kind+"/"+right.ID)
	})
	slices.SortFunc(out.Outputs, func(left, right platform.PackageImpactOutput) int {
		return strings.Compare(left.Resource.String(), right.Resource.String())
	})
	slices.SortFunc(out.Extensions, func(left, right platform.PackageImpactExtension) int { return strings.Compare(left.Type, right.Type) })
	slices.SortFunc(out.Connections, func(left, right platform.PackageImpactConnection) int {
		return strings.Compare(left.Resource.String(), right.Resource.String())
	})
	return out, nil
}
