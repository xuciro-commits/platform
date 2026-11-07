package host

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

func (h hostView) PackageLifecycleImpact(c platform.Caller, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	return h.t.packageLifecycleImpact(c.Member, id, after)
}

func (t *Tenant) packageLifecycleImpact(m platform.Member, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	out := platform.PackageLifecycleImpact{Dependencies: []string{}, References: []platform.PackageImpactReference{}, Extensions: []platform.PackageImpactExtension{}, Runs: []platform.ApplicationRun{}, Connections: []platform.PackageImpactConnection{}, Outputs: []platform.PackageImpactOutput{}}
	owned, removed, replaced := map[platform.AssetRef]bool{}, map[platform.AssetRef]bool{}, map[platform.AssetRef]bool{}
	for _, asset := range after {
		replaced[asset.Ref] = true
	}
	for _, row := range t.records.Type(build.ResourceType).Rows().All() {
		resource := row.Value.Interface().(build.Resource)
		if resource.ManagedBy != id || resource.Archived || resource.Draft == "package:"+id {
			continue
		}
		owned[resource.Ref] = true
		if !replaced[resource.Ref] {
			removed[resource.Ref] = true
		}
		switch resource.Ref.Kind {
		case platform.AssetObject:
			if objects := t.records.Type(resource.Ref.Name); objects != nil {
				out.Outputs = append(out.Outputs, platform.PackageImpactOutput{Resource: resource.Ref, Records: objects.Rows().Len(), Retained: true})
			}
		case platform.AssetDataset, platform.AssetMediaSet:
			out.Outputs = append(out.Outputs, platform.PackageImpactOutput{Resource: resource.Ref, Retained: true})
		case platform.AssetConnection:
			if record := t.records.Type(dataowner.ConnectionType).Rows().Get(resource.Draft); record != nil {
				connection := record.Value.Interface().(dataowner.Connection)
				var definition dataowner.ConnectionDefinition
				if connection.Published != "" {
					var accepted platform.ReleaseAsset
					if err := dataowner.DecodeDescriptor([]byte(connection.Published), &accepted); err != nil {
						return out, err
					}
					if err := dataowner.DecodeDescriptor(accepted.Body, &definition); err != nil {
						return out, err
					}
				}
				out.Connections = append(out.Connections, platform.PackageImpactConnection{Resource: resource.Ref, Configured: connection.Address != "" && (!definition.Credential || connection.Secret != ""), Credential: definition.Credential})
			}
		}
	}
	for _, impact := range t.packageRemovalImpacts(removed, replaced) {
		out.References = append(out.References, platform.PackageImpactReference(impact))
	}
	extensions, err := t.releaseExtensionReview(platform.ReleaseCandidate{Assets: after})
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
	ctx := t.applicationContext()
	ctx.Lock, ctx.Unlock = func() {}, func() {}
	seen := map[string]bool{}
	for ref := range owned {
		if ref.Kind != platform.AssetApp {
			continue
		}
		for offset := 0; ; {
			page, refusal := ctx.Runs(m, ref, offset, 100, time.Now())
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
