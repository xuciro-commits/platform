package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// SimulationSample is explicit synthetic data, never a request to copy a
// production row. The source owner's published model/scope/query is retained.
type SimulationSample struct {
	Type    string            `json:"type"`
	Records []json.RawMessage `json:"records"`
}

type simulationEnvironment struct {
	Modules   map[string][]byte
	Manifests []platform.Manifest
	Samples   []SimulationSample
}

// sampleOwner exposes only selected immutable metadata and read scopes. It
// cannot run production business callbacks or dispatch external effects.
type sampleOwner struct {
	manifest platform.Manifest
	ledger   *platform.Ledger
}

func newSampleOwner(tenant string, manifest platform.Manifest) *sampleOwner {
	var classes []string
	for _, entity := range manifest.Entities {
		classes = append(classes, entity.Type)
	}
	return &sampleOwner{manifest: manifest, ledger: platform.NewLedger(tenant, manifest.ID, manifest.Actions, classes...)}
}
func (a *sampleOwner) Manifest() platform.Manifest              { return a.manifest }
func (a *sampleOwner) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *sampleOwner) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *sampleOwner) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }
func (a *sampleOwner) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Native business actions require an isolated owner test fixture")
}
func (a *sampleOwner) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (a *sampleOwner) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (t *Tenant) simulationEnvironment(candidate platform.ReleaseCandidate, samples []SimulationSample) (simulationEnvironment, error) {
	environment := simulationEnvironment{Modules: map[string][]byte{}, Samples: samples}
	selected := map[platform.AssetRef]bool{}
	owners := map[string]bool{}
	for _, asset := range candidate.Assets {
		selected[asset.Ref] = true
		if asset.Ref.App != "build" {
			owners[asset.Ref.App] = true
		}
		if asset.Ref.App != "build" && asset.Ref.Kind == platform.AssetAction {
			return environment, fmt.Errorf("native action %s needs an isolated owner test fixture", asset.Ref)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(owners)) {
		app := t.app(id)
		if app == nil {
			return environment, fmt.Errorf("sample owner %s is not installed", id)
		}
		original := app.Manifest()
		manifest := platform.Manifest{ID: id, Title: original.Title, Version: original.Version, Actions: platform.NewCatalog(), Roles: original.AllRoles(), Languages: original.Languages}
		for _, entity := range original.Entities {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetObject, Name: entity.Type}] {
				if entity.Scope.Through != nil || entity.Scope.Participants != nil || entity.Scope.Structure != "" {
					return environment, fmt.Errorf("sample source %s needs an isolated scope fixture", entity.Type)
				}
				entity.Seed = nil
				entity.Standard = platform.Standard{}
				manifest.Entities = append(manifest.Entities, entity)
			}
		}
		for _, l := range original.LinkTypes {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetLinkType, Name: l.Name}] {
				manifest.LinkTypes = append(manifest.LinkTypes, l)
			}
		}
		for _, query := range original.Queries {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetQuery, Name: query.Name}] {
				manifest.Queries = append(manifest.Queries, query)
			}
		}
		for _, function := range original.Functions {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetFunction, Name: function.Name}] {
				manifest.Functions = append(manifest.Functions, function)
			}
		}
		for _, operation := range original.Operations {
			if selected[platform.AssetRef{App: id, Kind: platform.AssetCompute, Name: operation.Name}] {
				manifest.Operations = append(manifest.Operations, operation)
			}
		}
		environment.Manifests = append(environment.Manifests, manifest)
	}
	if len(samples) > 8 {
		return environment, fmt.Errorf("candidate samples accept at most eight object types")
	}
	total := 0
	seen := map[string]bool{}
	for _, sample := range samples {
		// The candidate's object references are the exact sample allow-list.
		allowed := false
		for ref := range selected {
			allowed = allowed || ref.Kind == platform.AssetObject && ref.Name == sample.Type
		}
		if !allowed || seen[sample.Type] || len(sample.Records) > 100 {
			return environment, fmt.Errorf("sample type %s is missing, duplicate or exceeds 100 records", sample.Type)
		}
		seen[sample.Type] = true
		ids := map[string]bool{}
		for _, raw := range sample.Records {
			total += len(raw)
			if total > 256<<10 {
				return environment, fmt.Errorf("candidate sample bytes exceed their bound")
			}
			if _, err := platform.DecodeValue(raw, 32<<10); err != nil {
				return environment, err
			}
			var identity struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &identity) != nil || identity.ID == "" || len(identity.ID) > 128 || ids[identity.ID] {
				return environment, fmt.Errorf("sample rows need unique bounded IDs")
			}
			ids[identity.ID] = true
		}
	}
	return environment, nil
}
