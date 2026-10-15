package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func agentReleaseAsset(a Agent, source string) (platform.ReleaseAsset, error) {
	cases, err := json.Marshal(a.Cases)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	if string(cases) == "null" {
		cases = []byte("[]")
	}
	descriptor := platform.AgentReleaseDescriptor{Name: a.Name, Title: a.Title, Description: a.Description, Instructions: a.Instructions, Tools: slices.Clone(a.Tools), Budget: a.Definition().Budget, Checkpoints: slices.Clone(a.Checkpoints), HandoffRole: a.HandoffRole, Cases: cases}
	if err := descriptor.Check(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: a.Name}, ContractVersion: 1, SourceVersion: source + ".agent-" + strconv.Itoa(a.Version), Body: body})
}

func decodeAgentAsset(asset platform.ReleaseAsset) (Agent, error) {
	var descriptor platform.AgentReleaseDescriptor
	versionText, found := strings.CutPrefix(asset.SourceVersion, "1.agent-")
	version, err := strconv.Atoi(versionText)
	if !found || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != versionText || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetAgent || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &descriptor) != nil || descriptor.Native || descriptor.Name != asset.Ref.Name || descriptor.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return Agent{}, fmt.Errorf("invalid builder agent asset")
	}
	record := Agent{Name: descriptor.Name, Title: descriptor.Title, Description: descriptor.Description, Instructions: descriptor.Instructions, Tools: slices.Clone(descriptor.Tools), Steps: descriptor.Budget.Steps, Tokens: descriptor.Budget.Tokens, Actions: descriptor.Budget.Actions, Cost: descriptor.Budget.Cost, Checkpoints: slices.Clone(descriptor.Checkpoints), HandoffRole: descriptor.HandoffRole, Version: version, State: "published"}
	if len(descriptor.Cases) > 0 {
		if err := json.Unmarshal(descriptor.Cases, &record.Cases); err != nil {
			return Agent{}, err
		}
	}
	return record, nil
}

func (b *Build) InstallAgentAsset(asset platform.ReleaseAsset) error {
	record, err := decodeAgentAsset(asset)
	if err != nil {
		return err
	}
	if err := b.checkAgent(record); err != nil {
		return err
	}
	b.agents[record.Name] = record
	return nil
}

func (b *Build) agentAssets() ([]platform.ReleaseAsset, error) {
	names := make([]string, 0, len(b.agents))
	for name := range b.agents {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.ReleaseAsset, 0, len(names))
	for _, name := range names {
		asset, err := agentReleaseAsset(b.agents[name], b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (b *Build) agentDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	rows, problem := b.agentInventory()
	if problem != nil {
		err = problem
		return
	}
	index := slices.IndexFunc(rows, func(a Agent) bool { return a.ID == id && !a.Archived })
	if index < 0 {
		err = fmt.Errorf("saved agent draft is unavailable")
		return
	}
	record := rows[index]
	if previous, ok := wasPublished[Agent](record.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: previous.Name}
		hadPrior = true
	}
	if err = b.checkAgent(record); err != nil {
		return
	}
	if record.Version >= 64 {
		err = fmt.Errorf("agent version family is full")
		return
	}
	record.Version++
	next = platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: record.Name}
	asset, problem := agentReleaseAsset(record, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareAgentReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	rows, err := b.agentInventory()
	if err != nil {
		return nil, err
	}
	out := []ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetAgent {
			continue
		}
		frozen, err := decodeAgentAsset(asset)
		if err != nil {
			return nil, err
		}
		index := slices.IndexFunc(rows, func(a Agent) bool { return !a.Archived && a.Name == frozen.Name })
		if index < 0 {
			return nil, fmt.Errorf("agent has no local draft")
		}
		frozen.Record = rows[index].Record
		frozen.Published = published(frozen)
		image, err := json.Marshal(frozen)
		if err != nil {
			return nil, err
		}
		out = append(out, ReleasePublication{Schema: SchemaAgentPublish, Image: image})
	}
	return out, nil
}
