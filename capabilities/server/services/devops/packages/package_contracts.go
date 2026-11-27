package packages

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/services/ontology"
	"slices"
	"strings"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
)

func contractProviders(installed map[string]platform.PackageDescriptor) (map[string][]string, error) {
	providers := map[string][]string{}
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		provided, err := packagedesc.ProvidedContracts(installed[id])
		if err != nil {
			return nil, err
		}
		for _, contract := range provided {
			providers[contract] = append(providers[contract], id)
		}
	}
	return providers, nil
}

func contractStatus(p platform.PackageDescriptor, providers map[string][]string) []protocol.PackageContractStatus {
	if p.Contracts == nil {
		return nil
	}
	var out []protocol.PackageContractStatus
	for _, id := range p.Contracts.Requires {
		out = append(out, protocol.PackageContractStatus{Contract: id, Required: true, Providers: append([]string{}, providers[id]...)})
	}
	for _, id := range p.Contracts.Optional {
		out = append(out, protocol.PackageContractStatus{Contract: id, Providers: append([]string{}, providers[id]...)})
	}
	return out
}

func requiredContractProblems(installed map[string]platform.PackageDescriptor) ([]string, error) {
	providers, err := contractProviders(installed)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		for _, status := range contractStatus(installed[id], providers) {
			if status.Required && len(status.Providers) == 0 {
				problems = append(problems, fmt.Sprintf("package %s requires contract %s, which has no active provider", id, status.Contract))
			}
		}
	}
	return problems, nil
}

func (d *Owner) activeDescriptors(index *protocol.PackageIndex) (map[string]platform.PackageDescriptor, error) {
	installed, err := d.installedDescriptors(index)
	if err != nil {
		return nil, err
	}
	state := d.packagesOf()
	for id := range installed {
		if held := state[id]; held == nil || held.State != "active" {
			delete(installed, id)
		}
	}
	return installed, nil
}

func (d *Owner) PackageContractStatus(id, version string) ([]protocol.PackageContractStatus, error) {
	index, err := d.packageIndex()
	if err != nil {
		return nil, err
	}
	installed, err := d.activeDescriptors(index)
	if err != nil {
		return nil, err
	}
	candidate, ok := installed[id]
	if !ok || candidate.Version != version {
		candidate, ok = packagedesc.Find(index, id, version)
		if !ok {
			return nil, fmt.Errorf("product %s@%s is unavailable", id, version)
		}
	}
	if planned, err := ResolveDependencies(candidate, index, installed); err == nil {
		for _, p := range planned {
			installed[p.ID] = p
		}
	}
	installed[id] = candidate
	providers, err := contractProviders(installed)
	if err != nil {
		return nil, err
	}
	out := contractStatus(candidate, providers)
	for i := range out {
		entries, err := contractEntries(candidate, out[i].Contract)
		if err != nil {
			return nil, err
		}
		out[i].Entries = entries
	}
	provided, err := packagedesc.ProvidedContracts(candidate)
	if err != nil {
		return nil, err
	}
	for _, contract := range provided {
		row := protocol.PackageContractStatus{Contract: contract, Provided: true, Providers: append([]string{}, providers[contract]...)}
		for _, key := range slices.Sorted(maps.Keys(installed)) {
			if key == id {
				continue
			}
			other := installed[key]
			if other.Contracts == nil || !slices.Contains(other.Contracts.Requires, contract) && !slices.Contains(other.Contracts.Optional, contract) {
				continue
			}
			row.Consumers = append(row.Consumers, key)
			entries, err := contractEntries(other, contract)
			if err != nil {
				return nil, err
			}
			row.Entries = append(row.Entries, entries...)
		}
		slices.Sort(row.Entries)
		row.Entries = slices.Compact(row.Entries)
		out = append(out, row)
	}
	return out, nil
}

func contractEntries(p platform.PackageDescriptor, contract string) ([]string, error) {
	if p.Blueprint == nil {
		return nil, nil
	}
	var out []string
	uses := func(list []string) bool {
		return slices.ContainsFunc(list, func(use string) bool { id, _, _ := strings.Cut(use, "#"); return id == contract })
	}
	for _, asset := range p.Blueprint.Assets {
		switch asset.Ref.Kind {
		case platform.AssetObject:
			var object ontology.Object
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				return nil, err
			}
			for _, action := range object.Actions {
				if uses(action.Uses) {
					out = append(out, asset.Ref.Name+"."+action.Name)
				}
			}
		case platform.AssetAction:
			a, err := ontology.ActionDescriptor(asset.Body)
			if err != nil {
				return nil, err
			}
			if uses(a.Uses) {
				out = append(out, a.Schema)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}
